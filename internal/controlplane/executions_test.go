package controlplane

import (
	"context"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/usage"
)

func TestExecutionJournalUnresolvedAndTrustworthySettlement(t *testing.T) {
	s, auths := testStore(t)
	ctx := usage.WithExecutionRequestID(WithRequest(context.Background(), RequestContext{}), "pending-execution")
	release := s.BeginExecution(ctx, auths[0], executor.Options{})
	release()
	release()
	rows, err := s.UnresolvedExecutions(ctx)
	if err != nil || len(rows) != 1 || rows[0].State != "unresolved_no_usage" {
		t.Fatalf("missing unresolved execution: %v %v", rows, err)
	}
	if s.InFlight(logical(s, auths[0])) != 0 {
		t.Fatal("journal leaked flight")
	}
	if err = s.RecordUsage(usage.Record{RequestID: "pending-execution", Provider: "codex", AuthID: auths[0].ID, Model: "model", Detail: usage.Detail{TotalTokens: 7}}); err != nil {
		t.Fatal(err)
	}
	rows, err = s.UnresolvedExecutions(ctx)
	if err != nil || len(rows) != 0 {
		t.Fatal("real late usage did not resolve journal")
	}
	if err = s.startExecution(ctx, "crash-before-usage", logical(s, auths[0]), "codex"); err != nil {
		t.Fatal(err)
	}
	path := s.path
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	restarted, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = restarted.Close() }()
	rows, err = restarted.UnresolvedExecutions(ctx)
	if err != nil || len(rows) != 1 || rows[0].State != "unresolved_restart" {
		t.Fatal("crash guessed usage instead of preserving unresolved evidence")
	}
	logs, err := restarted.RequestLogs(ctx, LogFilter{})
	if err != nil || len(logs) != 1 || logs[0].Total != 7 {
		t.Fatal("unresolved execution fabricated billable usage")
	}
}

func TestExecutionRetentionKeepsUnresolvedAndDoesNotReopenSettlements(t *testing.T) {
	s, auths := testStore(t)
	ctx := context.Background()
	account := logical(s, auths[0])
	for _, id := range []string{"settled", "unresolved"} {
		if err := s.startExecution(ctx, id, account, "codex"); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.RecordUsage(usage.Record{RequestID: "settled", Provider: "codex", AuthID: auths[0].ID, Model: "model", Detail: usage.Detail{TotalTokens: 7}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec("UPDATE executions SET state='unresolved_no_usage' WHERE id='unresolved'"); err != nil {
		t.Fatal(err)
	}
	now := s.now().Add(60 * 24 * time.Hour)
	s.now = func() time.Time { return now }
	if err := s.Retain(30); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM executions").Scan(&count); err != nil || count != 1 {
		t.Fatalf("settled journal retention: %d %v", count, err)
	}
	if err := s.startExecution(ctx, "settled", account, "codex"); err != nil {
		t.Fatal(err)
	}
	rows, err := s.UnresolvedExecutions(ctx)
	if err != nil || len(rows) != 1 || rows[0].ID != "unresolved" {
		t.Fatalf("retention lost recovery evidence: %v %v", rows, err)
	}
	if err := s.db.QueryRow("SELECT COUNT(*) FROM executions WHERE id='settled'").Scan(&count); err != nil || count != 0 {
		t.Fatal("replayed settlement reopened an already-settled execution")
	}
}
