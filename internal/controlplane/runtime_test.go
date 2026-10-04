package controlplane

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/runtime/executor/helps"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/usage"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

type fakeExecutor struct {
	mu                sync.Mutex
	calls             []string
	fail              map[string]bool
	stream            bool
	started           chan struct{}
	release           chan struct{}
	publishUsage      bool
	publishImageUsage bool
}

func (f *fakeExecutor) Identifier() string { return "codex" }
func (f *fakeExecutor) Execute(ctx context.Context, a *auth.Auth, req executor.Request, _ executor.Options) (executor.Response, error) {
	f.mu.Lock()
	f.calls = append(f.calls, a.ID)
	failed := f.fail[a.ID]
	f.mu.Unlock()
	if failed {
		return executor.Response{}, &auth.Error{Code: "rate_limit", Message: "limited", HTTPStatus: 429}
	}
	if f.publishUsage {
		reporter := helps.NewUsageReporter(ctx, "codex", req.Model, a)
		reporter.Publish(ctx, usage.Detail{TotalTokens: 10})
		if f.publishImageUsage {
			reporter.PublishAdditionalModel(ctx, "fixture-image", usage.Detail{TotalTokens: 3})
		}
	}
	return executor.Response{Payload: []byte(`{"id":"resp_fake-runtime","output":[]}`)}, nil
}
func (f *fakeExecutor) ExecuteStream(ctx context.Context, a *auth.Auth, _ executor.Request, _ executor.Options) (*executor.StreamResult, error) {
	f.mu.Lock()
	f.calls = append(f.calls, a.ID)
	failed := f.fail[a.ID]
	f.mu.Unlock()
	if failed {
		return nil, &auth.Error{Code: "rate_limit", Message: "limited", HTTPStatus: 429}
	}
	chunks := make(chan executor.StreamChunk)
	go func() {
		defer close(chunks)
		select {
		case chunks <- executor.StreamChunk{Payload: []byte(`data: {"type":"response.created","response":{"id":"resp_stream-runtime"}}`)}:
		case <-ctx.Done():
			return
		}
		if f.started != nil {
			close(f.started)
		}
		if f.release != nil {
			select {
			case <-f.release:
			case <-ctx.Done():
				return
			}
		}
		select {
		case chunks <- executor.StreamChunk{Payload: []byte("data: [DONE]\n")}:
		case <-ctx.Done():
		}
	}()
	return &executor.StreamResult{Chunks: chunks}, nil
}
func (f *fakeExecutor) Refresh(_ context.Context, a *auth.Auth) (*auth.Auth, error) { return a, nil }
func (f *fakeExecutor) CountTokens(ctx context.Context, a *auth.Auth, r executor.Request, o executor.Options) (executor.Response, error) {
	return f.Execute(ctx, a, r, o)
}
func (f *fakeExecutor) HttpRequest(_ context.Context, _ *auth.Auth, r *http.Request) (*http.Response, error) {
	if r.URL.String() != "https://chatgpt.com/backend-api/wham/usage" {
		return nil, errors.New("wrong quota endpoint")
	}
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"rate_limit":{"primary_window":{"used_percent":5,"limit_window_seconds":18000}}}`))}, nil
}

func runtimeManager(t *testing.T, s *Store, auths []*auth.Auth, f *fakeExecutor) *auth.Manager {
	t.Helper()
	m := auth.NewManager(nil, &auth.RoundRobinSelector{}, auth.NoopHook{})
	m.RegisterExecutor(f)
	m.SetRetryConfig(1, 0, 0)
	for _, a := range auths {
		if a.Provider != "codex" {
			continue
		}
		if _, err := m.Register(context.Background(), a); err != nil {
			t.Fatal(err)
		}
		registry.GetGlobalRegistry().RegisterClient(a.ID, a.Provider, []*registry.ModelInfo{{ID: "model"}})
		t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(a.ID) })
	}
	m.SetCandidatePolicy(s)
	return m
}

type delegateScheduler struct{}

func (delegateScheduler) PickAuth(context.Context, pluginapi.SchedulerPickRequest) (pluginapi.SchedulerPickResponse, bool, error) {
	return pluginapi.SchedulerPickResponse{Handled: true, DelegateBuiltin: pluginapi.SchedulerBuiltinRoundRobin}, true, nil
}

func TestRuntimeRetryAndPluginDelegateCannotEscapeScope(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(map[bool]string{false: "http", true: "stream"}[stream], func(t *testing.T) {
			s, auths := testStore(t)
			key, _ := scopedKey(t, s, logical(s, auths[0]))
			f := &fakeExecutor{fail: map[string]bool{"a": true}}
			m := runtimeManager(t, s, auths, f)
			m.SetPluginScheduler(delegateScheduler{})
			ctx := WithRequest(context.Background(), RequestContext{KeyID: key.ID})
			req := executor.Request{Model: "model", Payload: []byte(`{"model":"model"}`)}
			if stream {
				_, err := m.ExecuteStream(ctx, []string{"codex"}, req, executor.Options{})
				if err == nil {
					t.Fatal("scoped failed request unexpectedly succeeded")
				}
			} else {
				_, err := m.Execute(ctx, []string{"codex"}, req, executor.Options{})
				if err == nil {
					t.Fatal("scoped failed request unexpectedly succeeded")
				}
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			if len(f.calls) == 0 {
				t.Fatal("no upstream attempt")
			}
			for _, id := range f.calls {
				if id != "a" {
					t.Fatalf("scope escaped to %s", id)
				}
			}
			if s.state.Load().Keys[key.ID].Requests != 1 {
				t.Fatal("retry duplicated logical request admission")
			}
			if s.InFlight(logical(s, auths[0])) != 0 {
				t.Fatal("failed request leaked in-flight lease")
			}
		})
	}
}

func TestRuntimeStreamDisconnectReleasesInFlightAndPersistsOwner(t *testing.T) {
	s, auths := testStore(t)
	id := logical(s, auths[0])
	key, _ := scopedKey(t, s, id)
	f := &fakeExecutor{started: make(chan struct{}), release: make(chan struct{})}
	m := runtimeManager(t, s, auths, f)
	ctx, cancel := context.WithCancel(WithRequest(context.Background(), RequestContext{KeyID: key.ID}))
	defer cancel()
	result, err := m.ExecuteStream(ctx, []string{"codex"}, executor.Request{Model: "model", Payload: []byte(`{"model":"model"}`)}, executor.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if s.InFlight(id) != 1 {
		t.Fatal("stream lacks in-flight lease")
	}
	<-result.Chunks
	<-f.started
	owner, err := s.owner(objectHash(affinityObject{"response", "resp_stream-runtime"}), true)
	if err != nil || owner != id {
		t.Fatalf("stream object ownership not durable before forward: %s %v", owner, err)
	}
	cancel()
	for range result.Chunks {
	}
	if s.InFlight(id) != 0 {
		t.Fatal("disconnect leaked lease")
	}
	unresolved, err := s.UnresolvedExecutions(context.Background())
	if err != nil || len(unresolved) != 1 || unresolved[0].Account != id || unresolved[0].State != "unresolved_no_usage" {
		t.Fatalf("stream attempt ID was not journaled independently of its lease: %v %v", unresolved, err)
	}
}

func TestRuntimeKeyLimitCountsWebsocketStyleTurnsNotRetries(t *testing.T) {
	s, auths := testStore(t)
	key, _ := scopedKey(t, s, logical(s, auths[0]))
	key.RequestLimit = 1
	if err := s.SaveKey(key); err != nil {
		t.Fatal(err)
	}
	m := runtimeManager(t, s, auths, &fakeExecutor{})
	ctx := WithRequest(context.Background(), RequestContext{KeyID: key.ID})
	req := executor.Request{Model: "model"}
	if _, err := m.Execute(ctx, []string{"codex"}, req, executor.Options{}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Execute(ctx, []string{"codex"}, req, executor.Options{}); err == nil {
		t.Fatal("second turn escaped request limit")
	}
}

func TestQuotaRefreshUsesExistingExecutor(t *testing.T) {
	s, auths := testStore(t)
	m := runtimeManager(t, s, auths, &fakeExecutor{})
	id := logical(s, auths[0])
	if err := s.RefreshQuota(context.Background(), m, id); err != nil {
		t.Fatal(err)
	}
	q := s.state.Load().Quotas[id+":5h"]
	if q.UsedPercent == nil || *q.UsedPercent != 5 {
		t.Fatal("quota adapter did not persist provider response")
	}
}

func TestFiveAccountPhasesDeterministic(t *testing.T) {
	p := Pool{ID: "pool", Warm: WarmPolicy{WindowSeconds: 18000}}
	phases := Phases(p, []string{"e", "d", "a", "c", "b"})
	for i, id := range []string{"a", "b", "c", "d", "e"} {
		if phases[id] != time.Duration(i)*time.Hour {
			t.Fatalf("phase %s: %v", id, phases[id])
		}
	}
	p.Warm.JitterSeconds = 30
	first, second := Phases(p, []string{"a", "b"}), Phases(p, []string{"b", "a"})
	if first["a"] != second["a"] || first["b"] != second["b"] {
		t.Fatal("jitter not deterministic")
	}
}

func warmPool(t *testing.T, s *Store, auths []*auth.Auth) Pool {
	t.Helper()
	ids := []string{}
	for _, a := range auths {
		if a.Provider != "codex" {
			continue
		}
		id := logical(s, a)
		account := s.state.Load().Accounts[id]
		account.WarmEnabled = true
		if err := s.SaveAccount(account); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	p := Pool{ID: "warm-pool", Name: "warm-pool", Provider: "codex", Accounts: ids, Strategy: "round_robin", Warm: WarmPolicy{Enabled: true, Model: "model", WindowSeconds: 18000, CooldownSeconds: 1800, IdleSeconds: 3600}}
	if err := s.SavePool(p); err != nil {
		t.Fatal(err)
	}
	if err := s.PlanWarmups(); err != nil {
		t.Fatal(err)
	}
	return p
}

func scheduleFor(t *testing.T, s *Store, id string, pools ...string) Schedule {
	t.Helper()
	var data string
	pool := "warm-pool"
	if len(pools) > 0 {
		pool = pools[0]
	}
	if err := s.db.QueryRow("SELECT data FROM entities WHERE kind='schedule' AND id=?", scheduleID(pool, id)).Scan(&data); err != nil {
		t.Fatal(err)
	}
	var v Schedule
	if err := decode(data, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestWarmClaimCooldownRecentTrafficAndRestartNoDuplicate(t *testing.T) {
	s, auths := testStore(t)
	p := warmPool(t, s, auths)
	schedule := scheduleFor(t, s, p.Accounts[0])
	now := schedule.Next
	s.now = func() time.Time { return now }
	claimed, err := s.claim(schedule)
	if err != nil || !claimed {
		t.Fatalf("claim: %v %v", claimed, err)
	}
	if claimed, err = s.claim(schedule); err != nil || claimed {
		t.Fatal("duplicate concurrent claim")
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
	restarted.now = func() time.Time { return now }
	if err = restarted.RecoverWarmups(); err != nil {
		t.Fatal(err)
	}
	recovered := scheduleFor(t, restarted, p.Accounts[0])
	if recovered.Claimed || recovered.Result != "interrupted_no_replay" || !recovered.Next.After(now) {
		t.Fatalf("restart replays claimed probe: %#v", recovered)
	}
	if err = restarted.WarmNow(p.Accounts[0]); err == nil {
		t.Fatal("manual probe bypassed cooldown")
	}
	other := scheduleFor(t, restarted, p.Accounts[1])
	now = other.Next
	a := restarted.state.Load().Accounts[other.Account]
	a.LastTraffic = now
	if err = restarted.mutate("", "", func(tx *sql.Tx) error { return put(tx, "account", a.ID, a) }); err != nil {
		t.Fatal(err)
	}
	if claimed, err = restarted.claim(other); err != nil || claimed {
		t.Fatal("recent real traffic did not suppress idle probe")
	}
}

func TestWarmMembershipRescheduleAndRealExecutor(t *testing.T) {
	s, auths := testStore(t)
	p := warmPool(t, s, auths)
	before := scheduleFor(t, s, p.Accounts[0])
	p.Accounts = p.Accounts[:1]
	if err := s.SavePool(p); err != nil {
		t.Fatal(err)
	}
	if err := s.PlanWarmups(); err != nil {
		t.Fatal(err)
	}
	after := scheduleFor(t, s, p.Accounts[0])
	if before.PolicyHash == after.PolicyHash {
		t.Fatal("membership did not recalculate phase")
	}
	now := after.Next
	s.now = func() time.Time { return now }
	f := &fakeExecutor{}
	m := runtimeManager(t, s, auths, f)
	if err := s.RunWarmups(context.Background(), m); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	count := len(f.calls)
	f.mu.Unlock()
	if count != 1 {
		t.Fatalf("warm-up sent %d requests", count)
	}
	if scheduleFor(t, s, p.Accounts[0]).Result != "success" {
		t.Fatal("warm result missing")
	}
	if err := s.RunWarmups(context.Background(), m); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.calls) != 1 {
		t.Fatal("warm-up duplicated")
	}
}

func TestWarmResetAndStaggerShareOneClaim(t *testing.T) {
	s, auths := testStore(t)
	p := warmPool(t, s, auths)
	id := p.Accounts[0]
	oldUsed, newUsed := 90.0, 1.0
	reset := s.now().Add(time.Minute)
	if err := s.ObserveQuota([]QuotaWindow{{Account: id, Provider: "codex", Key: "5h", UsedPercent: &oldUsed, ResetAt: reset, ObservedAt: s.now(), Source: "test"}}); err != nil {
		t.Fatal(err)
	}
	now := reset.Add(time.Second)
	s.now = func() time.Time { return now }
	if err := s.ObserveQuota([]QuotaWindow{{Account: id, Provider: "codex", Key: "5h", UsedPercent: &newUsed, ResetAt: now.Add(5 * time.Hour), ObservedAt: now, Source: "test"}}); err != nil {
		t.Fatal(err)
	}
	schedule := scheduleFor(t, s, id)
	if schedule.Reason != "reset_confirmed" {
		t.Fatalf("no confirmed reset schedule: %#v", schedule)
	}
	now = schedule.Next
	claimed, err := s.claim(schedule)
	if err != nil || !claimed {
		t.Fatal("reset claim failed")
	}
	if claimed, err = s.claim(schedule); err != nil || claimed {
		t.Fatal("stagger duplicated reset probe")
	}
}
