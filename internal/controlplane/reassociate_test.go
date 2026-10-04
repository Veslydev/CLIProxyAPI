package controlplane

import (
	"context"
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
)

func TestExplicitFallbackReauthenticationKeepsLogicalMetadataAndScope(t *testing.T) {
	s, auths := testStore(t)
	old := logical(s, auths[2])
	account := s.state.Load().Accounts[old]
	account.WarmEnabled = true
	account.Paused = true
	if err := s.SaveAccount(account); err != nil {
		t.Fatal(err)
	}
	p := Pool{ID: "claude-personal", Name: "personal", Provider: "claude", Strategy: "native", Accounts: []string{old}}
	if err := s.SavePool(p); err != nil {
		t.Fatal(err)
	}
	key, _, err := s.CreateKey(APIKey{Name: "claude", Bindings: map[string]Binding{"claude": {Accounts: []string{old}}}})
	if err != nil {
		t.Fatal(err)
	}
	fresh := &auth.Auth{ID: "new-login", Provider: "claude", Label: "New label"}
	if err = s.SyncAccounts([]*auth.Auth{fresh}); err != nil {
		t.Fatal(err)
	}
	source := logical(s, fresh)
	freshAccount := s.state.Load().Accounts[source]
	freshAccount.Paused = true
	if err = s.SaveAccount(freshAccount); err != nil {
		t.Fatal(err)
	}
	if source == old {
		t.Fatal("unverified login auto-linked")
	}
	if err = s.ReassociateCredential(old, source, "wrong"); err == nil {
		t.Fatal("missing confirmation accepted")
	}
	if err = s.ReassociateCredential(old, source, old); err != nil {
		t.Fatal(err)
	}
	if err = s.SyncAccounts([]*auth.Auth{fresh}); err != nil {
		t.Fatal(err)
	}
	if logical(s, fresh) != old {
		t.Fatal("refresh lost explicit association")
	}
	st := s.state.Load()
	if !st.Accounts[old].Paused || !st.Accounts[old].WarmEnabled || st.Accounts[old].IdentityEvidence != "operator_confirmed" || st.Accounts[source].ReplacedBy != old || !contains(st.Pools[p.ID].Accounts, old) {
		t.Fatal("logical metadata lost")
	}
	ctx := WithRequest(context.Background(), RequestContext{KeyID: key.ID})
	if _, err = s.FilterCandidates(ctx, "model", executor.Options{}, []*auth.Auth{fresh}); err == nil {
		t.Fatal("paused owner became available")
	}
	account = st.Accounts[old]
	account.Paused = false
	if err = s.SaveAccount(account); err != nil {
		t.Fatal(err)
	}
	if candidates, err := s.FilterCandidates(ctx, "model", executor.Options{}, []*auth.Auth{fresh, auths[0]}); err != nil || len(candidates) != 1 || candidates[0].ID != fresh.ID {
		t.Fatalf("scope continuity: %v %v", candidates, err)
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
	if err = restarted.SyncAccounts([]*auth.Auth{fresh}); err != nil {
		t.Fatal(err)
	}
	if logical(restarted, fresh) != old {
		t.Fatal("restart lost explicit reauthentication")
	}
	fresh.Metadata = map[string]any{"account_uuid": "different-verified-principal"}
	if err = restarted.SyncAccounts([]*auth.Auth{fresh}); err != nil {
		t.Fatal(err)
	}
	if logical(restarted, fresh) == old {
		t.Fatal("different verified principal inherited manually confirmed scope")
	}
}

func TestReassociationHTTPRequiresSeparateAdminAndAudits(t *testing.T) {
	s, auths := testStore(t)
	target := logical(s, auths[2])
	fresh := &auth.Auth{ID: "http-replacement", Provider: "claude"}
	if err := s.SyncAccounts([]*auth.Auth{fresh}); err != nil {
		t.Fatal(err)
	}
	source := logical(s, fresh)
	a := s.state.Load().Accounts[source]
	a.Paused = true
	if err := s.SaveAccount(a); err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	admin := strings.Repeat("fixture-admin-", 4)
	s.RegisterRoutes(engine, admin)
	body := `{"source":"` + source + `","confirmation":"` + target + `"}`
	for _, credential := range []string{"", "fixture-inference", admin} {
		req := httptest.NewRequest("POST", "/api/control-plane/accounts/"+target+"/reassociate", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+credential)
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, req)
		want := 401
		if credential == admin {
			want = 200
		}
		if response.Code != want {
			t.Fatalf("admin separation: got %d want %d", response.Code, want)
		}
	}
	var count int
	if err := s.db.QueryRow("SELECT count(*) FROM audit WHERE action='account.reassociate' AND target=?", target+":"+source).Scan(&count); err != nil || count != 1 {
		t.Fatalf("reassociation audit: %d %v", count, err)
	}
}

func TestReassociationRejectsVerifiedPrincipalAndManagedReplacement(t *testing.T) {
	s, auths := testStore(t)
	target := logical(s, auths[2])
	if err := s.ReassociateCredential(target, logical(s, auths[0]), target); err == nil {
		t.Fatal("cross-provider relink accepted")
	}
	verified := &auth.Auth{ID: "other-claude", Provider: "claude", Metadata: map[string]any{"account_uuid": "different"}}
	if err := s.SyncAccounts([]*auth.Auth{verified}); err != nil {
		t.Fatal(err)
	}
	if err := s.ReassociateCredential(target, logical(s, verified), target); err == nil {
		t.Fatal("verified principal relinked")
	}
	fresh := &auth.Auth{ID: "unused", Provider: "claude"}
	if err := s.SyncAccounts([]*auth.Auth{fresh}); err != nil {
		t.Fatal(err)
	}
	source := logical(s, fresh)
	freshAccount := s.state.Load().Accounts[source]
	freshAccount.Paused = true
	if err := s.SaveAccount(freshAccount); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.CreateKey(APIKey{Name: "owned", Bindings: map[string]Binding{"claude": {Accounts: []string{source}}}}); err != nil {
		t.Fatal(err)
	}
	if err := s.ReassociateCredential(target, source, target); err == nil {
		t.Fatal("existing key scope silently migrated")
	}
	if logical(s, fresh) != source {
		t.Fatal("failed transaction changed mapping")
	}
}
