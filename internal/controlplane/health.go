package controlplane

import (
	"maps"
	"time"
)

func (s *Store) poolHealthReason(a Account, p Pool, now time.Time) string {
	if p.Health.RequireHealthy && a.Health != "active" {
		return "pool_health"
	}
	if p.Health.MaxInFlight > 0 && s.InFlight(a.ID) >= p.Health.MaxInFlight {
		return "pool_in_flight_limit"
	}
	if now.Before(a.PoolHealth[p.ID].CooldownUntil) {
		return "pool_cooldown"
	}
	return ""
}

func updatePoolHealth(a *Account, p Pool, status int, now time.Time) {
	if p.ID == "" {
		return
	}
	h := a.PoolHealth[p.ID]
	h.LastStatus = status
	seconds := int64(0)
	if status < 400 {
		h.ConsecutiveFailures = 0
	} else {
		h.ConsecutiveFailures++
		if status == 429 {
			seconds = p.Health.RateLimitCooldownSeconds
		}
		if (status >= 500 || status == 401 || status == 403) && (p.Health.ConsecutiveFailureLimit == 0 || h.ConsecutiveFailures >= p.Health.ConsecutiveFailureLimit) {
			seconds = p.Health.ErrorCooldownSeconds
		}
	}
	if until := now.Add(time.Duration(seconds) * time.Second); seconds > 0 && until.After(h.CooldownUntil) {
		h.CooldownUntil = until
	}
	a.PoolHealth = maps.Clone(a.PoolHealth)
	if a.PoolHealth == nil {
		a.PoolHealth = map[string]PoolAccountHealth{}
	}
	a.PoolHealth[p.ID] = h
}
