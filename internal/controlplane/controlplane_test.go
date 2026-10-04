package controlplane

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/usage"
)

func testStore(t *testing.T) (*Store, []*auth.Auth) {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "controlplane.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if !s.closed.Load() {
			if err := s.Close(); err != nil {
				t.Error(err)
			}
		}
	})
	s.now = func() time.Time { return time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC) }
	auths := []*auth.Auth{{ID: "a", Provider: "codex", Label: "A", Metadata: map[string]any{"account_id": "subject-a", "workspace_id": "workspace-a"}}, {ID: "b", Provider: "codex", Label: "B", Metadata: map[string]any{"account_id": "subject-b", "workspace_id": "workspace-b"}}, {ID: "c", Provider: "claude", Label: "C"}}
	if err = s.SyncAccounts(auths); err != nil {
		t.Fatal(err)
	}
	return s, auths
}

func logical(s *Store, a *auth.Auth) string {
	return s.state.Load().Credentials[credentialRef(a.Provider, a.ID)]
}
func scopedKey(t *testing.T, s *Store, ids ...string) (APIKey, string) {
	t.Helper()
	k, secret, err := s.CreateKey(APIKey{Name: "test", Bindings: map[string]Binding{"codex": {Accounts: ids}}})
	if err != nil {
		t.Fatal(err)
	}
	return k, secret
}

func TestSQLitePersistenceWALForeignKeysAndBackup(t *testing.T) {
	s, auths := testStore(t)
	id := logical(s, auths[0])
	a := s.state.Load().Accounts[id]
	a.Paused = true
	if err := s.SaveAccount(a); err != nil {
		t.Fatal(err)
	}
	var mode string
	var fk, version int
	if err := s.db.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
		t.Fatal(err)
	}
	_ = s.db.QueryRow("PRAGMA foreign_keys").Scan(&fk)
	_ = s.db.QueryRow("PRAGMA user_version").Scan(&version)
	if mode != "wal" || fk != 1 || version != schemaVersion {
		t.Fatalf("pragmas: %s %d %d", mode, fk, version)
	}
	if _, err := s.db.Exec("INSERT INTO credentials(ref,identity,account) VALUES('bad','bad','missing')"); err == nil {
		t.Fatal("foreign key accepted missing account")
	}
	backup, err := s.Backup()
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(filepath.Dir(s.path), backup))
	if err != nil {
		t.Fatal(err)
	}
	var count int
	if err = db.QueryRow("SELECT count(*) FROM entities WHERE kind='account'").Scan(&count); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	if count != 3 {
		t.Fatalf("backup lost WAL data: %d", count)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s2, err := Open(s.path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s2.Close() }()
	if !s2.state.Load().Accounts[id].Paused {
		t.Fatal("account pause did not survive restart")
	}
}

func TestMigrationBacksUpExistingV1(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.sqlite")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(schema); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("INSERT INTO entities(kind,id,data) VALUES('settings','routing','{\"strategy\":\"round_robin\",\"quota_fresh_seconds\":900}')"); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	files, err := filepath.Glob(path + ".backup-*.sqlite")
	if err != nil || len(files) != 1 {
		t.Fatalf("missing pre-migration backup %v %v", files, err)
	}
	old, err := sql.Open("sqlite", files[0])
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = old.Close() }()
	var v int
	_ = old.QueryRow("PRAGMA user_version").Scan(&v)
	if v != 1 {
		t.Fatalf("backup version = %d", v)
	}
	if s.state.Load().Settings.Strategy != "round_robin" {
		t.Fatal("migration lost settings")
	}
}

func TestStableAccountIdentityRefreshReloginAndWorkspaces(t *testing.T) {
	s, auths := testStore(t)
	id := logical(s, auths[0])
	a := s.state.Load().Accounts[id]
	a.WarmEnabled = true
	if err := s.SaveAccount(a); err != nil {
		t.Fatal(err)
	}
	updated := auths[0].Clone()
	updated.ID = "rewritten-login"
	updated.Label = "renamed"
	updated.Metadata["access_token"] = "DO-NOT-PERSIST"
	if err := s.SyncAccounts([]*auth.Auth{updated}); err != nil {
		t.Fatal(err)
	}
	if logical(s, updated) != id || !s.state.Load().Accounts[id].WarmEnabled {
		t.Fatal("identity/metadata lost across login")
	}
	other := updated.Clone()
	other.ID = "other-workspace"
	other.Metadata["workspace_id"] = "other"
	if err := s.SyncAccounts([]*auth.Auth{other}); err != nil {
		t.Fatal(err)
	}
	if logical(s, other) == id {
		t.Fatal("workspaces collapsed")
	}
	var count int
	_ = s.db.QueryRow("SELECT count(*) FROM entities WHERE data LIKE '%DO-NOT-PERSIST%'").Scan(&count)
	if count != 0 {
		t.Fatal("OAuth token persisted")
	}
}

