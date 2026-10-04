package controlplane

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/usage"
	log "github.com/sirupsen/logrus"
)

func decode(data string, v any) error { return json.Unmarshal([]byte(data), v) }

// SynchronousUsage keeps durable accounting outside the volatile dispatch queue.
func (s *Store) SynchronousUsage() bool { return true }

// HandleUsage settles at the publication boundary. OAuth headers,
// raw API keys, session IDs and failure bodies are deliberately not serialized.
func (s *Store) HandleUsage(ctx context.Context, r usage.Record) {
	if ctx != nil {
		if observation, ok := ctx.Value(httpObservationKey{}).(*httpObservation); ok {
			observation.published.Store(true)
		}
	}
	defer func() {
		if recover() != nil {
			s.accountingFailed.Store(true)
			log.Error("control-plane usage persistence panicked; request admission disabled")
		}
	}()
	if s.closed.Load() {
		return
	}
	var evidence *dispatchEvidence
	request := RequestFromContext(ctx)
	if request.Evidence != nil {
		request.Evidence.mu.Lock()
		if value, ok := request.Evidence.executions[r.RequestID]; ok {
			evidence = &value
		} else if value, ok := request.Evidence.executions[usage.ExecutionRequestIDFromContext(ctx)]; ok {
			// Side-model usage has a distinct settlement ID, but belongs to
			// the same bounded request-scoped dispatch evidence as its parent.
			evidence = &value
		}
		request.Evidence.mu.Unlock()
	}
	if err := s.recordUsage(r, request.WarmAccount != "", evidence); err != nil {
		s.accountingFailed.Store(true)
		log.WithError(err).Error("control-plane usage persistence failed")
	}
}

func (s *Store) RecordUsage(r usage.Record) error {
	return s.recordUsage(r, false)
}

