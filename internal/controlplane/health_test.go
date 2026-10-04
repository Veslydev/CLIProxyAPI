package controlplane

import (
	"context"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/usage"
)

func TestPoolCooldownPersistsWithoutBlockingOtherPool(t *testing.T) {
	s, auths := testStore(t)
	id := logical(s, auths[0])
	p := s.state.Load().Pools["default-codex"]
	p.Health = HealthPolicy{RateLimitCooldownSeconds: 120, ErrorCooldownSeconds: 60, ConsecutiveFailureLimit: 2}
	if err := s.SavePool(p); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordUsage(usage.Record{RequestID: "limited", Provider: "codex", AuthID: auths[0].ID, Model: "model", Failed: true, Fail: usage.Failure{StatusCode: 429}}); err != nil {
		t.Fatal(err)
	}
	if s.poolHealthReason(s.state.Load().Accounts[id], p, s.now()) != "pool_cooldown" {
		t.Fatal("rate limit cooldown missing")
	}
	other := p
	other.ID = "other"
	if s.poolHealthReason(s.state.Load().Accounts[id], other, s.now()) != "" {
		t.Fatal("pool cooldown contaminated another pool")
	}
	key, _ := scopedKey(t, s, id)
	ctx := WithRequest(context.Background(), RequestContext{KeyID: key.ID})
	if _, err := s.FilterCandidates(ctx, "model", executor.Options{}, auths[:2]); err == nil {
		t.Fatal("bound key escaped cooled account")
	}
	path, now := s.path, s.now()
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	restarted, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = restarted.Close() }()
	restarted.now = func() time.Time { return now }
	if restarted.poolHealthReason(restarted.state.Load().Accounts[id], p, now) != "pool_cooldown" {
		t.Fatal("restart lost cooldown")
	}
	if restarted.poolHealthReason(restarted.state.Load().Accounts[id], p, now.Add(121*time.Second)) != "" {
		t.Fatal("expired cooldown blocked account")
	}
}

func TestWarmPoolCooldownPreservesScheduleAndBlocksClaim(t *testing.T) {
	s, auths := testStore(t)
	p := warmPool(t, s, auths)
	id := p.Accounts[0]
	before := scheduleFor(t, s, id)
	s.now = func() time.Time { return before.Next }
	p.Health.RateLimitCooldownSeconds = 3600
	if err := s.SavePool(p); err != nil {
		t.Fatal(err)
	}
	if err := s.recordUsage(usage.Record{RequestID: "warm-cooldown", Provider: "codex", AuthID: auths[0].ID, Model: "model", Failed: true, Fail: usage.Failure{StatusCode: 429}}, true, &dispatchEvidence{Decision: Decision{Pool: p.ID}}); err != nil {
		t.Fatal(err)
	}
	if err := s.PlanWarmups(); err != nil {
		t.Fatal(err)
	}
	after := scheduleFor(t, s, id)
	if !after.Next.Equal(before.Next) || after.PolicyHash != before.PolicyHash {
		t.Fatal("transient pool cooldown rescheduled warm phase")
	}
	if claimed, err := s.claim(after); err != nil || claimed {
		t.Fatalf("cooled pool probe claimed: %v %v", claimed, err)
	}
	release := s.BeginExecution(context.Background(), auths[0], executor.Options{})
	p.Health.MaxInFlight = 1
	if err := s.SavePool(p); err != nil {
		t.Fatal(err)
	}
	if err := s.PlanWarmups(); err != nil {
		t.Fatal(err)
	}
	if !scheduleFor(t, s, id).Next.Equal(before.Next) {
		t.Fatal("in-flight pressure removed warm schedule")
	}
	release()
}

