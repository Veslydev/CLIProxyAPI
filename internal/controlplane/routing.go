package controlplane

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"math/rand/v2"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/usage"
)

type RequestContext struct {
	KeyID       string
	WarmAccount string
	WarmPool    string
	Evidence    *requestEvidence
}

type dispatchEvidence struct {
	Decision Decision
	Retry    int
}
type requestEvidence struct {
	mu         sync.Mutex
	current    Decision
	native     map[string]Decision
	attempts   int
	executions map[string]dispatchEvidence
}

func WithRequest(ctx context.Context, r RequestContext) context.Context {
	if r.Evidence == nil {
		r.Evidence = &requestEvidence{executions: make(map[string]dispatchEvidence)}
	}
	return auth.WithPolicyRequestContext(ctx, r)
}
func RequestFromContext(ctx context.Context) RequestContext {
	if ctx == nil {
		return RequestContext{}
	}
	r, _ := auth.PolicyRequestContext(ctx).(RequestContext)
	return r
}
func routingError(code, message string) error {
	return &auth.Error{Code: code, Message: message, HTTPStatus: http.StatusForbidden}
}

func (s *Store) AdmitRequest(ctx context.Context, req executor.Request, _ executor.Options) error {
	if s.accountingFailed.Load() {
		return &auth.Error{Code: "accounting_unavailable", Message: "durable accounting failed; repair storage before restarting", HTTPStatus: http.StatusServiceUnavailable}
	}
	r := RequestFromContext(ctx)
	if r.Evidence != nil {
		r.Evidence.mu.Lock()
		r.Evidence.attempts = 0
		r.Evidence.current = Decision{}
		r.Evidence.native = nil
		r.Evidence.mu.Unlock()
	}
	if r.KeyID == "" {
		return nil
	}
	key, ok := s.state.Load().Keys[r.KeyID]
	if !ok {
		return routingError("invalid_inference_key", "invalid inference key")
	}
	if len(key.Models) > 0 && !contains(key.Models, req.Model) {
		return routingError("model_forbidden", "model not allowed by inference key")
	}
	if _, err := s.admitKey(r.KeyID); err != nil {
		return &auth.Error{Code: "inference_key_limit", Message: err.Error(), HTTPStatus: http.StatusTooManyRequests}
	}
	return nil
}

func (s *Store) BeginExecution(ctx context.Context, a *auth.Auth, opts executor.Options) func() {
	s.ObserveExecution(ctx, a, opts)
	id := s.state.Load().Credentials[credentialRef(a.Provider, a.ID)]
	value, _ := s.flights.LoadOrStore(id, &atomic.Int64{})
	counter := value.(*atomic.Int64)
	counter.Add(1)
	var once sync.Once
	return func() {
		once.Do(func() {
			counter.Add(-1)
			s.FinishExecution(ctx)
		})
	}
}

func (s *Store) ObserveExecution(ctx context.Context, a *auth.Auth, _ executor.Options) {
	requestID := usage.ExecutionRequestIDFromContext(ctx)
	if requestID == "" {
		return
	}
	id := s.state.Load().Credentials[credentialRef(a.Provider, a.ID)]
	r := RequestFromContext(ctx)
	if r.Evidence != nil {
		r.Evidence.mu.Lock()
		if _, exists := r.Evidence.executions[requestID]; exists {
			r.Evidence.mu.Unlock()
			return
		}
		d := r.Evidence.current
		if d.Account != id {
			if native, ok := r.Evidence.native[id]; ok {
				d = native
			} else {
				d = Decision{At: s.now(), Account: id, Pool: "default-" + a.Provider, Strategy: "native", Reason: "native_selector"}
			}
		}
		{
			if len(r.Evidence.executions) >= 128 {
				var oldest string
				var at time.Time
				for key, value := range r.Evidence.executions {
					if oldest == "" || value.Decision.At.Before(at) {
						oldest, at = key, value.Decision.At
					}
				}
				delete(r.Evidence.executions, oldest)
			}
			r.Evidence.executions[requestID] = dispatchEvidence{Decision: d, Retry: r.Evidence.attempts}
		}
		r.Evidence.attempts++
		r.Evidence.mu.Unlock()
	}
	if err := s.startExecution(ctx, requestID, id, a.Provider); err != nil {
		s.accountingFailed.Store(true)
	}
}

