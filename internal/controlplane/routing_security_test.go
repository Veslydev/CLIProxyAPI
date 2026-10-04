package controlplane

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
)

func TestRoutingPenaltiesCanBeConfigured(t *testing.T) {
	s, auths := testStore(t)
	st := s.state.Load()
	first := auths[0].Clone()
	first.Failed = 10
	first.Success = 0
	candidates := []*auth.Auth{first, auths[1]}
	if got := choose(s, st, candidates, "capacity_weighted", "test", s.now()); got.ID != auths[1].ID {
		t.Fatal("failure penalty ignored")
	}
	settings := st.Settings
	settings.FailurePenalty = 0
	if err := s.SaveSettings(settings); err != nil {
		t.Fatal(err)
	}
	st = s.state.Load()
	if got := choose(s, st, candidates, "capacity_weighted", "test", s.now()); got.ID != first.ID {
		t.Fatal("disabled failure penalty still applied")
	}
	release := s.BeginExecution(context.Background(), first, executor.Options{})
	defer release()
	if got := choose(s, st, candidates, "capacity_weighted", "test", s.now()); got.ID != auths[1].ID {
		t.Fatal("in-flight penalty ignored")
	}
	settings.InFlightPenalty = 0
	if err := s.SaveSettings(settings); err != nil {
		t.Fatal(err)
	}
	if got := choose(s, s.state.Load(), candidates, "capacity_weighted", "test", s.now()); got.ID != first.ID {
		t.Fatal("disabled in-flight penalty still applied")
	}
}

func TestStickyHeadroomThresholdDoesNotMoveHardOwner(t *testing.T) {
	s, auths := testStore(t)
	settings := s.state.Load().Settings
	settings.Strategy = "capacity_weighted"
	settings.StickyMinRemainingPercent = 20
	if err := s.SaveSettings(settings); err != nil {
		t.Fatal(err)
	}
	key, _ := scopedKey(t, s, logical(s, auths[0]), logical(s, auths[1]))
	ctx := WithRequest(context.Background(), RequestContext{KeyID: key.ID})
	opts := executor.Options{Headers: http.Header{"X-Session-Id": []string{"headroom-test"}}}
	first, err := s.FilterCandidates(ctx, "model", opts, auths[:2])
	if err != nil {
		t.Fatal(err)
	}
	used := 90.0
	if err = s.ObserveQuota([]QuotaWindow{{Account: logical(s, first[0]), Provider: "codex", Key: "5h", UsedPercent: &used, ObservedAt: s.now(), ResetAt: s.now().Add(time.Hour), Source: "test"}}); err != nil {
		t.Fatal(err)
	}
	next, err := s.FilterCandidates(ctx, "model", opts, auths[:2])
	if err != nil || next[0].ID == first[0].ID {
		t.Fatalf("sticky headroom threshold ignored: %v", err)
	}
	if err = s.ObserveResponse(ctx, first[0], opts, []byte(`{"id":"resp_threshold_owner","output":[]}`)); err != nil {
		t.Fatal(err)
	}
	hard, err := s.FilterCandidates(ctx, "model", executor.Options{OriginalRequest: []byte(`{"previous_response_id":"resp_threshold_owner"}`)}, auths[:2])
	if err != nil || hard[0].ID != first[0].ID {
		t.Fatalf("soft threshold moved hard owner: %v", err)
	}
}

func TestSingleAccountPinSurvivesExhaustionScopeChangeAndRestart(t *testing.T) {
	s, auths := testStore(t)
	p := s.state.Load().Pools["default-codex"]
	p.Strategy = "single_account"
	if err := s.SavePool(p); err != nil {
		t.Fatal(err)
	}
	key, _ := scopedKey(t, s, logical(s, auths[0]), logical(s, auths[1]))
	ctx := WithRequest(context.Background(), RequestContext{KeyID: key.ID})
	first, err := s.FilterCandidates(ctx, "model", executor.Options{}, auths[:2])
	if err != nil {
		t.Fatal(err)
	}
	id := logical(s, first[0])
	used := 100.0
	if err = s.ObserveQuota([]QuotaWindow{{Account: id, Provider: "codex", Key: "5h", UsedPercent: &used, ObservedAt: s.now(), ResetAt: s.now().Add(time.Hour), Source: "test"}}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.FilterCandidates(ctx, "model", executor.Options{}, auths[:2]); err == nil || !strings.Contains(err.Error(), "single_account_unavailable") {
		t.Fatalf("single account silently failed over: %v", err)
	}
	path, now := s.path, s.now()
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	restarted, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = restarted.Close() }()
	restarted.now = func() time.Time { return now }
	if _, err = restarted.FilterCandidates(ctx, "model", executor.Options{}, auths[:2]); err == nil {
		t.Fatal("restart lost single account pin")
	}
}

