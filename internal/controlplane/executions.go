package controlplane

import (
	"context"
	"time"
)

type Execution struct {
	ID       string    `json:"id"`
	At       time.Time `json:"at"`
	Account  string    `json:"account"`
	Provider string    `json:"provider"`
	Model    string    `json:"model"`
	APIKey   string    `json:"api_key"`
	State    string    `json:"state"`
}

func (s *Store) startExecution(ctx context.Context, id, account, provider string) error {
	// Only bounded identifiers persist. No body, auth metadata or raw key.
	r := RequestFromContext(ctx)
	model := ""
	if r.Evidence != nil {
		r.Evidence.mu.Lock()
		model = r.Evidence.current.Model
		if e, ok := r.Evidence.executions[id]; ok {
			model = e.Decision.Model
		}
		r.Evidence.mu.Unlock()
	}
	_, err := s.db.Exec("INSERT OR IGNORE INTO executions(id,at,account,provider,model,api_key,state) SELECT ?,?,?,?,?,?,'running' WHERE NOT EXISTS (SELECT 1 FROM settlements WHERE id=?)", id, s.now().Unix(), account, provider, model, r.KeyID, id)
	return err
}

func (s *Store) UnresolvedExecutions(ctx context.Context) ([]Execution, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id,at,account,provider,model,api_key,state FROM executions WHERE state IN ('unresolved_restart','unresolved_no_usage') ORDER BY at DESC,id DESC LIMIT 100")
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []Execution{}
	for rows.Next() {
		var v Execution
		var at int64
		if err = rows.Scan(&v.ID, &at, &v.Account, &v.Provider, &v.Model, &v.APIKey, &v.State); err != nil {
			return nil, err
		}
		v.At = time.Unix(at, 0).UTC()
		out = append(out, v)
	}
	return out, rows.Err()
}