func (s *Store) FinishExecution(ctx context.Context) {
	if id := usage.ExecutionRequestIDFromContext(ctx); id != "" {
		if _, err := s.db.Exec("UPDATE executions SET state='unresolved_no_usage' WHERE id=? AND state='running'", id); err != nil {
			s.accountingFailed.Store(true)
		}
	}
}

func (s *Store) InFlight(id string) int64 {
	if value, ok := s.flights.Load(id); ok {
		return value.(*atomic.Int64).Load()
	}
	return 0
}

type affinityObject struct{ kind, value string }

func objects(payload []byte) ([]affinityObject, bool) {
	var root any
	if json.Unmarshal(payload, &root) != nil {
		return nil, false
	}
	out := []affinityObject{}
	encrypted := false
	var walk func(any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			for k, v := range x {
				if k == "encrypted_content" {
					encrypted = true
				}
				if value, ok := v.(string); ok && value != "" {
					switch k {
					case "encrypted_content":
						out = append(out, affinityObject{"encrypted", value})
					case "previous_response_id":
						out = append(out, affinityObject{"response", value})
					case "conversation_id", "conversation":
						out = append(out, affinityObject{"conversation", value})
					case "file_id", "resource_id":
						out = append(out, affinityObject{k, value})
					}
				}
				if k == "conversation" {
					if c, ok := v.(map[string]any); ok {
						if id, ok := c["id"].(string); ok {
							out = append(out, affinityObject{"conversation", id})
						}
					}
				}
				walk(v)
			}
		case []any:
			for _, v := range x {
				walk(v)
			}
		}
	}
	walk(root)
	return out, encrypted
}
func objectHash(o affinityObject) string { return hash("codex\x00" + o.kind + "\x00" + o.value) }