func TestDefaultPoolImportsEveryAccountWithoutReaddingRemovedMember(t *testing.T) {
	s, auths := testStore(t)
	pool := s.state.Load().Pools["default-codex"]
	if len(pool.Accounts) != 2 {
		t.Fatalf("default pool lost members: %v", pool.Accounts)
	}
	pool.Accounts = []string{logical(s, auths[0])}
	if err := s.SavePool(pool); err != nil {
		t.Fatal(err)
	}
	if err := s.SyncAccounts(auths); err != nil {
		t.Fatal(err)
	}
	if len(s.state.Load().Pools[pool.ID].Accounts) != 1 {
		t.Fatal("sync restored explicitly removed pool member")
	}
}

func TestKeyScopeFailoverPoolUpdateAndRevocation(t *testing.T) {
	s, auths := testStore(t)
	idA, idB := logical(s, auths[0]), logical(s, auths[1])
	key, secret := scopedKey(t, s, idA)
	ctx := WithRequest(context.Background(), RequestContext{KeyID: key.ID})
	selected, err := s.FilterCandidates(ctx, "model", executor.Options{}, auths)
	if err != nil || len(selected) != 1 || selected[0].ID != "a" {
		t.Fatalf("scope selection: %v %v", selected, err)
	}
	if _, err = s.FilterCandidates(ctx, "model", executor.Options{}, auths[1:]); err == nil {
		t.Fatal("failover escaped key scope")
	}
	pool := Pool{ID: "test", Name: "test", Provider: "codex", Accounts: []string{idA}, Strategy: "round_robin", Sticky: true}
	if err = s.SavePool(pool); err != nil {
		t.Fatal(err)
	}
	key.Bindings = map[string]Binding{"codex": {Pools: []string{pool.ID}}}
	if err = s.SaveKey(key); err != nil {
		t.Fatal(err)
	}
	pool.Accounts = []string{idB}
	if err = s.SavePool(pool); err != nil {
		t.Fatal(err)
	}
	selected, err = s.FilterCandidates(ctx, "model", executor.Options{}, auths)
	if err != nil || selected[0].ID != "b" {
		t.Fatalf("membership not invalidated: %v %v", selected, err)
	}
	key.Revoked = true
	if err = s.SaveKey(key); err != nil {
		t.Fatal(err)
	}
	if _, err = s.FilterCandidates(ctx, "model", executor.Options{}, auths); err == nil {
		t.Fatal("revoked key selected an account")
	}
	if _, err = s.Admit(secret); err == nil {
		t.Fatal("revoked key admitted")
	}
	rows, err := s.entities(context.Background(), "key")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(rows[0]), secret) {
		t.Fatal("key secret exposed")
	}
}

