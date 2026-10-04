package controlplane

import (
	"context"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/usage"
)

func TestReauthGuardRequiresNativePrincipalWorkspaceNotEmail(t *testing.T) {
	s, auths := testStore(t)
	id := logical(s, auths[0])
	validate, err := s.ReauthValidator(id, "codex")
	if err != nil {
		t.Fatal(err)
	}
	same := auths[0].Clone()
	same.ID = "new-reference"
	same.Label = "renamed"
	if err = validate(same); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"account_id", "workspace_id"} {
		different := same.Clone()
		different.Metadata[field] = "different"
		if err = validate(different); err == nil {
			t.Fatalf("accepted different %s", field)
		}
	}
	if err = validate(&auth.Auth{ID: "new-reference", Provider: "codex", Metadata: map[string]any{"email": "same@example.invalid"}}); err == nil {
		t.Fatal("email matched without native evidence")
	}
	if _, err = s.ReauthValidator(logical(s, auths[2]), "claude"); err == nil {
		t.Fatal("identity-less target was treated as verified")
	}
	if _, err = s.ReauthValidator(id, "claude"); err == nil {
		t.Fatal("cross-provider target accepted")
	}
}

func TestValidatedReauthChangedReferenceRetainsScopeHistoryAndOwner(t *testing.T) {
	s, auths := testStore(t)
	id := logical(s, auths[0])
	p := warmPool(t, s, auths)
	a := s.state.Load().Accounts[id]
	a.Paused = true
	if err := s.SaveAccount(a); err != nil {
		t.Fatal(err)
	}
	key, _ := scopedKey(t, s, id)
	if err := s.ObserveResponse(context.Background(), auths[0], executor.Options{}, []byte(`{"id":"resp_reauth_owner","output":[]}`)); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordUsage(usage.Record{RequestID: "reauth-usage", Provider: "codex", AuthID: auths[0].ID, Model: "model", Detail: usage.Detail{TotalTokens: 7}}); err != nil {
		t.Fatal(err)
	}
	validate, err := s.ReauthValidator(id, "codex")
	if err != nil {
		t.Fatal(err)
	}
	fresh := auths[0].Clone()
	fresh.ID, fresh.Label = "new-login-ref", "changed display"
	if err = validate(fresh); err != nil {
		t.Fatal(err)
	}
	if err = s.SyncAccounts([]*auth.Auth{fresh, auths[1], auths[2]}); err != nil {
		t.Fatal(err)
	}
	current := s.state.Load().Accounts[id]
	if logical(s, fresh) != id || !current.Paused || !current.WarmEnabled || current.Requests != 1 || !contains(s.state.Load().Pools[p.ID].Accounts, id) || !contains(s.state.Load().Keys[key.ID].Bindings["codex"].Accounts, id) {
		t.Fatal("verified reauth lost logical metadata, history or scope")
	}
	owner, err := s.owner(objectHash(affinityObject{"response", "resp_reauth_owner"}), true)
	if err != nil || owner != id {
		t.Fatal("reauth lost hard continuation owner")
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
	if logical(restarted, fresh) != id || !restarted.state.Load().Accounts[id].Paused {
		t.Fatal("restart lost validated continuity")
	}
}
