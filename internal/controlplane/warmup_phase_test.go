package controlplane

import (
	"context"
	"database/sql"
	"sort"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/usage"
)

func TestWarmUsageEvidenceUsesSelectedPool(t *testing.T) {
	for _, strategy := range []string{"round_robin", "native"} {
		t.Run(strategy, func(t *testing.T) {
			s, auths := testStore(t)
			p := warmPool(t, s, auths)
			p.Strategy = strategy
			if err := s.SavePool(p); err != nil {
				t.Fatal(err)
			}
			id := p.Accounts[0]
			usage.RegisterNamedPlugin(t.Name(), s)
			t.Cleanup(func() { usage.RegisterNamedPlugin(t.Name(), noUsagePlugin{}) })
			if err := s.WarmNow(id); err != nil {
				t.Fatal(err)
			}
			f := &fakeExecutor{publishUsage: true, publishImageUsage: true}
			m := runtimeManager(t, s, auths, f)
			if err := s.RunWarmups(context.Background(), m); err != nil {
				t.Fatal(err)
			}
			rows, err := s.RequestLogs(context.Background(), LogFilter{Account: id})
			if err != nil {
				t.Fatal(err)
			}
			if len(rows) != 2 {
				t.Fatalf("expected base and side-model records, got %d", len(rows))
			}
			var total int64
			for _, row := range rows {
				if row.Pool != p.ID || row.Routing == nil || row.Routing.Pool != p.ID || row.Retries != 0 {
					t.Fatalf("warm settlement lost dispatch evidence: %+v", row)
				}
				total += row.Total
			}
			if total != 13 {
				t.Fatalf("base and side-model tokens lost: %d", total)
			}
		})
	}
}

type noUsagePlugin struct{}

func (noUsagePlugin) HandleUsage(context.Context, usage.Record) {}

