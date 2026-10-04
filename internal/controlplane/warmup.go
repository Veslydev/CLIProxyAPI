package controlplane

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/translator"
	log "github.com/sirupsen/logrus"
)

// Phases derive spacing from the actual window and eligible pool membership.
func Phases(p Pool, ids []string) map[string]time.Duration {
	ids = append([]string(nil), ids...)
	sort.Strings(ids)
	out := map[string]time.Duration{}
	if len(ids) == 0 || p.Warm.WindowSeconds <= 0 {
		return out
	}
	spacing := time.Duration(p.Warm.SpacingSeconds) * time.Second
	if spacing == 0 {
		spacing = time.Duration(p.Warm.WindowSeconds) * time.Second / time.Duration(len(ids))
	}
	window := time.Duration(p.Warm.WindowSeconds) * time.Second
	jitterSeconds := p.Warm.JitterSeconds
	if limit := int64(spacing / (4 * time.Second)); jitterSeconds > limit {
		jitterSeconds = limit
	}
	phaseOffset := time.Duration(0)
	for _, id := range ids {
		phase := phaseOffset
		if jitterSeconds > 0 {
			sum := sha256.Sum256([]byte(p.ID + ":" + id))
			phase += time.Duration(binary.BigEndian.Uint64(sum[:8])%uint64(jitterSeconds+1)) * time.Second
		}
		out[id] = phase % window
		phaseOffset = (phaseOffset + spacing%window) % window
	}
	return out
}

func primaryWarmWindow(q QuotaWindow) bool {
	return q.Model == "" && (q.Key == "5h" || q.Key == "primary")
}

// Explicit duration is an operator override. Automatic planning requires fresh,
// compatible primary-window evidence for every opted-in member; unknown is not 5h.
func resolvedWarmPool(st *snapshot, p Pool, ids []string, now time.Time) Pool {
	if p.Warm.WindowSeconds != 0 {
		return p
	}
	var duration int64
	for _, id := range ids {
		var primary QuotaWindow
		for _, q := range st.QuotasByAccount[id] {
			if primaryWarmWindow(q) && q.DurationSeconds > 0 && q.DurationSeconds <= 31536000 && !q.ObservedAt.After(now) && now.Sub(q.ObservedAt) <= time.Duration(st.Settings.QuotaFreshSeconds)*time.Second && q.ObservedAt.After(primary.ObservedAt) {
				primary = q
			}
		}
		if primary.DurationSeconds == 0 || (duration != 0 && duration != primary.DurationSeconds) {
			return p
		}
		duration = primary.DurationSeconds
	}
	p.Warm.WindowSeconds = duration
	return p
}

func warmMembers(st *snapshot, p Pool) []string {
	ids := []string{}
	for _, id := range p.Accounts {
		a := st.Accounts[id]
		if a.WarmEnabled && !a.Paused && a.Health == "active" {
			ids = append(ids, id)
		}
	}
	return ids
}

func nextPhase(now time.Time, window, phase time.Duration) time.Time {
	base := time.Unix(0, now.UnixNano()/int64(window)*int64(window)).UTC()
	next := base.Add(phase)
	if !next.After(now) {
		next = next.Add(window)
	}
	return next
}