func TestHealthFailureThresholdAndProviderPolicy(t *testing.T) {
	s, auths := testStore(t)
	id := logical(s, auths[0])
	a := s.state.Load().Accounts[id]
	p := s.state.Load().Pools["default-codex"]
	p.Health = HealthPolicy{ErrorCooldownSeconds: 60, ConsecutiveFailureLimit: 2, MaxInFlight: 1}
	updatePoolHealth(&a, p, 503, s.now())
	if s.poolHealthReason(a, p, s.now()) != "" {
		t.Fatal("failure threshold ignored")
	}
	updatePoolHealth(&a, p, 503, s.now())
	if s.poolHealthReason(a, p, s.now()) != "pool_cooldown" {
		t.Fatal("repeated failures did not cool account")
	}
	before := a.PoolHealth[p.ID].CooldownUntil
	updatePoolHealth(&a, p, 200, s.now())
	if a.PoolHealth[p.ID].ConsecutiveFailures != 0 || !a.PoolHealth[p.ID].CooldownUntil.Equal(before) {
		t.Fatal("concurrent success cleared committed cooldown")
	}
	release := s.BeginExecution(context.Background(), auths[0], executor.Options{})
	if s.poolHealthReason(s.state.Load().Accounts[id], p, s.now()) != "pool_in_flight_limit" {
		t.Fatal("pool pressure ignored")
	}
	release()
	settings := s.state.Load().Settings
	settings.Providers = map[string]ProviderPolicy{"codex": {Disabled: true}}
	if err := s.SaveSettings(settings); err != nil {
		t.Fatal(err)
	}
	if _, err := s.FilterCandidates(context.Background(), "model", executor.Options{}, auths[:2]); err == nil {
		t.Fatal("disabled provider dispatched")
	}
	if err := s.RefreshQuota(context.Background(), nil, id); err == nil || err.Error() != "provider probes are disabled" {
		t.Fatalf("disabled provider reached quota transport: %v", err)
	}
}

func TestModelPricingAndAccountingFailureAdmission(t *testing.T) {
	s, auths := testStore(t)
	settings := s.state.Load().Settings
	settings.RetentionDays = 45
	settings.Pricing = []ModelPrice{{Provider: "codex", Model: "priced", InputPerMillion: 2, OutputPerMillion: 4}}
	if err := s.SaveSettings(settings); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordUsage(usage.Record{RequestID: "priced", Provider: "codex", AuthID: auths[0].ID, Model: "priced", Detail: usage.Detail{InputTokens: 1000000, OutputTokens: 1000000, TotalTokens: 2000000}}); err != nil {
		t.Fatal(err)
	}
	logs, err := s.RequestLogs(context.Background(), LogFilter{})
	if err != nil || len(logs) != 1 || logs[0].Cost == nil || *logs[0].Cost != 6 {
		t.Fatalf("model pricing: %v %v", logs, err)
	}
	settings.Pricing = append(settings.Pricing, ModelPrice{Provider: "codex", Model: "free"})
	if err := s.SaveSettings(settings); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordUsage(usage.Record{RequestID: "free", Provider: "codex", AuthID: auths[0].ID, Model: "free"}); err != nil {
		t.Fatal(err)
	}
	free, err := s.RequestLogs(context.Background(), LogFilter{Model: "free"})
	if err != nil || len(free) != 1 || free[0].Cost == nil || *free[0].Cost != 0 {
		t.Fatalf("explicit free model is not unknown pricing: %#v %v", free, err)
	}
	s.accountingFailed.Store(true)
	if err := s.AdmitRequest(context.Background(), executor.Request{}, executor.Options{}); err == nil {
		t.Fatal("failed storage did not stop dispatch")
	}
}

func TestPublishedAccountingSurvivesImmediateStoreClose(t *testing.T) {
	s, auths := testStore(t)
	m := usage.NewManager(1)
	defer m.Stop()
	m.Register(s)
	m.Publish(context.Background(), usage.Record{RequestID: "durable-publication", Provider: "codex", AuthID: auths[0].ID, Model: "model", Detail: usage.Detail{TotalTokens: 7}})
	path := s.path
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	restarted, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = restarted.Close() }()
	logs, err := restarted.RequestLogs(context.Background(), LogFilter{})
	if err != nil || len(logs) != 1 || logs[0].Total != 7 {
		t.Fatalf("publication was only queued: %#v %v", logs, err)
	}
}

func TestAccountingPanicDoesNotLeaveAdmissionEnabled(t *testing.T) {
	s, auths := testStore(t)
	m := usage.NewManager(1)
	defer m.Stop()
	m.Register(s)
	s.now = func() time.Time { panic("fixture clock failure") }
	m.Publish(context.Background(), usage.Record{RequestID: "panic", Provider: "codex", AuthID: auths[0].ID, Model: "model"})
	if err := s.AdmitRequest(context.Background(), executor.Request{}, executor.Options{}); err == nil {
		t.Fatal("dispatcher panic recovery left durable accounting fail-open")
	}
}