func (s *Store) owner(h string, hard bool) (string, error) {
	var id string
	var expires int64
	var storedHard bool
	err := s.db.QueryRow("SELECT account,hard,expires FROM affinity WHERE hash=?", h).Scan(&id, &storedHard, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if storedHard != hard || expires <= s.now().Unix() {
		return "", nil
	}
	return id, nil
}

func (s *Store) bind(h, id string, hard bool, ttl time.Duration) error {
	_, err := s.db.Exec("INSERT INTO affinity(hash,account,hard,expires) VALUES(?,?,?,?) ON CONFLICT(hash) DO UPDATE SET account=CASE WHEN affinity.hard=1 THEN affinity.account ELSE excluded.account END,expires=excluded.expires", h, id, hard, s.now().Add(ttl).Unix())
	return err
}

func (s *Store) FilterCandidates(ctx context.Context, model string, opts executor.Options, candidates []*auth.Auth) ([]*auth.Auth, error) {
	st := s.state.Load()
	now := s.now()
	request := RequestFromContext(ctx)
	key := APIKey{}
	if request.KeyID != "" {
		var ok bool
		key, ok = st.Keys[request.KeyID]
		if !ok || key.Revoked || (!key.ExpiresAt.IsZero() && !now.Before(key.ExpiresAt)) {
			return nil, routingError("key_revoked", "inference key revoked or expired")
		}
		if len(key.Models) > 0 && !contains(key.Models, model) {
			return nil, routingError("model_forbidden", "model not allowed by inference key")
		}
	}
	obj, encrypted := objects(opts.OriginalRequest)
	hasCodex := false
	if providers, ok := opts.Metadata[executor.CandidateProvidersMetadataKey].([]string); ok {
		hasCodex = contains(providers, "codex")
	}
	for _, a := range candidates {
		if a.Provider == "codex" {
			hasCodex = true
			break
		}
	}
	if !hasCodex {
		for _, o := range obj {
			id, err := s.owner(objectHash(o), true)
			if err != nil {
				return nil, err
			}
			if id != "" {
				hasCodex = true
				break
			}
		}
		if !hasCodex {
			obj = nil
			encrypted = false
		}
	}
	owner := ""
	for _, o := range obj {
		id, err := s.owner(objectHash(o), true)
		if err != nil {
			return nil, err
		}
		if id == "" {
			return nil, routingError("continuation_owner_unknown", "account-bound continuation owner is unknown; start a new request without continuation state")
		}
		if owner != "" && owner != id {
			return nil, routingError("continuation_owner_conflict", "continuation objects belong to different accounts")
		}
		owner = id
	}
	if encrypted && owner == "" {
		return nil, routingError("continuation_owner_unknown", "encrypted continuation ownership cannot be proven")
	}
	strategy := st.Settings.Strategy
	poolByAccount := map[string]string{}
	skipped := map[string]string{}
	eligible := []*auth.Auth{}
	for _, a := range candidates {
		id := st.Credentials[credentialRef(a.Provider, a.ID)]
		account, known := st.Accounts[id]
		if !known {
			skipped[a.ID] = "account_not_imported"
			continue
		}
		if st.Settings.Providers[a.Provider].Disabled {
			skipped[id] = "provider_disabled"
			continue
		}
		if account.Paused || a.Disabled {
			skipped[id] = "paused"
			continue
		}
		if request.WarmAccount != "" && request.WarmAccount != id {
			skipped[id] = "warm_pin"
			continue
		}
		if owner != "" && (owner != id || a.Provider != "codex") {
			skipped[id] = "hard_affinity"
			continue
		}
		if request.KeyID != "" {
			binding, ok := key.Bindings[a.Provider]
			allowed := ok && contains(binding.Accounts, id)
			for _, pid := range binding.Pools {
				p, exists := st.Pools[pid]
				if exists && p.Provider == a.Provider && contains(p.Accounts, id) && (len(p.Models) == 0 || contains(p.Models, model)) {
					allowed = true
					poolByAccount[id] = pid
					break
				}
			}
			if !allowed {
				skipped[id] = "key_scope"
				continue
			}
		}
		if request.WarmPool != "" {
			p, exists := st.Pools[request.WarmPool]
			if !exists || request.WarmAccount != id || p.Provider != a.Provider || !p.Warm.Enabled || !account.WarmEnabled || !contains(p.Accounts, id) {
				skipped[id] = "warm_pool_scope"
				continue
			}
			poolByAccount[id] = p.ID
		}
		if poolByAccount[id] == "" {
			p := st.Pools["default-"+a.Provider]
			if contains(p.Accounts, id) {
				poolByAccount[id] = p.ID
			}
		}
		p := st.Pools[poolByAccount[id]]
		if reason := s.poolHealthReason(account, p, now); reason != "" {
			skipped[id] = reason
			continue
		}
		if len(p.Models) > 0 && !contains(p.Models, model) {
			skipped[id] = "pool_model"
			continue
		}
		rel, _, _, _ := quotaAvailability(st, id, now, model)
		if rel <= p.ReservePercent/100 {
			skipped[id] = "quota_reserve_or_exhausted"
			continue
		}
		if a.Unavailable && !a.NextRetryAfter.IsZero() && now.Before(a.NextRetryAfter) {
			skipped[id] = "health_cooldown"
			continue
		}
		if state := a.ModelStates[model]; state != nil && state.Unavailable && now.Before(state.NextRetryAfter) {
			skipped[id] = "model_cooldown"
			continue
		}
		eligible = append(eligible, a)
	}
	if len(eligible) == 0 {
		if owner != "" {
			return nil, routingError("continuation_owner_unavailable", "required continuation owner is unavailable within inference key scope")
		}
		return nil, routingError("scope_exhausted", "no eligible account within provider/account/pool scope")
	}
	// Mixed model routes choose a provider first; never score incompatible providers together.
	sort.Slice(eligible, func(i, j int) bool { return eligible[i].Provider+eligible[i].ID < eligible[j].Provider+eligible[j].ID })
	provider := eligible[0].Provider
	var providers []string
	for _, a := range eligible {
		if !contains(providers, a.Provider) {
			providers = append(providers, a.Provider)
		}
	}
	if owner == "" && len(providers) > 1 {
		provider = providers[s.next("provider:"+request.KeyID+":"+model)%uint64(len(providers))]
	}
	providerSet := eligible[:0]
	for _, a := range eligible {
		if a.Provider == provider {
			providerSet = append(providerSet, a)
		}
	}
	eligible = providerSet
	if len(eligible) > 0 {
		p := st.Pools[poolByAccount[st.Credentials[credentialRef(provider, eligible[0].ID)]]]
		if p.Strategy != "" && p.Strategy != "native" {
			strategy = p.Strategy
		}
	}
	stickyRaw := opts.Headers.Get("Session_id")
	if stickyRaw == "" {
		stickyRaw = opts.Headers.Get("X-Session-ID")
	}
	if stickyRaw == "" {
		var root map[string]json.RawMessage
		_ = json.Unmarshal(opts.OriginalRequest, &root)
		_ = json.Unmarshal(root["prompt_cache_key"], &stickyRaw)
	}
	stickyHash := ""
	if stickyRaw != "" && st.Settings.StickySeconds > 0 {
		stickyHash = hash("soft\x00" + request.KeyID + "\x00" + provider + "\x00" + stickyRaw)
	}
	reason := "strategy"
	var chosen *auth.Auth
	if strategy == "single_account" && request.WarmAccount == "" {
		pin := hash("single\x00" + request.KeyID + "\x00" + provider + "\x00" + model)
		id, err := s.owner(pin, true)
		if err != nil {
			return nil, err
		}
		if id == "" {
			id = st.Credentials[credentialRef(provider, eligible[0].ID)]
			if err = s.bind(pin, id, true, 100*365*24*time.Hour); err != nil {
				return nil, err
			}
			// Concurrent first requests must agree with the first committed owner.
			id, err = s.owner(pin, true)
			if err != nil {
				return nil, err
			}
		}
		for _, a := range eligible {
			if st.Credentials[credentialRef(provider, a.ID)] == id {
				chosen = a
				break
			}
		}
		if chosen == nil || (owner != "" && owner != id) {
			return nil, routingError("single_account_unavailable", "pinned single account is unavailable within request scope")
		}
		reason = "single_account_pin"
	} else if owner != "" {
		chosen = eligible[0]
		reason = "hard_continuation"
	} else if stickyHash != "" {
		id, err := s.owner(stickyHash, false)
		if err != nil {
			return nil, err
		}
		for _, a := range eligible {
			logical := st.Credentials[credentialRef(a.Provider, a.ID)]
			remaining, _, _, known := quotaAvailability(st, logical, now, model)
			if logical == id && st.Pools[poolByAccount[logical]].Sticky && (!known || remaining*100 >= st.Settings.StickyMinRemainingPercent) {
				chosen = a
				reason = "soft_sticky"
				break
			}
		}
	}
	if chosen == nil {
		if strategy == "native" && stickyHash == "" {
			if request.Evidence != nil {
				decisions := make(map[string]Decision, len(eligible))
				for _, a := range eligible {
					id := st.Credentials[credentialRef(a.Provider, a.ID)]
					rel, _, reset, known := quotaAvailability(st, id, now, model)
					d := Decision{At: now, Account: id, Pool: poolByAccount[id], Model: model, Strategy: "native", Reason: "native_selector", Skipped: skipped, APIKey: request.KeyID, QuotaKnown: known, ResetAt: reset, InFlight: s.InFlight(id)}
					if known {
						remaining := rel * 100
						d.RemainingPercent = &remaining
					}
					decisions[id] = d
				}
				request.Evidence.mu.Lock()
				request.Evidence.current = Decision{}
				request.Evidence.native = decisions
				request.Evidence.mu.Unlock()
			}
			return eligible, nil
		}
		chosen = chooseWithPools(s, st, eligible, strategy, request.KeyID+":"+provider+":"+model, now, poolByAccount, model)
	}
	id := st.Credentials[credentialRef(chosen.Provider, chosen.ID)]
	if stickyHash != "" && st.Pools[poolByAccount[id]].Sticky {
		if err := s.bind(stickyHash, id, false, time.Duration(st.Settings.StickySeconds)*time.Second); err != nil {
			return nil, err
		}
	}
	decision := Decision{At: now, Account: id, Pool: poolByAccount[id], Model: model, Strategy: strategy, Reason: reason, Skipped: skipped}
	rel, _, reset, known := quotaAvailability(st, id, now, model)
	decision.QuotaKnown = known
	if known {
		remaining := rel * 100
		decision.RemainingPercent = &remaining
	}
	decision.ResetAt = reset
	decision.InFlight = s.InFlight(id)
	decision.APIKey = request.KeyID
	if reason == "strategy" && (strategy == "capacity_weighted" || strategy == "usage_weighted" || strategy == "reset_drain") {
		decision.WarmPhase = s.routingPhase(st, id, poolByAccount[id], model, now)
		if decision.WarmPhase != nil {
			decision.Reason = "strategy_with_bounded_phase_hint"
		}
	}
	if request.Evidence != nil {
		request.Evidence.mu.Lock()
		request.Evidence.current = decision
		request.Evidence.native = nil
		request.Evidence.mu.Unlock()
	}
	data, err := encode(decision)
	if err != nil {
		return nil, err
	}
	if _, err = s.db.Exec("INSERT INTO decisions(at,data) VALUES(?,?)", now.Unix(), data); err != nil {
		return nil, err
	}
	return []*auth.Auth{chosen}, nil
}

func (s *Store) next(key string) uint64 {
	value, _ := s.offsets.LoadOrStore(key, &atomic.Uint64{})
	return value.(*atomic.Uint64).Add(1) - 1
}
func choose(s *Store, st *snapshot, candidates []*auth.Auth, strategy, key string, now time.Time, models ...string) *auth.Auth {
	return chooseWithPools(s, st, candidates, strategy, key, now, nil, models...)
}

func chooseWithPools(s *Store, st *snapshot, candidates []*auth.Auth, strategy, key string, now time.Time, pools map[string]string, models ...string) *auth.Auth {
	if strategy == "round_robin" || strategy == "native" {
		return candidates[s.next(key)%uint64(len(candidates))]
	}
	if strategy == "fill_first" || strategy == "sequential_drain" || strategy == "single_account" {
		return candidates[0]
	}
	best := candidates[0]
	bestScore := -math.MaxFloat64
	for _, a := range candidates {
		id := st.Credentials[credentialRef(a.Provider, a.ID)]
		relative, absolute, reset, _ := quotaAvailability(st, id, now, models...)
		score := absolute
		switch strategy {
		case "relative_availability":
			score = math.Log(math.Max(rand.Float64(), 1e-12)) / math.Max(relative, 1e-6)
		case "usage_weighted":
			score = 1 / (1 + float64(st.Accounts[id].Requests))
		case "reset_drain":
			score = relative
			if !reset.IsZero() {
				score = relative / math.Max(reset.Sub(now).Seconds(), 1)
			}
		}
		if strategy == "capacity_weighted" || strategy == "usage_weighted" || strategy == "reset_drain" {
			score /= 1 + st.Settings.InFlightPenalty*float64(s.InFlight(id))
			score /= 1 + st.Settings.FailurePenalty*float64(a.Failed)/math.Max(float64(a.Success+a.Failed), 1)
			model := ""
			if len(models) > 0 {
				model = models[0]
			}
			if phase := s.routingPhase(st, id, pools[id], model, now); phase != nil {
				score *= 1 + phase.Bonus
			}
		}
		if score > bestScore {
			best = a
			bestScore = score
		}
	}
	return best
}

func (s *Store) ObserveResponse(_ context.Context, a *auth.Auth, _ executor.Options, payload []byte) error {
	if a.Provider != "codex" {
		return nil
	}
	id := s.state.Load().Credentials[credentialRef(a.Provider, a.ID)]
	if id == "" {
		return errors.New("continuation account not imported")
	}
	for _, line := range bytes.Split(payload, []byte("\n")) {
		line = bytes.TrimSpace(line)
		line = bytes.TrimPrefix(line, []byte("data:"))
		var root map[string]json.RawMessage
		if json.Unmarshal(line, &root) != nil {
			continue
		}
		if response := root["response"]; len(response) > 0 {
			if err := json.Unmarshal(response, &root); err != nil {
				continue
			}
		}
		var responseID string
		_ = json.Unmarshal(root["id"], &responseID)
		_, hasOutput := root["output"]
		if responseID != "" && (strings.HasPrefix(responseID, "resp_") || hasOutput) {
			if err := s.bind(objectHash(affinityObject{"response", responseID}), id, true, 30*24*time.Hour); err != nil {
				return err
			}
		}
		var conversationID string
		_ = json.Unmarshal(root["conversation_id"], &conversationID)
		if conversationID != "" {
			if err := s.bind(objectHash(affinityObject{"conversation", conversationID}), id, true, 30*24*time.Hour); err != nil {
				return err
			}
		}
		objectPayload, err := json.Marshal(root)
		if err != nil {
			return err
		}
		bound, _ := objects(objectPayload)
		for _, o := range bound {
			if o.kind == "encrypted" || o.kind == "file_id" || o.kind == "resource_id" || o.kind == "conversation" {
				if err := s.bind(objectHash(o), id, true, 30*24*time.Hour); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
