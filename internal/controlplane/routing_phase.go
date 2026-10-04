package controlplane

import "time"

// This is an optional bounded scheduling-locality hint, not quota evidence.
// Native reset/remaining values and all eligibility checks remain independent.
func (s *Store) routingPhase(st *snapshot, account, pool, model string, now time.Time) *PhaseEvidence {
	if st.Settings.PhasePreference <= 0 {
		return nil
	}
	v, ok := st.Schedules[scheduleID(pool, account)]
	if !ok || !v.Active || v.Suspended != "" || v.Claimed || v.WindowSeconds <= 0 || v.PlannedAt.IsZero() || v.PlannedAt.After(now) || now.Sub(v.PlannedAt) > time.Duration(st.Settings.QuotaFreshSeconds)*time.Second || !v.Next.After(now) || v.Next.Sub(now) > time.Duration(v.WindowSeconds)*time.Second {
		return nil
	}
	p := st.Pools[pool]
	if !p.Warm.Enabled || !st.Accounts[account].WarmEnabled || !contains(p.Accounts, account) {
		return nil
	}
	resolved := resolvedWarmPool(st, p, warmMembers(st, p), now)
	fingerprint, err := warmPolicyHash(resolved, warmMembers(st, p))
	if err != nil || fingerprint != v.PolicyHash || resolved.Warm.WindowSeconds != v.WindowSeconds {
		return nil
	}
	_, _, _, known := quotaAvailability(st, account, now, model)
	if !known {
		return nil
	}
	weight := st.Settings.PhasePreference
	if weight > 0.05 {
		weight = 0.05
	}
	bonus := weight * (1 - v.Next.Sub(now).Seconds()/float64(v.WindowSeconds))
	return &PhaseEvidence{Pool: pool, Next: v.Next, PhaseSeconds: v.PhaseSeconds, Bonus: bonus}
}