func TestWarmProbeUsesSelectedPoolsReserve(t *testing.T) {
	s, auths := testStore(t)
	p := warmPool(t, s, auths)
	p.ReservePercent = 80
	if err := s.SavePool(p); err != nil {
		t.Fatal(err)
	}
	if err := s.PlanWarmups(); err != nil {
		t.Fatal(err)
	}
	id := p.Accounts[0]
	used := 50.0
	if err := s.ObserveQuota([]QuotaWindow{{Account: id, Provider: "codex", Key: "5h", UsedPercent: &used, ResetAt: s.now().Add(5 * time.Hour), ObservedAt: s.now(), Source: "test"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.WarmNow(id); err == nil {
		t.Fatal("manual warm admitted a pool-reserve-ineligible probe")
	}
	f := &fakeExecutor{}
	m := runtimeManager(t, s, auths, f)
	if err := s.RunWarmups(context.Background(), m); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.calls) != 0 {
		t.Fatal("warm request used default pool and bypassed selected pool reserve")
	}
}

func TestWarmAutomaticUnknownDoesNotEraseCooldownEvidence(t *testing.T) {
	s, auths := testStore(t)
	p := warmPool(t, s, auths)
	p.Warm.WindowSeconds = 0
	if err := s.SavePool(p); err != nil {
		t.Fatal(err)
	}
	before := scheduleFor(t, s, p.Accounts[0])
	before.Last = s.now()
	if err := s.mutate("", "", func(tx *sql.Tx) error { return saveSchedule(tx, before) }); err != nil {
		t.Fatal(err)
	}
	if err := s.PlanWarmups(); err != nil {
		t.Fatal(err)
	}
	after := scheduleFor(t, s, before.Account)
	if !after.Last.Equal(before.Last) {
		t.Fatal("missing primary duration erased cooldown evidence")
	}
	now := before.Next
	s.now = func() time.Time { return now }
	if claimed, err := s.claim(after); err != nil || claimed {
		t.Fatalf("unknown automatic duration was guessed: %v %v", claimed, err)
	}
}

func TestWarmAutomaticWindowUsesNativePrimaryEvidence(t *testing.T) {
	s, auths := testStore(t)
	for _, id := range []string{"d", "e", "f"} {
		auths = append(auths, &auth.Auth{ID: id, Provider: "codex", Metadata: map[string]any{"account_id": id}})
	}
	if err := s.SyncAccounts(auths); err != nil {
		t.Fatal(err)
	}
	p := warmPool(t, s, auths)
	used := 0.0
	for _, id := range p.Accounts {
		if err := s.ObserveQuota([]QuotaWindow{{Account: id, Provider: "codex", Key: "primary", DurationSeconds: 14400, UsedPercent: &used, ObservedAt: s.now(), Source: "test"}}); err != nil {
			t.Fatal(err)
		}
	}
	p.Warm.WindowSeconds = 0
	if err := s.SavePool(p); err != nil {
		t.Fatal(err)
	}
	if err := s.PlanWarmups(); err != nil {
		t.Fatal(err)
	}
	first := scheduleFor(t, s, p.Accounts[0])
	for _, id := range p.Accounts {
		next := scheduleFor(t, s, id).Next
		delta := next.Sub(first.Next)
		if delta < 0 {
			delta += 4 * time.Hour
		}
		if delta%(48*time.Minute) != 0 {
			t.Fatalf("phase does not derive from native 4h/5: %v", delta)
		}
	}
}

func TestWarmSimultaneousResetsKeepFiveDistinctPoolPhases(t *testing.T) {
	s, auths := testStore(t)
	for _, id := range []string{"d", "e", "f"} {
		auths = append(auths, &auth.Auth{ID: id, Provider: "codex", Metadata: map[string]any{"account_id": id}})
	}
	if err := s.SyncAccounts(auths); err != nil {
		t.Fatal(err)
	}
	p := warmPool(t, s, auths)
	used, lower := 90.0, 0.0
	reset := s.now().Add(time.Minute)
	for _, id := range p.Accounts {
		if err := s.ObserveQuota([]QuotaWindow{{Account: id, Provider: "codex", Key: "5h", UsedPercent: &used, ResetAt: reset, ObservedAt: s.now(), Source: "test"}}); err != nil {
			t.Fatal(err)
		}
	}
	now := reset.Add(time.Second)
	s.now = func() time.Time { return now }
	seen := map[time.Time]bool{}
	slots := []time.Time{}
	for _, id := range p.Accounts {
		if err := s.ObserveQuota([]QuotaWindow{{Account: id, Provider: "codex", Key: "5h", UsedPercent: &lower, ResetAt: reset.Add(5 * time.Hour), ObservedAt: now, Source: "test"}}); err != nil {
			t.Fatal(err)
		}
		schedule := scheduleFor(t, s, id)
		if schedule.Reason != "reset_confirmed" || !schedule.Next.After(now) || seen[schedule.Next] {
			t.Fatal("simultaneous resets collapsed staggered phases")
		}
		seen[schedule.Next] = true
		slots = append(slots, schedule.Next)
	}
	if len(seen) != 5 {
		t.Fatal("lost phase slots")
	}
	sort.Slice(slots, func(i, j int) bool { return slots[i].Before(slots[j]) })
	f := &fakeExecutor{}
	m := runtimeManager(t, s, auths, f)
	for i, slot := range slots {
		now = slot
		if err := s.RunWarmups(context.Background(), m); err != nil {
			t.Fatal(err)
		}
		f.mu.Lock()
		count := len(f.calls)
		f.mu.Unlock()
		if count != i+1 {
			t.Fatalf("expected one reset probe per phase: got %d after phase %d", count, i)
		}
	}
	var completed int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM warm_runs WHERE reason='reset_confirmed' AND result='success'").Scan(&completed); err != nil {
		t.Fatal(err)
	}
	if completed != 5 {
		t.Fatalf("reset probe history missing: %d", completed)
	}
}

func TestWarmJitterStaysWithinAssignedSlot(t *testing.T) {
	p := Pool{ID: "short-window", Warm: WarmPolicy{WindowSeconds: 100, JitterSeconds: 30}}
	phases := Phases(p, []string{"a", "b", "c", "d", "e"})
	for i, id := range []string{"a", "b", "c", "d", "e"} {
		base := time.Duration(i) * 20 * time.Second
		if phases[id] < base || phases[id] > base+5*time.Second {
			t.Fatalf("jitter escaped slot for %s: %v", id, phases[id])
		}
	}
}

func TestWarmResetRequiresAdvancedAccountWideEvidence(t *testing.T) {
	for _, model := range []string{"", "unrelated-model"} {
		t.Run(model, func(t *testing.T) {
			s, auths := testStore(t)
			p := warmPool(t, s, auths)
			id := p.Accounts[0]
			used, lower := 90.0, 0.0
			reset := s.now().Add(time.Minute)
			old := QuotaWindow{Account: id, Provider: "codex", Key: "5h", Model: model, UsedPercent: &used, ResetAt: reset, ObservedAt: s.now(), Source: "test"}
			if err := s.ObserveQuota([]QuotaWindow{old}); err != nil {
				t.Fatal(err)
			}
			now := reset.Add(time.Second)
			s.now = func() time.Time { return now }
			old.UsedPercent = &lower
			old.ObservedAt = now
			if model != "" {
				old.ResetAt = reset.Add(5 * time.Hour)
			}
			if err := s.ObserveQuota([]QuotaWindow{old}); err != nil {
				t.Fatal(err)
			}
			if scheduleFor(t, s, id).Reason == "reset_confirmed" {
				t.Fatal("clock/lower watermark or unrelated model fabricated an account reset")
			}
		})
	}
}

func TestWarmRecentTrafficDeferralKeepsAssignedPhase(t *testing.T) {
	s, auths := testStore(t)
	p := warmPool(t, s, auths)
	schedule := scheduleFor(t, s, p.Accounts[0])
	now := schedule.Next
	s.now = func() time.Time { return now }
	a := s.state.Load().Accounts[schedule.Account]
	a.LastTraffic = now
	if err := s.mutate("", "", func(tx *sql.Tx) error { return put(tx, "account", a.ID, a) }); err != nil {
		t.Fatal(err)
	}
	if claimed, err := s.claim(schedule); err != nil || claimed {
		t.Fatalf("recent traffic claim: %v %v", claimed, err)
	}
	deferred := scheduleFor(t, s, a.ID)
	if deferred.Next.Sub(schedule.Next)%(5*time.Hour) != 0 {
		t.Fatal("idle suppression destroyed the deterministic phase")
	}
}

func TestWarmMissedSlotDoesNotBurstAfterDowntime(t *testing.T) {
	s, auths := testStore(t)
	p := warmPool(t, s, auths)
	schedule := scheduleFor(t, s, p.Accounts[0])
	now := schedule.Next.Add(5*time.Hour + time.Minute)
	s.now = func() time.Time { return now }
	if claimed, err := s.claim(schedule); err != nil || claimed {
		t.Fatalf("missed phase was dispatched late: %v %v", claimed, err)
	}
	deferred := scheduleFor(t, s, schedule.Account)
	if !deferred.Next.After(now) || deferred.Next.Sub(schedule.Next)%(5*time.Hour) != 0 {
		t.Fatal("missed slot not replanned on its phase")
	}
}

func TestWarmClaimRejectsSnapshotFromPreviousPoolPolicy(t *testing.T) {
	s, auths := testStore(t)
	p := warmPool(t, s, auths)
	schedule := scheduleFor(t, s, p.Accounts[0])
	now := schedule.Next
	s.now = func() time.Time { return now }
	p.Warm.Model = "different-model"
	if err := s.SavePool(p); err != nil {
		t.Fatal(err)
	}
	if err := s.PlanWarmups(); err != nil {
		t.Fatal(err)
	}
	now = scheduleFor(t, s, schedule.Account).Next
	if claimed, err := s.claim(schedule); err != nil || claimed {
		t.Fatalf("stale policy claimed: %v %v", claimed, err)
	}
}

func TestWarmResetEvidenceSurvivesCooldown(t *testing.T) {
	s, auths := testStore(t)
	p := warmPool(t, s, auths)
	id := p.Accounts[0]
	oldUsed, newUsed := 90.0, 0.0
	reset := s.now().Add(time.Minute)
	if err := s.ObserveQuota([]QuotaWindow{{Account: id, Provider: "codex", Key: "5h", UsedPercent: &oldUsed, ResetAt: reset, ObservedAt: s.now(), Source: "test"}}); err != nil {
		t.Fatal(err)
	}
	schedule := scheduleFor(t, s, id)
	schedule.Last = reset.Add(-5 * time.Minute)
	if err := s.mutate("", "", func(tx *sql.Tx) error { return saveSchedule(tx, schedule) }); err != nil {
		t.Fatal(err)
	}
	now := reset.Add(time.Second)
	s.now = func() time.Time { return now }
	if err := s.ObserveQuota([]QuotaWindow{{Account: id, Provider: "codex", Key: "5h", UsedPercent: &newUsed, ResetAt: reset.Add(5 * time.Hour), ObservedAt: now, Source: "test"}}); err != nil {
		t.Fatal(err)
	}
	pending := scheduleFor(t, s, id)
	if pending.Reason != "reset_confirmed" || pending.Next.Before(schedule.Last.Add(30*time.Minute)) {
		t.Fatal("cooldown discarded reset evidence")
	}
}

func TestWarmActivePrimaryWaitsForResetBeforeStagger(t *testing.T) {
	s, auths := testStore(t)
	p := warmPool(t, s, auths)
	schedule := scheduleFor(t, s, p.Accounts[0])
	now := schedule.Next
	s.now = func() time.Time { return now }
	used := 10.0
	reset := now.Add(2 * time.Hour)
	if err := s.ObserveQuota([]QuotaWindow{{Account: schedule.Account, Provider: "codex", Key: "5h", UsedPercent: &used, ResetAt: reset, ObservedAt: now, Source: "test"}}); err != nil {
		t.Fatal(err)
	}
	if claimed, err := s.claim(schedule); err != nil || claimed {
		t.Fatalf("active window warmed early: %v %v", claimed, err)
	}
	if !scheduleFor(t, s, schedule.Account).Next.After(reset) {
		t.Fatal("active window not aligned after reset")
	}
}
