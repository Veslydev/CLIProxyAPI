package controlplane

import (
	"database/sql"
	"errors"
	"sort"
	"time"
)

func scheduleID(pool, account string) string { return hash(pool + "\x00" + account) }

// Version 4 separates memberships from the account-wide probe guard. The
// pre-upgrade online backup is created by migrate before this transaction.
func migrateWarmSchedules(tx *sql.Tx) error {
	rows, err := tx.Query("SELECT id,data FROM entities WHERE kind='schedule'")
	if err != nil {
		return err
	}
	type row struct {
		id       string
		schedule Schedule
	}
	var old []row
	for rows.Next() {
		var r row
		var data string
		if err = rows.Scan(&r.id, &data); err != nil {
			break
		}
		if err = decode(data, &r.schedule); err != nil {
			break
		}
		old = append(old, r)
	}
	if err == nil {
		err = rows.Err()
	}
	_ = rows.Close()
	if err != nil {
		return err
	}
	for _, r := range old {
		v := r.schedule
		var pool Pool
		var data string
		err = tx.QueryRow("SELECT data FROM entities WHERE kind='pool' AND id=?", v.Pool).Scan(&data)
		if err == nil {
			if err = decode(data, &pool); err != nil {
				return err
			}
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		guard := WarmAccount{Account: v.Account, Last: v.Last, Pool: v.Pool, Result: v.Result, CooldownUntil: v.Last.Add(time.Duration(pool.Warm.CooldownSeconds) * time.Second)}
		if v.Claimed {
			v.ClaimID = "migration:" + r.id
			guard.ClaimID = v.ClaimID
		}
		v.Active = true
		if _, err = tx.Exec("DELETE FROM entities WHERE kind='schedule' AND id=?", r.id); err != nil {
			return err
		}
		if err = put(tx, "schedule", scheduleID(v.Pool, v.Account), v); err != nil {
			return err
		}
		if err = put(tx, "warm_account", v.Account, guard); err != nil {
			return err
		}
	}
	return nil
}

func warmPolicyHash(p Pool, ids []string) (string, error) {
	ids = append([]string(nil), ids...)
	sort.Strings(ids)
	data, err := encode(struct {
		Policy  WarmPolicy
		Members []string
	}{p.Warm, ids})
	return hash(data), err
}

func loadSchedule(tx *sql.Tx, pool, account string) (Schedule, error) {
	var v Schedule
	var data string
	err := tx.QueryRow("SELECT data FROM entities WHERE kind='schedule' AND id=?", scheduleID(pool, account)).Scan(&data)
	if err == nil {
		err = decode(data, &v)
	}
	return v, err
}

func saveSchedule(tx *sql.Tx, v Schedule) error {
	return put(tx, "schedule", scheduleID(v.Pool, v.Account), v)
}

func resetImmediate(p Pool, v Schedule) bool {
	return v.Reason == "reset_confirmed" && p.Warm.ResetMode == "immediate"
}

func phaseAfter(v Schedule, boundary time.Time) time.Time {
	if v.WindowSeconds <= 0 {
		return boundary.Add(time.Minute)
	}
	return nextPhase(boundary, time.Duration(v.WindowSeconds)*time.Second, time.Duration(v.PhaseSeconds*float64(time.Second)))
}

func (s *Store) warmEligible(st *snapshot, v Schedule, now time.Time) bool {
	p, exists := st.Pools[v.Pool]
	a := st.Accounts[v.Account]
	if !exists || !v.Active || v.Suspended != "" || !p.Warm.Enabled || st.Settings.Providers[p.Provider].Disabled || !a.WarmEnabled || a.Paused || a.Health != "active" || !contains(p.Accounts, a.ID) || s.poolHealthReason(a, p, now) != "" || s.InFlight(a.ID) > 0 {
		return false
	}
	if len(p.Models) > 0 && !contains(p.Models, p.Warm.Model) {
		return false
	}
	rel, _, _, _ := quotaAvailability(st, a.ID, now, p.Warm.Model)
	if rel <= p.ReservePercent/100 {
		return false
	}
	resolved := resolvedWarmPool(st, p, warmMembers(st, p), now)
	if resolved.Warm.WindowSeconds <= 0 || resolved.Warm.WindowSeconds != v.WindowSeconds {
		return false
	}
	policy, err := warmPolicyHash(resolved, warmMembers(st, p))
	return err == nil && policy == v.PolicyHash
}

// Earliest eligible slot wins; simultaneous slots prefer manual, then confirmed
// reset, then stable pool ID. Every losing pool keeps its own future policy.
func scheduleLess(a, b Schedule) bool {
	if !a.Next.Equal(b.Next) {
		return a.Next.Before(b.Next)
	}
	rank := func(v Schedule) int {
		if v.Reason == "manual" {
			return 0
		}
		if v.Reason == "reset_confirmed" {
			return 1
		}
		return 2
	}
	if rank(a) != rank(b) {
		return rank(a) < rank(b)
	}
	return a.Pool < b.Pool
}

func (s *Store) effectiveSchedule(st *snapshot, account string, now time.Time) (Schedule, bool) {
	var best Schedule
	found := false
	for _, v := range st.Schedules {
		if v.Account != account || !s.warmEligible(st, v, now) {
			continue
		}
		if !found || scheduleLess(v, best) {
			best, found = v, true
		}
	}
	return best, found
}

func (s *Store) WarmSchedules() []Schedule {
	st := s.state.Load()
	out := make([]Schedule, 0, len(st.Schedules))
	for _, v := range st.Schedules {
		guard := st.WarmAccounts[v.Account]
		if guard.Last.After(v.Last) {
			v.Last, v.Result = guard.Last, guard.Result
		}
		best, ok := s.effectiveSchedule(st, v.Account, s.now())
		v.Effective = ok && best.Pool == v.Pool
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Account != out[j].Account {
			return out[i].Account < out[j].Account
		}
		return scheduleLess(out[i], out[j])
	})
	return out
}
