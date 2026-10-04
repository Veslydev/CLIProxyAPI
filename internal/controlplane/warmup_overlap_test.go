package controlplane

import (
	"context"
	"database/sql"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestWarmOverlapRetainsEveryPoolPolicy(t *testing.T) {
	s, auths := testStore(t)
	p := warmPool(t, s, auths)
	other := p
	other.ID, other.Name = "other-warm", "other-warm"
	other.Warm.Model, other.Warm.Prompt = "other-model", "Other prompt"
	other.Warm.WindowSeconds = 14400
	if err := s.SavePool(other); err != nil {
		t.Fatal(err)
	}
	if err := s.PlanWarmups(); err != nil {
		t.Fatal(err)
	}
	rows, err := s.entities(context.Background(), "schedule")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != len(p.Accounts)*2 {
		t.Fatalf("overlap discarded pool policies: %d schedules for %d memberships", len(rows), len(p.Accounts)*2)
	}
}

func TestWarmOptOutReenableCannotEraseCooldown(t *testing.T) {
	s, auths := testStore(t)
	p := warmPool(t, s, auths)
	id := p.Accounts[0]
	if err := s.WarmNow(id); err != nil {
		t.Fatal(err)
	}
	f := &fakeExecutor{}
	m := runtimeManager(t, s, auths, f)
	if err := s.RunWarmups(context.Background(), m); err != nil {
		t.Fatal(err)
	}
	p.Warm.Enabled = false
	if err := s.SavePool(p); err != nil {
		t.Fatal(err)
	}
	if err := s.PlanWarmups(); err != nil {
		t.Fatal(err)
	}
	p.Warm.Enabled = true
	if err := s.SavePool(p); err != nil {
		t.Fatal(err)
	}
	if err := s.PlanWarmups(); err != nil {
		t.Fatal(err)
	}
	if err := s.WarmNow(id); err == nil {
		t.Fatal("opt-out/re-enable erased global last-probe cooldown")
	}
}

func TestWarmConcurrentClaimsOneAccount(t *testing.T) {
	s, auths := testStore(t)
	p := warmPool(t, s, auths)
	schedule := scheduleFor(t, s, p.Accounts[0])
	now := schedule.Next
	s.now = func() time.Time { return now }
	var wg sync.WaitGroup
	results := make(chan bool, 12)
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, err := s.claim(schedule)
			if err != nil {
				t.Error(err)
			}
			results <- ok
		}()
	}
	wg.Wait()
	close(results)
	count := 0
	for ok := range results {
		if ok {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("%d claims for one account", count)
	}
}

func TestWarmClaimCapturesDispatchPolicyAndIgnoresStaleFinish(t *testing.T) {
	s, auths := testStore(t)
	p := warmPool(t, s, auths)
	v := scheduleFor(t, s, p.Accounts[0])
	now := v.Next
	s.now = func() time.Time { return now }
	dispatch, ok, err := s.claimWarm(v)
	if err != nil || !ok {
		t.Fatalf("claim: %v %v", ok, err)
	}
	p.Warm.Model, p.Warm.Prompt = "edited-model", "edited prompt"
	if err = s.SavePool(p); err != nil {
		t.Fatal(err)
	}
	if err = s.PlanWarmups(); err != nil {
		t.Fatal(err)
	}
	if dispatch.Model == p.Warm.Model || dispatch.Prompt == p.Warm.Prompt || dispatch.Provider != "codex" || dispatch.Schedule.ClaimID == "" {
		t.Fatal("claim did not capture the original dispatch policy")
	}
	stale := dispatch.Schedule
	stale.ClaimID = "different-attempt"
	if err = s.finishWarm(stale, "success"); err != nil {
		t.Fatal(err)
	}
	if !scheduleFor(t, s, v.Account).Claimed {
		t.Fatal("stale completion cleared another claim")
	}
	if err = s.finishWarm(dispatch.Schedule, "success"); err != nil {
		t.Fatal(err)
	}
	if scheduleFor(t, s, v.Account).Claimed {
		t.Fatal("own completion did not release claim")
	}
}