func TestContinuationDoesNotEscapeToHealthyOtherProvider(t *testing.T) {
	s, auths := testStore(t)
	if err := s.ObserveResponse(context.Background(), auths[0], executor.Options{}, []byte(`{"id":"resp_owner","output":[{"encrypted_content":"known"}]}`)); err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{`{"previous_response_id":"resp_owner"}`, `{"previous_response_id":"resp_owner","input":[{"encrypted_content":"unknown"}]}`, `{"previous_response_id":"resp_unknown"}`} {
		opts := executor.Options{OriginalRequest: []byte(body), Metadata: map[string]any{executor.CandidateProvidersMetadataKey: []string{"codex", "claude"}}}
		if _, err := s.FilterCandidates(context.Background(), "model", opts, auths[2:]); err == nil {
			t.Fatalf("continuation escaped: %s", body)
		}
	}
	if _, err := s.FilterCandidates(context.Background(), "model", executor.Options{OriginalRequest: []byte(`{"previous_response_id":"resp_owner"}`)}, auths[2:]); err == nil {
		t.Fatal("known owner escaped when provider metadata missing")
	}
}

func TestClaudeQuotaCanonicalKeysAndModelIsolation(t *testing.T) {
	s, auths := testStore(t)
	id, now := logical(s, auths[2]), s.now()
	body, err := ParseQuotaBody("claude", id, []byte(`{"five_hour":{"utilization":20},"seven_day_opus":{"utilization":100}}`), now)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.ObserveQuota(body); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.state.Load().Quotas[id+":5h"]; !ok {
		t.Fatal("noncanonical primary window")
	}
	if _, ok := s.state.Load().Quotas[id+":7d-opus"]; !ok {
		t.Fatal("noncanonical model window")
	}
	for _, tc := range []struct {
		model string
		want  float64
	}{{"claude-sonnet-4", .8}, {"claude-opus-4", 0}} {
		rel, _, _, _ := quotaAvailability(s.state.Load(), id, now, tc.model)
		if rel != tc.want {
			t.Fatalf("%s quota %f, want %f", tc.model, rel, tc.want)
		}
	}
}

func TestUnknownAdditionalQuotaIsNotAccountWide(t *testing.T) {
	s, auths := testStore(t)
	id, now := logical(s, auths[0]), s.now()
	q, err := ParseQuotaBody("codex", id, []byte(`{"additional_rate_limits":[{"limit_name":"other","rate_limit":{"primary_window":{"used_percent":100}}},{"limit_name":"model","metered_feature":"model","rate_limit":{"primary_window":{"used_percent":100}}}]}`), now)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.ObserveQuota(q); err != nil {
		t.Fatal(err)
	}
	if rel, _, _, _ := quotaAvailability(s.state.Load(), id, now, "unrelated"); rel != 1 {
		t.Fatal("additional window blocked unrelated model")
	}
	if rel, _, _, _ := quotaAvailability(s.state.Load(), id, now, "model"); rel != 0 {
		t.Fatal("model window ignored")
	}
}

func TestReusedCredentialFilenameCannotInheritAnotherPrincipalScope(t *testing.T) {
	s, auths := testStore(t)
	oldID := logical(s, auths[0])
	key, _ := scopedKey(t, s, oldID)
	replacement := auths[0].Clone()
	replacement.Metadata["account_id"] = "different-principal"
	if err := s.SyncAccounts([]*auth.Auth{replacement, auths[1], auths[2]}); err != nil {
		t.Fatal(err)
	}
	if logical(s, replacement) == oldID {
		t.Fatal("different principal inherited logical identity")
	}
	ctx := WithRequest(context.Background(), RequestContext{KeyID: key.ID})
	if _, err := s.FilterCandidates(ctx, "model", executor.Options{}, []*auth.Auth{replacement}); err == nil {
		t.Fatal("new principal inherited old API-key scope")
	}
}

func TestOriginalContinuationObjectsRetainOwnerAcrossTransformedPayloadAndScope(t *testing.T) {
	s, auths := testStore(t)
	ctx := context.Background()
	if err := s.ObserveResponse(ctx, auths[0], executor.Options{}, []byte(`{"id":"resp_object-owner","conversation_id":"conv_object-owner","output":[{"file_id":"file_object-owner","resource_id":"resource_object-owner","encrypted_content":"opaque-object-owner"}]}`)); err != nil {
		t.Fatal(err)
	}
	key, _ := scopedKey(t, s, logical(s, auths[1]))
	for _, original := range []string{
		`{"previous_response_id":"resp_object-owner"}`,
		`{"conversation":{"id":"conv_object-owner"}}`,
		`{"input":[{"type":"input_file","file_id":"file_object-owner"}]}`,
		`{"input":[{"resource_id":"resource_object-owner"}]}`,
		`{"input":[{"type":"reasoning","encrypted_content":"opaque-object-owner"}]}`,
	} {
		opts := executor.Options{OriginalRequest: []byte(original)}
		selected, err := s.FilterCandidates(ctx, "model", opts, auths[:2])
		if err != nil || len(selected) != 1 || selected[0].ID != auths[0].ID {
			t.Fatalf("original object lost owner: %s %v", original, err)
		}
		if _, err = s.FilterCandidates(WithRequest(ctx, RequestContext{KeyID: key.ID}), "model", opts, auths[:2]); err == nil {
			t.Fatal("hard ownership expanded key scope")
		}
	}
	var raw string
	if err := s.db.QueryRow("SELECT group_concat(hash) FROM affinity").Scan(&raw); err != nil || strings.Contains(raw, "object-owner") {
		t.Fatal("original continuation persisted a raw object ID")
	}
}
