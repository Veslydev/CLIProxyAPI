package controlplane

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
)

func TestPhaseHintRequiresFreshScopedScheduleAndQuota(t *testing.T) {
	s, auths := testStore(t)
	p := warmPool(t, s, auths)
	settings := s.state.Load().Settings
	settings.PhasePreference = .05
	if err := s.SaveSettings(settings); err != nil {
		t.Fatal(err)
	}
	now := s.now()
	used := 50.0
	for _, id := range p.Accounts {
		if err := s.ObserveQuota([]QuotaWindow{{Account: id, Provider: "codex", Key: "5h", UsedPercent: &used, ObservedAt: now, ResetAt: now.Add(5 * time.Hour), Source: "fixture"}}); err != nil {
			t.Fatal(err)
		}
	}
	id := p.Accounts[0]
	v := scheduleFor(t, s, id)
	v.Next = now.Add(time.Minute)
	if err := s.mutate("", "", func(tx *sql.Tx) error { return saveSchedule(tx, v) }); err != nil {
		t.Fatal(err)
	}
	phase := s.routingPhase(s.state.Load(), id, p.ID, "model", now)
	if phase == nil || phase.Bonus <= 0 || phase.Bonus > .05 {
		t.Fatal("no bounded phase signal")
	}
	if s.routingPhase(s.state.Load(), id, "default-codex", "model", now) != nil {
		t.Fatal("phase escaped selected pool")
	}
	if s.routingPhase(s.state.Load(), id, p.ID, "model", now.Add(901*time.Second)) != nil {
		t.Fatal("stale phase was used")
	}
	v.PlannedAt = now.Add(-time.Hour)
	if err := s.mutate("", "", func(tx *sql.Tx) error { return saveSchedule(tx, v) }); err != nil {
		t.Fatal(err)
	}
	if s.routingPhase(s.state.Load(), id, p.ID, "model", now) != nil {
		t.Fatal("stale schedule was used")
	}
}

func TestPhaseCannotBypassExhaustionScopeOrHardOwner(t *testing.T) {
	s, auths := testStore(t)
	p := warmPool(t, s, auths)
	settings := s.state.Load().Settings
	settings.PhasePreference = .05
	if err := s.SaveSettings(settings); err != nil {
		t.Fatal(err)
	}
	k, _ := scopedKey(t, s, p.Accounts...)
	k.Bindings["codex"] = Binding{Pools: []string{p.ID}}
	if err := s.SaveKey(k); err != nil {
		t.Fatal(err)
	}
	used := 100.0
	if err := s.ObserveQuota([]QuotaWindow{{Account: p.Accounts[0], Provider: "codex", Key: "5h", UsedPercent: &used, ObservedAt: s.now(), ResetAt: s.now().Add(time.Hour), Source: "fixture"}}); err != nil {
		t.Fatal(err)
	}
	ctx := WithRequest(context.Background(), RequestContext{KeyID: k.ID})
	candidates := []*auth.Auth{auths[0], auths[1]}
	selected, err := s.FilterCandidates(ctx, "model", executor.Options{}, candidates)
	if err != nil || len(selected) != 1 || selected[0].ID != auths[1].ID {
		t.Fatalf("exhaustion bypass: %v %v", selected, err)
	}
	if err = s.ObserveResponse(ctx, auths[0], executor.Options{}, []byte(`{"id":"resp_phase_owner","output":[]}`)); err != nil {
		t.Fatal(err)
	}
	if _, err = s.FilterCandidates(ctx, "model", executor.Options{OriginalRequest: []byte(`{"previous_response_id":"resp_phase_owner"}`)}, candidates); err == nil {
		t.Fatal("exhausted hard owner moved")
	}
	k.Bindings["codex"] = Binding{Accounts: []string{p.Accounts[0]}}
	if err = s.SaveKey(k); err != nil {
		t.Fatal(err)
	}
	if _, err = s.FilterCandidates(ctx, "model", executor.Options{}, candidates); err == nil {
		t.Fatal("exhausted scope escaped")
	}
}

func TestUnknownQuotaNeverGetsPhaseCapacity(t *testing.T) {
	s, auths := testStore(t)
	p := warmPool(t, s, auths)
	settings := s.state.Load().Settings
	settings.PhasePreference = .05
	if err := s.SaveSettings(settings); err != nil {
		t.Fatal(err)
	}
	if s.routingPhase(s.state.Load(), p.Accounts[0], p.ID, "model", s.now()) != nil {
		t.Fatal("planned phase invented unknown quota")
	}
}