func (s *Store) PlanWarmups() error {
	return s.mutate("", "", func(tx *sql.Tx) error {
		// Read after serialization so an earlier management commit cannot be
		// overwritten by a planner that waited with an obsolete snapshot.
		st := s.state.Load()
		ids := make([]string, 0, len(st.Pools))
		for id := range st.Pools {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		seen := map[string]bool{}
		for _, pid := range ids {
			p := st.Pools[pid]
			if !p.Warm.Enabled || st.Settings.Providers[p.Provider].Disabled {
				continue
			}
			eligible := warmMembers(st, p)
			p = resolvedWarmPool(st, p, eligible, s.now())
			phases := Phases(p, eligible)
			policyHash, errEncode := warmPolicyHash(p, eligible)
			if errEncode != nil {
				return errEncode
			}
			for _, id := range eligible {
				seen[scheduleID(pid, id)] = true
				schedule, err := loadSchedule(tx, pid, id)
				if err != nil && !errors.Is(err, sql.ErrNoRows) {
					return err
				}
				if schedule.Claimed {
					continue
				}
				unchanged := schedule.Active && schedule.PolicyHash == policyHash && schedule.WindowSeconds > 0
				schedule.Account = id
				schedule.Pool = pid
				schedule.Active, schedule.PlannedAt, schedule.Suspended = true, s.now(), ""
				if p.Warm.WindowSeconds <= 0 {
					schedule.Suspended = "unknown_primary_duration"
					if err = saveSchedule(tx, schedule); err != nil {
						return err
					}
					continue
				}
				if unchanged {
					if err = saveSchedule(tx, schedule); err != nil {
						return err
					}
					continue
				}
				if schedule.Reason != "reset_confirmed" && schedule.Reason != "manual" {
					schedule.Reason = "staggered_idle"
				}
				schedule.PolicyHash = policyHash
				schedule.WindowSeconds = p.Warm.WindowSeconds
				schedule.SpacingSeconds = float64(p.Warm.SpacingSeconds)
				if schedule.SpacingSeconds == 0 {
					schedule.SpacingSeconds = float64(p.Warm.WindowSeconds) / float64(len(eligible))
				}
				schedule.PhaseSeconds = float64(phases[id]) / float64(time.Second)
				schedule.Next = nextPhase(s.now(), time.Duration(p.Warm.WindowSeconds)*time.Second, phases[id])
				cooldownUntil := schedule.Last.Add(time.Duration(p.Warm.CooldownSeconds) * time.Second)
				if guard := st.WarmAccounts[id]; guard.CooldownUntil.After(cooldownUntil) {
					cooldownUntil = guard.CooldownUntil
				}
				if schedule.Next.Before(cooldownUntil) {
					schedule.Next = nextPhase(cooldownUntil, time.Duration(p.Warm.WindowSeconds)*time.Second, phases[id])
				}
				if err = saveSchedule(tx, schedule); err != nil {
					return err
				}
			}
		}
		rows, err := tx.Query("SELECT id,data FROM entities WHERE kind='schedule'")
		if err != nil {
			return err
		}
		var obsolete []Schedule
		for rows.Next() {
			var id, data string
			var schedule Schedule
			if err = rows.Scan(&id, &data); err != nil {
				break
			}
			if err = decode(data, &schedule); err != nil {
				break
			}
			if !seen[id] && !schedule.Claimed {
				schedule.Active = false
				schedule.Suspended = "opt_out_or_ineligible_membership"
				obsolete = append(obsolete, schedule)
			}
		}
		if err == nil {
			err = rows.Err()
		}
		_ = rows.Close()
		if err != nil {
			return err
		}
		for _, schedule := range obsolete {
			if err = saveSchedule(tx, schedule); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Store) queueReset(tx *sql.Tx, account string, reset time.Time) error {
	st := s.state.Load()
	guard := st.WarmAccounts[account]
	if guard.ClaimID != "" || !guard.Last.Before(reset) || !guard.ResetAt.Before(reset) {
		return nil
	}
	for _, v := range st.Schedules {
		if v.Account != account || !v.Active || v.Claimed || !v.Last.Before(reset) {
			continue
		}
		p := st.Pools[v.Pool]
		if !p.Warm.Enabled || v.Reason == "manual" || v.WindowSeconds <= 0 {
			continue
		}
		v.ResetAt, v.Reason = reset, "reset_confirmed"
		v.Next = s.now()
		if until := v.Last.Add(time.Duration(p.Warm.CooldownSeconds) * time.Second); v.Next.Before(until) {
			v.Next = until
		}
		if v.Next.Before(guard.CooldownUntil) {
			v.Next = guard.CooldownUntil
		}
		if !resetImmediate(p, v) {
			v.Next = phaseAfter(v, v.Next)
		}
		if err := saveSchedule(tx, v); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) WarmNow(account string) error {
	return s.mutate("warm.manual", account, func(tx *sql.Tx) error {
		st := s.state.Load()
		schedule, ok := s.effectiveSchedule(st, account, s.now())
		if !ok {
			return errors.New("account must opt into an enabled warm-up pool")
		}
		p := s.state.Load().Pools[schedule.Pool]
		a := s.state.Load().Accounts[account]
		if !p.Warm.Enabled || !a.WarmEnabled || a.Paused || a.Health != "active" || s.state.Load().Settings.Providers[p.Provider].Disabled || s.poolHealthReason(a, p, s.now()) != "" || !contains(p.Accounts, account) {
			return errors.New("account must opt into an enabled warm-up pool")
		}
		guard := st.WarmAccounts[account]
		if schedule.Claimed || guard.ClaimID != "" || s.now().Before(guard.CooldownUntil) || s.now().Sub(schedule.Last) < time.Duration(p.Warm.CooldownSeconds)*time.Second {
			return errors.New("warm-up already running or cooling down")
		}
		schedule.Next = s.now()
		schedule.Reason = "manual"
		return saveSchedule(tx, schedule)
	})
}

func (s *Store) claim(schedule Schedule) (bool, error) {
	_, claimed, err := s.claimWarm(schedule)
	return claimed, err
}

type warmClaim struct {
	Schedule Schedule
	Provider string
	Model    string
	Prompt   string
}

func (s *Store) claimWarm(schedule Schedule) (warmClaim, bool, error) {
	var dispatch warmClaim
	claimed := false
	err := s.mutate("", "", func(tx *sql.Tx) error {
		current, err := loadSchedule(tx, schedule.Pool, schedule.Account)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		// A row snapshot can outlive a management mutation. Do not execute the
		// old pool's model/policy after claiming a different current schedule.
		if current.Pool != schedule.Pool || current.PolicyHash != schedule.PolicyHash || current.Reason != schedule.Reason || !current.Next.Equal(schedule.Next) {
			return nil
		}
		st := s.state.Load()
		p := st.Pools[current.Pool]
		a := st.Accounts[current.Account]
		now := s.now()
		guard := st.WarmAccounts[current.Account]
		if !s.warmEligible(st, current, now) || guard.ClaimID != "" {
			return nil
		}
		// Arbitration is revalidated while holding the global mutation lock.
		if best, ok := s.effectiveSchedule(st, a.ID, now); !ok || best.Pool != current.Pool {
			return nil
		}
		if current.Claimed || now.Before(current.Next) || !p.Warm.Enabled || !a.WarmEnabled || a.Paused || a.Health != "active" || s.state.Load().Settings.Providers[p.Provider].Disabled || s.poolHealthReason(a, p, now) != "" || !contains(p.Accounts, a.ID) {
			return nil
		}
		if s.InFlight(a.ID) > 0 {
			return nil
		}
		if p.Warm.WindowSeconds == 0 {
			resolved := resolvedWarmPool(st, p, warmMembers(st, p), now)
			if resolved.Warm.WindowSeconds == 0 || resolved.Warm.WindowSeconds != current.WindowSeconds {
				return nil
			}
		}
		if now.Before(guard.CooldownUntil) || now.Sub(current.Last) < time.Duration(p.Warm.CooldownSeconds)*time.Second {
			boundary := guard.CooldownUntil
			if until := current.Last.Add(time.Duration(p.Warm.CooldownSeconds) * time.Second); until.After(boundary) {
				boundary = until
			}
			current.Next = boundary
			if current.Reason != "manual" && !resetImmediate(p, current) {
				current.Next = phaseAfter(current, boundary)
			}
			return saveSchedule(tx, current)
		}
		window := time.Duration(current.WindowSeconds) * time.Second
		if window <= 0 {
			window = time.Duration(p.Warm.WindowSeconds) * time.Second
		}
		if window <= 0 {
			return nil
		}
		phase := time.Duration(current.PhaseSeconds * float64(time.Second))
		if current.WindowSeconds == 0 {
			phase = Phases(p, warmMembers(st, p))[a.ID]
		}
		// Missing a phase must not replay every overdue member as one burst.
		if current.Reason != "manual" && !resetImmediate(p, current) && now.Sub(current.Next) >= time.Minute {
			current.Next = nextPhase(now, window, phase)
			return saveSchedule(tx, current)
		}
		if current.Reason == "staggered_idle" {
			for _, q := range st.QuotasByAccount[a.ID] {
				if primaryWarmWindow(q) && q.UsedPercent != nil && *q.UsedPercent > 0 && q.ResetAt.After(now) && !q.ObservedAt.After(now) && now.Sub(q.ObservedAt) <= time.Duration(st.Settings.QuotaFreshSeconds)*time.Second {
					current.Next = nextPhase(q.ResetAt, window, phase)
					return saveSchedule(tx, current)
				}
			}
		}
		if current.Reason != "manual" && now.Sub(a.LastTraffic) < time.Duration(p.Warm.IdleSeconds)*time.Second {
			current.Next = a.LastTraffic.Add(time.Duration(p.Warm.IdleSeconds) * time.Second)
			if !resetImmediate(p, current) {
				current.Next = nextPhase(current.Next, window, phase)
			}
			return saveSchedule(tx, current)
		}
		current.Claimed = true
		current.Last = now
		current.Result = "running"
		current.ClaimID = uuid.NewString()
		guard = WarmAccount{Account: a.ID, Last: now, Pool: p.ID, ClaimID: current.ClaimID, Result: "running", ResetAt: current.ResetAt}
		// The most conservative active policy determines shared cooldown, captured
		// now so opt-out, policy edits or evidence loss cannot shorten it later.
		for _, other := range st.Pools {
			if other.Warm.Enabled && contains(other.Accounts, a.ID) {
				until := now.Add(time.Duration(other.Warm.CooldownSeconds) * time.Second)
				if until.After(guard.CooldownUntil) {
					guard.CooldownUntil = until
				}
			}
		}
		claimed = true
		dispatch = warmClaim{Schedule: current, Provider: a.Provider, Model: p.Warm.Model, Prompt: p.Warm.Prompt}
		if err := put(tx, "warm_account", a.ID, guard); err != nil {
			return err
		}
		return saveSchedule(tx, current)
	})
	return dispatch, claimed && err == nil, err
}

func (s *Store) finishWarm(schedule Schedule, result string) error {
	return s.mutate("", "", func(tx *sql.Tx) error {
		current, err := loadSchedule(tx, schedule.Pool, schedule.Account)
		if err != nil {
			return err
		}
		st := s.state.Load()
		guard := st.WarmAccounts[current.Account]
		if !current.Claimed || guard.ClaimID != current.ClaimID || guard.ClaimID == "" || (schedule.ClaimID != "" && schedule.ClaimID != current.ClaimID) {
			return nil
		}
		guard.ClaimID, guard.Result = "", result
		if err = put(tx, "warm_account", current.Account, guard); err != nil {
			return err
		}
		p := st.Pools[current.Pool]
		window := time.Duration(current.WindowSeconds) * time.Second
		if window <= 0 {
			window = time.Duration(p.Warm.WindowSeconds) * time.Second
		}
		current.Claimed = false
		current.ClaimID = ""
		current.Result = result
		phase := time.Duration(current.PhaseSeconds * float64(time.Second))
		if current.WindowSeconds == 0 {
			phase = Phases(p, warmMembers(st, p))[current.Account]
		}
		boundary := s.now()
		if until := current.Last.Add(time.Duration(p.Warm.CooldownSeconds) * time.Second); boundary.Before(until) {
			boundary = until
		}
		if window > 0 {
			current.Next = nextPhase(boundary, window, phase)
		} else {
			current.Next = boundary.Add(time.Minute)
		}
		current.Reason = "staggered_idle"
		if guard.CooldownUntil.After(boundary) {
			boundary = guard.CooldownUntil
			current.Next = phaseAfter(current, boundary)
		}
		if err := saveSchedule(tx, current); err != nil {
			return err
		}
		// One probe services the account, not independent pool quota windows.
		for _, other := range st.Schedules {
			if other.Account != current.Account || other.Pool == current.Pool {
				continue
			}
			other.Last, other.Result = current.Last, "shared_probe:"+current.Pool+":"+result
			other.Reason = "staggered_idle"
			if !other.Next.After(guard.CooldownUntil) {
				other.Next = phaseAfter(other, guard.CooldownUntil)
			}
			if err = saveSchedule(tx, other); err != nil {
				return err
			}
		}
		_, err = tx.Exec("INSERT INTO warm_runs(at,account,pool,reason,result) VALUES(?,?,?,?,?)", s.now().Unix(), current.Account, current.Pool, schedule.Reason, result)
		return err
	})
}

func (s *Store) RecoverWarmups() error {
	rows, err := s.entities(context.Background(), "schedule")
	if err != nil {
		return err
	}
	for _, data := range rows {
		var schedule Schedule
		if err = json.Unmarshal(data, &schedule); err != nil {
			return err
		}
		if schedule.Claimed {
			if err = s.finishWarm(schedule, "interrupted_no_replay"); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Store) RunWarmups(ctx context.Context, manager *auth.Manager) error {
	rows, err := s.entities(ctx, "schedule")
	if err != nil {
		return err
	}
	for _, data := range rows {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var schedule Schedule
		if err = json.Unmarshal(data, &schedule); err != nil {
			return err
		}
		if schedule.Next.After(s.now()) {
			continue
		}
		dispatch, claimed, err := s.claimWarm(schedule)
		if err != nil {
			return err
		}
		if !claimed {
			continue
		}
		// Execute the claimed snapshot, not a later management policy revision.
		schedule = dispatch.Schedule
		prompt := dispatch.Prompt
		if prompt == "" {
			prompt = "Reply with OK."
		}
		body, err := json.Marshal(map[string]any{"model": dispatch.Model, "messages": []map[string]string{{"role": "user", "content": prompt}}, "max_tokens": 8})
		if err != nil {
			return err
		}
		warmCtx := WithRequest(ctx, RequestContext{WarmAccount: schedule.Account, WarmPool: schedule.Pool})
		_, err = manager.Execute(warmCtx, []string{dispatch.Provider}, executor.Request{Model: dispatch.Model, Payload: body, Format: translator.FromString("openai")}, executor.Options{OriginalRequest: body, SourceFormat: translator.FromString("openai")})
		result := "success"
		if err != nil {
			result = "failed"
			if ctx.Err() != nil {
				result = "canceled"
			}
		}
		if err = s.finishWarm(schedule, result); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) Run(ctx context.Context, manager *auth.Manager, retentionDays int) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	maintenance := s.now()
	lastQuotaRefresh := map[string]time.Time{}
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.SyncAccounts(manager.List()); err != nil {
				log.WithError(err).Error("control-plane account sync failed")
				continue
			}
			if err := s.PlanWarmups(); err != nil {
				log.WithError(err).Error("control-plane warm scheduling failed")
				continue
			}
			for id, a := range s.state.Load().Accounts {
				policy := s.state.Load().Settings.Providers[a.Provider]
				interval := policy.QuotaRefreshSeconds
				if interval == 0 {
					interval = 300
				}
				if !policy.Disabled && s.now().Sub(lastQuotaRefresh[id]) >= time.Duration(interval)*time.Second {
					if (a.Provider == "codex" || a.Provider == "claude") && !a.Paused {
						if err := s.RefreshQuota(ctx, manager, id); err != nil && ctx.Err() == nil {
							log.WithField("account", id).Debug("provider quota refresh unavailable")
						}
					}
					lastQuotaRefresh[id] = s.now()
				}
			}
			if err := s.RunWarmups(ctx, manager); err != nil && ctx.Err() == nil {
				log.WithError(err).Error("control-plane warmer failed")
			}
			if s.now().Sub(maintenance) >= 24*time.Hour {
				days := s.state.Load().Settings.RetentionDays
				if days == 0 {
					days = retentionDays
				}
				if err := s.Retain(days); err != nil {
					log.WithError(err).Error("control-plane retention failed")
				}
				maintenance = s.now()
			}
		}
	}
}