func (s *Store) recordUsage(r usage.Record, warm bool, evidence ...*dispatchEvidence) error {
	st := s.state.Load()
	accountID := st.Credentials[credentialRef(r.Provider, r.AuthID)]
	keyID := ""
	if k, ok := s.Key(r.APIKey); ok {
		keyID = k.ID
	} else if _, ok := st.Keys[r.APIKey]; ok {
		keyID = r.APIKey
	}
	status := 200
	if r.Failed {
		status = r.Fail.StatusCode
		if status == 0 {
			status = 502
		}
	}
	model := r.Alias
	if model == "" {
		model = r.Model
	}
	actual := r.ResponseModel
	if actual == "" {
		actual = r.Model
	}
	at := r.RequestedAt
	if at.IsZero() {
		at = s.now()
	}
	entry := RequestLog{ID: r.RequestID, At: at, Provider: r.Provider, Model: model, ActualModel: actual, Account: accountID, APIKey: keyID, Status: status, Input: r.Detail.InputTokens, Output: r.Detail.OutputTokens, Reasoning: r.Detail.ReasoningTokens, Cache: r.Detail.CachedTokens, Total: r.Detail.TotalTokens, LatencyMS: r.Latency.Milliseconds(), TTFTMS: r.TTFT.Milliseconds()}
	if r.Failed {
		switch status {
		case 401, 403:
			entry.ErrorCategory = "authentication"
		case 429:
			entry.ErrorCategory = "rate_limit"
		case 400, 404, 422:
			entry.ErrorCategory = "request"
		default:
			entry.ErrorCategory = "upstream"
		}
	}
	if len(evidence) > 0 && evidence[0] != nil {
		entry.Routing = &evidence[0].Decision
		entry.Retries = evidence[0].Retry
		entry.Pool = evidence[0].Decision.Pool
	}
	if entry.ID == "" {
		return fmt.Errorf("usage missing idempotency ID")
	}
	inputPrice, outputPrice := st.Settings.InputPricePerMillion, st.Settings.OutputPricePerMillion
	priced := inputPrice > 0 || outputPrice > 0
	for _, price := range st.Settings.Pricing {
		if price.Model == actual && (price.Provider == "" || price.Provider == r.Provider) {
			inputPrice, outputPrice = price.InputPerMillion, price.OutputPerMillion
			priced = true
			if price.Provider == r.Provider {
				break
			}
		}
	}
	if priced {
		cost := (float64(entry.Input)*inputPrice + float64(entry.Output)*outputPrice) / 1e6
		entry.Cost = &cost
	}
	if key, ok := st.Keys[keyID]; ok && entry.Pool == "" {
		for _, id := range key.Bindings[r.Provider].Pools {
			if p, ok := st.Pools[id]; ok && contains(p.Accounts, accountID) {
				entry.Pool = id
				break
			}
		}
	}
	if entry.Pool == "" {
		entry.Pool = "default-" + r.Provider
	}
	data, err := encode(entry)
	if err != nil {
		return err
	}
	accountUpdates := make(map[string]Account)
	keyUpdates := make(map[string]APIKey)
	err = s.mutateAccounting(func(tx *sql.Tx) error {
		settlement, err := tx.Exec("INSERT OR IGNORE INTO settlements(id) VALUES(?)", entry.ID)
		if err != nil {
			return err
		}
		settled, err := settlement.RowsAffected()
		if err != nil || settled == 0 {
			return err
		}
		if _, err = tx.Exec("UPDATE executions SET state='settled',model=?,api_key=? WHERE id=?", entry.Model, keyID, entry.ID); err != nil {
			return err
		}
		result, err := tx.Exec("INSERT OR IGNORE INTO requests(id,at,provider,model,account,pool,api_key,status,tokens,latency,data) VALUES(?,?,?,?,?,?,?,?,?,?,?)", entry.ID, at.Unix(), entry.Provider, model, accountID, entry.Pool, keyID, status, entry.Total, entry.LatencyMS, data)
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		if err != nil || n == 0 {
			return err
		}
		cost := 0.0
		if entry.Cost != nil {
			cost = *entry.Cost
		}
		failed := 0
		if r.Failed {
			failed = 1
		}
		_, err = tx.Exec(`INSERT INTO rollups(day,provider,model,account,pool,api_key,requests,failures,tokens,latency,cost) VALUES(?,?,?,?,?,?,1,?,?,?,?) ON CONFLICT(day,provider,model,account,pool,api_key) DO UPDATE SET requests=requests+1,failures=failures+excluded.failures,tokens=tokens+excluded.tokens,latency=latency+excluded.latency,cost=cost+excluded.cost`, at.UTC().Format("2006-01-02"), r.Provider, model, accountID, entry.Pool, keyID, failed, entry.Total, entry.LatencyMS, cost)
		if err != nil {
			return err
		}
		if account, ok := s.state.Load().Accounts[accountID]; ok {
			updatePoolHealth(&account, s.state.Load().Pools[entry.Pool], status, s.now())
			account.Requests++
			account.Failures += int64(failed)
			if !warm {
				account.LastTraffic = s.now()
			}
			accountUpdates[accountID] = account
			if err = put(tx, "account", accountID, account); err != nil {
				return err
			}
		}
		if key, ok := s.state.Load().Keys[keyID]; ok {
			key.Tokens += entry.Total
			key.Cost += cost
			keyUpdates[keyID] = key
			if err = put(tx, "key", keyID, key); err != nil {
				return err
			}
		}
		return nil
	}, accountUpdates, keyUpdates)
	if err != nil {
		return err
	}
	if accountID != "" {
		return s.ObserveQuota(ParseQuota(r.Provider, accountID, r.ResponseHeaders, s.now()))
	}
	return nil
}

type LogFilter struct {
	Provider, Model, Account, Pool, APIKey string
	Since                                  int64
	Limit                                  int
	Before                                 int64
	BeforeID                               string
	Status                                 int
	Offset                                 int
}