func TestWarmOverlapSimultaneousSlotsArbitrateAndSurviveEdits(t *testing.T) {
	s, auths := testStore(t)
	p := warmPool(t, s, auths)
	id := p.Accounts[0]
	other := p
	other.ID, other.Name = "a-other", "a-other"
	other.Warm.CooldownSeconds = 3600
	if err := s.SavePool(other); err != nil {
		t.Fatal(err)
	}
	if err := s.PlanWarmups(); err != nil {
		t.Fatal(err)
	}
	now := s.now()
	a, b := scheduleFor(t, s, id, p.ID), scheduleFor(t, s, id, other.ID)
	a.Next, b.Next = now, now
	for _, v := range []Schedule{a, b} {
		if err := s.mutate("", "", func(tx *sql.Tx) error { return saveSchedule(tx, v) }); err != nil {
			t.Fatal(err)
		}
	}
	if ok, err := s.claim(a); err != nil || ok {
		t.Fatalf("stable arbitration not enforced: %v %v", ok, err)
	}
	if ok, err := s.claim(b); err != nil || !ok {
		t.Fatalf("winning pool cannot claim: %v %v", ok, err)
	}
	if ok, err := s.claim(a); err != nil || ok {
		t.Fatalf("overlapping global claim: %v %v", ok, err)
	}
	if err := s.finishWarm(b, "success"); err != nil {
		t.Fatal(err)
	}
	guard := s.state.Load().WarmAccounts[id]
	if !guard.CooldownUntil.Equal(now.Add(time.Hour)) {
		t.Fatal("shared cooldown did not capture most conservative policy")
	}
	other.Warm.Enabled = false
	p.Accounts = p.Accounts[:1]
	p.Warm.CooldownSeconds = 60
	if err := s.SavePool(other); err != nil {
		t.Fatal(err)
	}
	if err := s.SavePool(p); err != nil {
		t.Fatal(err)
	}
	if err := s.PlanWarmups(); err != nil {
		t.Fatal(err)
	}
	if scheduleFor(t, s, id, other.ID).Active {
		t.Fatal("one-pool opt-out remained active")
	}
	if !scheduleFor(t, s, id, p.ID).Active {
		t.Fatal("one-pool opt-out suppressed remaining pool")
	}
	if err := s.WarmNow(id); err == nil {
		t.Fatal("policy/membership edits shortened captured cooldown")
	}
	path := s.path
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	restarted, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = restarted.Close() }()
	restarted.now = func() time.Time { return now }
	if err := restarted.WarmNow(id); err == nil {
		t.Fatal("restart erased account cooldown")
	}
}

func TestWarmImmediateResetStillHonorsTrafficAndCooldown(t *testing.T) {
	s, auths := testStore(t)
	p := warmPool(t, s, auths)
	p.Warm.ResetMode = "immediate"
	if err := s.SavePool(p); err != nil {
		t.Fatal(err)
	}
	if err := s.PlanWarmups(); err != nil {
		t.Fatal(err)
	}
	id := p.Accounts[0]
	now := s.now()
	if err := s.mutate("", "", func(tx *sql.Tx) error { return s.queueReset(tx, id, now.Add(-time.Second)) }); err != nil {
		t.Fatal(err)
	}
	v := scheduleFor(t, s, id)
	if !v.Next.Equal(now) || v.Reason != "reset_confirmed" {
		t.Fatal("immediate reset queued a future phase")
	}
	a := s.state.Load().Accounts[id]
	a.LastTraffic = now
	if err := s.SaveAccount(a); err != nil {
		t.Fatal(err)
	}
	// SaveAccount only mutates flags, never overwrites traffic evidence.
	if err := s.mutate("", "", func(tx *sql.Tx) error { return put(tx, "account", id, a) }); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.claim(v); err != nil || ok {
		t.Fatalf("recent traffic bypass: %v %v", ok, err)
	}
	v = scheduleFor(t, s, id)
	now = now.Add(time.Duration(p.Warm.IdleSeconds) * time.Second)
	s.now = func() time.Time { return now }
	if !v.Next.Equal(now) {
		t.Fatal("immediate reset traffic deferral was phased")
	}
	if ok, err := s.claim(v); err != nil || !ok {
		t.Fatalf("eligible confirmed reset did not claim: %v %v", ok, err)
	}
	if err := s.finishWarm(v, "success"); err != nil {
		t.Fatal(err)
	}
	if err := s.WarmNow(id); err == nil {
		t.Fatal("reset and manual modes duplicated probe")
	}
}

func TestMigrationV3WarmSchedulesBackupAndInterruptedRecovery(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v3.sqlite")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, ddl := range []string{schema, schemaV2, schemaV3} {
		if _, err = db.Exec(ddl); err != nil {
			t.Fatal(err)
		}
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	p := Pool{ID: "p", Warm: WarmPolicy{CooldownSeconds: 3600}}
	v := Schedule{Account: "a", Pool: "p", Last: now, Next: now, Claimed: true, Reason: "manual", WindowSeconds: 18000}
	if err = put(tx, "pool", "p", p); err != nil {
		t.Fatal(err)
	}
	if err = put(tx, "schedule", "a", v); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	s.now = func() time.Time { return now }
	files, _ := filepath.Glob(path + ".backup-*.sqlite")
	if len(files) != 1 {
		t.Fatal("missing v3 pre-upgrade backup")
	}
	old, err := sql.Open("sqlite", files[0])
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = old.Close() }()
	var version int
	if err = old.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 3 {
		t.Fatal("backup is not v3")
	}
	if err = s.RecoverWarmups(); err != nil {
		t.Fatal(err)
	}
	got := scheduleFor(t, s, "a", "p")
	guard := s.state.Load().WarmAccounts["a"]
	if got.Claimed || got.Result != "interrupted_no_replay" || guard.ClaimID != "" || !guard.CooldownUntil.Equal(now.Add(time.Hour)) {
		t.Fatal("migration lost claim/cooldown evidence")
	}
}