func TestKeyAtomicRequestLimit(t *testing.T) {
	s, auths := testStore(t)
	key, secret := scopedKey(t, s, logical(s, auths[0]))
	key.RequestLimit = 5
	if err := s.SaveKey(key); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	success := 0
	for range 20 {
		wg.Go(func() {
			if _, err := s.Admit(secret); err == nil {
				mu.Lock()
				success++
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	if success != 5 {
		t.Fatalf("request limit admitted %d", success)
	}
}

func TestQuotaRoutingFreshStaleResetAndCapacity(t *testing.T) {
	s, auths := testStore(t)
	settings := s.state.Load().Settings
	settings.Strategy = "capacity_weighted"
	if err := s.SaveSettings(settings); err != nil {
		t.Fatal(err)
	}
	now := s.now()
	usedA, usedB := 100.0, 20.0
	idA, idB := logical(s, auths[0]), logical(s, auths[1])
	q := []QuotaWindow{{Account: idA, Provider: "codex", Key: "5h", UsedPercent: &usedA, ObservedAt: now, ResetAt: now.Add(time.Hour), Source: "test"}, {Account: idB, Provider: "codex", Key: "weekly", UsedPercent: &usedB, ObservedAt: now, ResetAt: now.Add(7 * 24 * time.Hour), Source: "test"}}
	if err := s.ObserveQuota(q); err != nil {
		t.Fatal(err)
	}
	selected, err := s.FilterCandidates(context.Background(), "model", executor.Options{}, auths[:2])
	if err != nil || selected[0].ID != "b" {
		t.Fatalf("exhausted account selected %v %v", selected, err)
	}
	q[0].ObservedAt = now.Add(time.Second)
	q[0].ResetAt = now.Add(-time.Second)
	if err = s.ObserveQuota(q[:1]); err != nil {
		t.Fatal(err)
	}
	selected, err = s.FilterCandidates(context.Background(), "model", executor.Options{}, auths[:2])
	if err != nil || selected[0].ID != "a" {
		t.Fatalf("expired evidence excluded account %v %v", selected, err)
	}
	q[0].ObservedAt = now.Add(-time.Hour)
	q[0].ResetAt = now.Add(time.Hour)
	q[0].Key = "stale"
	if err = s.ObserveQuota(q[:1]); err != nil {
		t.Fatal(err)
	}
	rel, _, _, _ := quotaAvailability(s.state.Load(), idA, now)
	if rel != 1 {
		t.Fatalf("stale evidence treated as current: %f", rel)
	}
	if err := s.ObserveQuota([]QuotaWindow{{Account: idB, Provider: "codex", Key: "5h", UsedPercent: &usedA, ObservedAt: now, ResetAt: now.Add(time.Hour), Source: "test"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.FilterCandidates(context.Background(), "model", executor.Options{}, auths[1:2]); err == nil {
		t.Fatal("secondary headroom bypassed primary exhaustion")
	}
}

func TestProviderQuotaAdapters(t *testing.T) {
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	h := http.Header{}
	h.Set("x-codex-primary-used-percent", "50")
	h.Set("x-codex-primary-window-minutes", "300")
	h.Set("x-codex-primary-reset-after-seconds", "3600")
	h.Set("x-codex-secondary-used-percent", "80")
	h.Set("x-codex-secondary-window-minutes", "43200")
	q := ParseQuota("codex", "a", h, now)
	if len(q) != 2 || q[0].Key != "5h" || q[1].Key != "monthly" || !q[0].ResetAt.Equal(now.Add(time.Hour)) {
		t.Fatalf("codex windows: %#v", q)
	}
	h = http.Header{}
	h.Set("anthropic-ratelimit-unified-7d-utilization", "0.25")
	q = ParseQuota("claude", "b", h, now)
	if len(q) != 1 || *q[0].UsedPercent != 25 || q[0].Key != "7d" {
		t.Fatalf("Claude native windows: %#v", q)
	}
	if len(ParseQuota("claude", "b", http.Header{}, now)) != 0 {
		t.Fatal("invented quota")
	}
	q, err := ParseQuotaBody("codex", "a", []byte(`{"rate_limit":{"primary_window":{"used_percent":5,"limit_window_seconds":18000},"secondary_window":{"used_percent":10,"limit_window_seconds":604800}},"additional_rate_limits":[{"limit_name":"model","rate_limit":{"primary_window":{"used_percent":1}}}]}`), now)
	if err != nil || len(q) != 3 || q[1].Key != "weekly" {
		t.Fatalf("quota body %v %v", q, err)
	}
	q, err = ParseQuotaBody("claude", "b", []byte(`{"five_hour":{"utilization":40,"resets_at":"2026-10-02T15:00:00Z"},"seven_day_opus":{"utilization":2}}`), now)
	if err != nil || len(q) != 2 {
		t.Fatalf("claude body %v %v", q, err)
	}
}

func TestSoftStickyHardAffinityAndSensitiveHashes(t *testing.T) {
	s, auths := testStore(t)
	key, _ := scopedKey(t, s, logical(s, auths[0]), logical(s, auths[1]))
	ctx := WithRequest(context.Background(), RequestContext{KeyID: key.ID})
	opts := executor.Options{Headers: http.Header{"X-Session-Id": []string{"sensitive-session"}}}
	first, err := s.FilterCandidates(ctx, "model", opts, auths[:2])
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.FilterCandidates(ctx, "model", opts, auths[:2])
	if err != nil || first[0].ID != second[0].ID {
		t.Fatal("soft sticky not stable")
	}
	a := s.state.Load().Accounts[logical(s, first[0])]
	a.Paused = true
	if err = s.SaveAccount(a); err != nil {
		t.Fatal(err)
	}
	second, err = s.FilterCandidates(ctx, "model", opts, auths[:2])
	if err != nil || second[0].ID == first[0].ID {
		t.Fatal("soft sticky did not reallocate")
	}
	if err = s.ObserveResponse(ctx, first[0], opts, []byte(`data: {"type":"response.created","response":{"id":"resp_sensitive-owner"}}`)); err != nil {
		t.Fatal(err)
	}
	hard := executor.Options{OriginalRequest: []byte(`{"previous_response_id":"resp_sensitive-owner"}`)}
	if _, err = s.FilterCandidates(ctx, "model", hard, auths[:2]); err == nil || !strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("hard affinity moved silently: %v", err)
	}
	a.Paused = false
	if err = s.SaveAccount(a); err != nil {
		t.Fatal(err)
	}
	owned, err := s.FilterCandidates(ctx, "model", hard, auths[:2])
	if err != nil || owned[0].ID != first[0].ID {
		t.Fatal("hard owner lost")
	}
	var raw string
	if err = s.db.QueryRow("SELECT group_concat(hash) FROM affinity").Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(raw, "sensitive") {
		t.Fatal("raw session/response IDs persisted")
	}
	for _, body := range []string{`{"previous_response_id":"resp_unknown"}`, `{"input":[{"file_id":"file_unknown"}]}`, `{"input":[{"encrypted_content":"secret"}]}`} {
		if _, err = s.FilterCandidates(ctx, "model", executor.Options{OriginalRequest: []byte(body)}, auths[:2]); err == nil {
			t.Fatalf("unknown owner accepted: %s", body)
		}
	}
}

func TestTelemetryIdempotentRollupRedactionAndRetention(t *testing.T) {
	s, auths := testStore(t)
	key, secret := scopedKey(t, s, logical(s, auths[0]))
	r := usage.Record{RequestID: "request-1", Provider: "codex", AuthID: "a", APIKey: secret, SessionID: "raw-session", RequestedAt: s.now(), Model: "m", Latency: 10 * time.Millisecond, Detail: usage.Detail{InputTokens: 2, OutputTokens: 3, TotalTokens: 5}, Fail: usage.Failure{Body: "secret-provider-error"}}
	for range 2 {
		if err := s.RecordUsage(r); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := s.RequestLogs(context.Background(), LogFilter{})
	if err != nil || len(rows) != 1 {
		t.Fatalf("duplicate settlement %v %v", rows, err)
	}
	if s.state.Load().Keys[key.ID].Tokens != 5 {
		t.Fatal("tokens settled twice")
	}
	rollups, err := s.Analytics(context.Background(), "")
	if err != nil || len(rollups) != 1 || rollups[0].Requests != 1 || rollups[0].Tokens != 5 {
		t.Fatal("rollups not idempotent")
	}
	data, _ := json.Marshal(rows)
	for _, sensitive := range []string{secret, "raw-session", "secret-provider-error"} {
		if strings.Contains(string(data), sensitive) {
			t.Fatal("sensitive telemetry exposed")
		}
	}
	s.now = func() time.Time { return r.RequestedAt.AddDate(0, 0, 40) }
	if err = s.Retain(30); err != nil {
		t.Fatal(err)
	}
	rows, err = s.RequestLogs(context.Background(), LogFilter{})
	if err != nil || len(rows) != 0 {
		t.Fatal("retention failed")
	}
	rollups, err = s.Analytics(context.Background(), "2026-01-01")
	if err != nil || len(rollups) != 1 {
		t.Fatal("raw retention lost rollups")
	}
	if err = s.RecordUsage(r); err != nil {
		t.Fatal(err)
	}
	rollups, err = s.Analytics(context.Background(), "2026-01-01")
	if err != nil || len(rollups) != 1 || rollups[0].Requests != 1 || s.state.Load().Keys[key.ID].Tokens != 5 {
		t.Fatal("retention allowed duplicate accounting settlement")
	}
}

func TestAdminAuthAndInferenceSeparation(t *testing.T) {
	s, auths := testStore(t)
	_, inference := scopedKey(t, s, logical(s, auths[0]))
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	admin := strings.Repeat("a", 32)
	s.RegisterRoutes(engine, admin)
	for _, test := range []struct {
		secret string
		want   int
	}{{"", 401}, {inference, 401}, {admin, 200}} {
		req := httptest.NewRequest("GET", "/api/control-plane/state", nil)
		req.Header.Set("Authorization", "Bearer "+test.secret)
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, req)
		if w.Code != test.want {
			t.Fatalf("auth status %d want %d", w.Code, test.want)
		}
	}
}

func TestDatabaseFilesPrivate(t *testing.T) {
	s, _ := testStore(t)
	info, err := os.Stat(s.path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("database mode %o", info.Mode().Perm())
	}
}