func (s *Store) RequestLogs(ctx context.Context, f LogFilter) ([]RequestLog, error) {
	if f.Limit <= 0 || f.Limit > 500 {
		f.Limit = 100
	}
	query := "SELECT data FROM requests WHERE at>=?"
	args := []any{f.Since}
	for _, filter := range []struct{ column, value string }{{"provider", f.Provider}, {"model", f.Model}, {"account", f.Account}, {"pool", f.Pool}, {"api_key", f.APIKey}} {
		if filter.value != "" {
			query += " AND " + filter.column + "=?"
			args = append(args, filter.value)
		}
	}
	if f.Before > 0 {
		if f.BeforeID != "" {
			query += " AND (at<? OR (at=? AND id<?))"
			args = append(args, f.Before, f.Before, f.BeforeID)
		} else {
			query += " AND at<?"
			args = append(args, f.Before)
		}
	}
	if f.Status > 0 {
		query += " AND status=?"
		args = append(args, f.Status)
	}
	query += " ORDER BY at DESC,id DESC LIMIT ?"
	args = append(args, f.Limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []RequestLog{}
	for rows.Next() {
		var data string
		var v RequestLog
		if err = rows.Scan(&data); err != nil {
			return nil, err
		}
		if err = decode(data, &v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

type Rollup struct {
	Day      string  `json:"day"`
	Provider string  `json:"provider"`
	Model    string  `json:"model"`
	Account  string  `json:"account"`
	Pool     string  `json:"pool"`
	APIKey   string  `json:"api_key"`
	Requests int64   `json:"requests"`
	Failures int64   `json:"failures"`
	Tokens   int64   `json:"tokens"`
	Latency  int64   `json:"latency_ms"`
	Cost     float64 `json:"cost"`
}

func (s *Store) Analytics(ctx context.Context, since string) ([]Rollup, error) {
	return s.AnalyticsFiltered(ctx, since, "", LogFilter{})
}

func (s *Store) AnalyticsFiltered(ctx context.Context, since, until string, f LogFilter) ([]Rollup, error) {
	where, args := s.analyticsWhere(since, until, f)
	query := "SELECT day,provider,model,account,pool,api_key,requests,failures,tokens,latency,cost FROM rollups" + where
	limit := f.Limit
	if limit <= 0 || limit > 5000 {
		limit = 5000
	}
	offset := f.Offset
	if offset < 0 {
		offset = 0
	}
	query += " ORDER BY day DESC,provider,model,account,pool,api_key LIMIT ? OFFSET ?"
	args = append(args, limit, offset)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []Rollup{}
	for rows.Next() {
		var v Rollup
		if err = rows.Scan(&v.Day, &v.Provider, &v.Model, &v.Account, &v.Pool, &v.APIKey, &v.Requests, &v.Failures, &v.Tokens, &v.Latency, &v.Cost); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Store) analyticsWhere(since, until string, f LogFilter) (string, []any) {
	if since == "" {
		since = s.now().AddDate(0, 0, -30).Format("2006-01-02")
	}
	query := " WHERE day>=?"
	args := []any{since}
	if until != "" {
		query += " AND day<=?"
		args = append(args, until)
	}
	for _, filter := range []struct{ column, value string }{{"provider", f.Provider}, {"model", f.Model}, {"account", f.Account}, {"pool", f.Pool}, {"api_key", f.APIKey}} {
		if filter.value != "" {
			query += " AND " + filter.column + "=?"
			args = append(args, filter.value)
		}
	}
	return query, args
}

type AnalyticsPage struct {
	Rows       []Rollup `json:"rows"`
	Totals     Rollup   `json:"totals"`
	Groups     int64    `json:"groups"`
	NextOffset int      `json:"next_offset"`
	Daily      []Rollup `json:"daily"`
}

func (s *Store) AnalyticsPage(ctx context.Context, since, until string, f LogFilter) (AnalyticsPage, error) {
	if f.Limit <= 0 || f.Limit > 500 {
		f.Limit = 100
	}
	if f.Offset < 0 {
		f.Offset = 0
	}
	page := AnalyticsPage{NextOffset: -1}
	var err error
	page.Rows, err = s.AnalyticsFiltered(ctx, since, until, f)
	if err != nil {
		return page, err
	}
	where, args := s.analyticsWhere(since, until, f)
	err = s.db.QueryRowContext(ctx, "SELECT count(*),coalesce(sum(requests),0),coalesce(sum(failures),0),coalesce(sum(tokens),0),coalesce(sum(latency),0),coalesce(sum(cost),0) FROM rollups"+where, args...).Scan(&page.Groups, &page.Totals.Requests, &page.Totals.Failures, &page.Totals.Tokens, &page.Totals.Latency, &page.Totals.Cost)
	if err != nil {
		return page, err
	}
	rows, err := s.db.QueryContext(ctx, "SELECT day,sum(requests),sum(failures),sum(tokens),sum(latency),sum(cost) FROM rollups"+where+" GROUP BY day ORDER BY day DESC LIMIT 3660", args...)
	if err != nil {
		return page, err
	}
	defer func() { _ = rows.Close() }()
	page.Daily = []Rollup{}
	for rows.Next() {
		var day Rollup
		if err = rows.Scan(&day.Day, &day.Requests, &day.Failures, &day.Tokens, &day.Latency, &day.Cost); err != nil {
			return page, err
		}
		page.Daily = append(page.Daily, day)
	}
	if err = rows.Err(); err != nil {
		return page, err
	}
	if int64(f.Offset+len(page.Rows)) < page.Groups {
		page.NextOffset = f.Offset + len(page.Rows)
	}
	return page, err
}

func (s *Store) Retain(days int) error {
	if days <= 0 {
		days = 30
	}
	cutoff := s.now().AddDate(0, 0, -days).Unix()
	return s.mutate("maintenance.retention", "telemetry", func(tx *sql.Tx) error {
		for _, table := range []string{"requests", "quota_history", "decisions", "warm_runs"} {
			if _, err := tx.Exec("DELETE FROM "+table+" WHERE at<?", cutoff); err != nil {
				return err
			}
		}
		// Unresolved rows are recovery evidence, not ordinary raw telemetry.
		if _, err := tx.Exec("DELETE FROM executions WHERE state='settled' AND at<?", cutoff); err != nil {
			return err
		}
		_, err := tx.Exec("DELETE FROM affinity WHERE expires<?", s.now().Unix())
		return err
	})
}

func (s *Store) History(ctx context.Context, kind, account string) ([]json.RawMessage, error) {
	query := ""
	args := []any{}
	switch kind {
	case "quota":
		query = "SELECT data FROM quota_history WHERE account=? ORDER BY at DESC LIMIT 200"
		args = append(args, account)
	case "decisions":
		query = "SELECT data FROM decisions ORDER BY id DESC LIMIT 200"
	case "warm":
		query = "SELECT json_object('at',at,'account',account,'pool',pool,'reason',reason,'result',result) FROM warm_runs ORDER BY id DESC LIMIT 200"
	case "audit":
		query = "SELECT json_object('at',at,'action',action,'target',target) FROM audit ORDER BY id DESC LIMIT 200"
	default:
		return nil, fmt.Errorf("unknown history")
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []json.RawMessage{}
	for rows.Next() {
		var data string
		if err = rows.Scan(&data); err != nil {
			return nil, err
		}
		out = append(out, json.RawMessage(strings.TrimSpace(data)))
	}
	return out, rows.Err()
}

type HistoryPage struct {
	Rows []json.RawMessage `json:"rows"`
	Next int64             `json:"next"`
}

func (s *Store) HistoryPage(ctx context.Context, kind, account string, before int64, limit int) (HistoryPage, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	table, expression := "", "data"
	switch kind {
	case "quota":
		table = "quota_history"
	case "decisions":
		table = "decisions"
	case "warm":
		table = "warm_runs"
		expression = "json_object('at',at,'account',account,'pool',pool,'reason',reason,'result',result)"
	case "audit":
		table = "audit"
		expression = "json_object('at',at,'action',action,'target',target)"
	default:
		return HistoryPage{}, fmt.Errorf("unknown history")
	}
	query := "SELECT id," + expression + " FROM " + table + " WHERE 1=1"
	args := []any{}
	if kind == "quota" {
		query += " AND account=?"
		args = append(args, account)
	}
	if before > 0 {
		query += " AND id<?"
		args = append(args, before)
	}
	query += " ORDER BY id DESC LIMIT ?"
	args = append(args, limit+1)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return HistoryPage{}, err
	}
	defer func() { _ = rows.Close() }()
	page := HistoryPage{Rows: []json.RawMessage{}}
	var last int64
	for rows.Next() {
		var id int64
		var data string
		if err = rows.Scan(&id, &data); err != nil {
			return HistoryPage{}, err
		}
		if len(page.Rows) == limit {
			page.Next = last
			break
		}
		page.Rows = append(page.Rows, json.RawMessage(data))
		last = id
	}
	return page, rows.Err()
}
