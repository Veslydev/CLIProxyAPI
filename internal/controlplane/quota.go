package controlplane

import (
	"database/sql"
	"errors"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func number(v string) *float64 {
	n, err := strconv.ParseFloat(v, 64)
	if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
		return nil
	}
	return &n
}
func timestamp(v string) time.Time {
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return t.UTC()
	}
	if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
		return time.Unix(n, 0).UTC()
	}
	return time.Time{}
}

// ParseQuota only reports provider evidence actually present in upstream headers.
func ParseQuota(provider, account string, h http.Header, now time.Time) []QuotaWindow {
	out := []QuotaWindow{}
	if provider == "codex" {
		for _, slot := range []string{"primary", "secondary"} {
			prefix := "x-codex-" + slot + "-"
			used := number(h.Get(prefix + "used-percent"))
			if used == nil {
				continue
			}
			duration := int64(0)
			if n := number(h.Get(prefix + "window-minutes")); n != nil {
				duration = int64(*n * 60)
			}
			key := slot
			if slot == "primary" && duration == 18000 {
				key = "5h"
			} else if slot == "secondary" && duration == 604800 {
				key = "weekly"
			} else if slot == "secondary" && duration >= 2419200 && duration <= 2678400 {
				key = "monthly"
			}
			reset := timestamp(h.Get(prefix + "reset-at"))
			if reset.IsZero() {
				if n := number(h.Get(prefix + "reset-after-seconds")); n != nil {
					reset = now.Add(time.Duration(*n) * time.Second)
				}
			}
			out = append(out, QuotaWindow{Account: account, Provider: provider, Key: key, RawKey: slot, UsedPercent: used, ResetAt: reset, ObservedAt: now, DurationSeconds: duration, Source: "upstream_headers"})
		}
	} else if provider == "claude" {
		for _, window := range []string{"5h", "7d", "7d-sonnet", "7d-opus"} {
			prefix := "anthropic-ratelimit-unified-" + window + "-"
			used := number(h.Get(prefix + "utilization"))
			if used != nil {
				v := *used * 100
				used = &v
			}
			reset := timestamp(h.Get(prefix + "reset"))
			status := h.Get(prefix + "status")
			if used == nil && reset.IsZero() && status == "" {
				continue
			}
			if used == nil && status == "rejected" {
				v := 100.0
				used = &v
			}
			model := ""
			if strings.HasPrefix(window, "7d-") {
				model = strings.TrimPrefix(window, "7d-")
			}
			duration := int64(604800)
			if window == "5h" {
				duration = 18000
			}
			out = append(out, QuotaWindow{Account: account, Provider: provider, Key: window, RawKey: window, Model: model, UsedPercent: used, ResetAt: reset, ObservedAt: now, DurationSeconds: duration, Source: "anthropic_unified_headers"})
		}
		for _, kind := range []string{"requests", "tokens", "input-tokens", "output-tokens"} {
			prefix := "anthropic-ratelimit-" + kind + "-"
			capacity := number(h.Get(prefix + "limit"))
			remaining := number(h.Get(prefix + "remaining"))
			if capacity == nil && remaining == nil {
				continue
			}
			out = append(out, QuotaWindow{Account: account, Provider: provider, Key: kind, RawKey: kind, Remaining: remaining, Capacity: capacity, ResetAt: timestamp(h.Get(prefix + "reset")), ObservedAt: now, Source: "anthropic_rate_limit_headers"})
		}
	}
	return out
}

func quotaID(q QuotaWindow) string { return q.Account + ":" + q.Key }

func (s *Store) ObserveQuota(windows []QuotaWindow) error {
	if len(windows) == 0 {
		return nil
	}
	return s.mutate("", "", func(tx *sql.Tx) error {
		for _, q := range windows {
			a, ok := s.state.Load().Accounts[q.Account]
			if !ok || a.Provider != q.Provider || q.Source == "" || q.ObservedAt.IsZero() {
				return errors.New("invalid quota observation")
			}
			old := s.state.Load().Quotas[quotaID(q)]
			if !old.ObservedAt.IsZero() && q.ObservedAt.Before(old.ObservedAt) {
				continue
			}
			if q.UsedPercent != nil && (*q.UsedPercent < 0 || *q.UsedPercent > 100) {
				return errors.New("invalid quota percentage")
			}
			if err := put(tx, "quota", quotaID(q), q); err != nil {
				return err
			}
			data, err := encode(q)
			if err != nil {
				return err
			}
			if _, err = tx.Exec("INSERT INTO quota_history(account,at,data) VALUES(?,?,?)", q.Account, q.ObservedAt.Unix(), data); err != nil {
				return err
			}
			// A clock boundary alone is not proof of reset: require a new lower watermark.
			if a.WarmEnabled && q.Model == "" && !strings.HasPrefix(q.Key, "additional:") && !old.ResetAt.IsZero() && q.ResetAt.After(old.ResetAt) && !q.ObservedAt.Before(old.ResetAt) && old.UsedPercent != nil && q.UsedPercent != nil && *q.UsedPercent < *old.UsedPercent {
				if err = s.queueReset(tx, q.Account, old.ResetAt); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func quotaAvailability(st *snapshot, id string, now time.Time, models ...string) (float64, float64, time.Time, bool) {
	relative := 1.0
	absolute := math.Inf(1)
	var reset time.Time
	known := false
	for _, q := range st.QuotasByAccount[id] {
		if q.Model != "" || strings.HasPrefix(q.Key, "additional:") {
			if len(models) == 0 || q.Model == "" {
				continue
			}
			model := strings.ToLower(models[0])
			matches := model == strings.ToLower(q.Model)
			if q.Provider == "claude" && (q.Model == "sonnet" || q.Model == "opus") {
				matches = strings.Contains(model, q.Model)
			}
			if !matches {
				continue
			}
		}
		if q.Account != id || now.Sub(q.ObservedAt) > time.Duration(st.Settings.QuotaFreshSeconds)*time.Second || (!q.ResetAt.IsZero() && !now.Before(q.ResetAt)) {
			continue
		}
		remaining := 1.0
		if q.UsedPercent != nil {
			remaining = math.Max(0, 1-*q.UsedPercent/100)
			known = true
		}
		if q.Remaining != nil {
			known = true
			if *q.Remaining <= 0 {
				remaining = 0
			}
			if *q.Remaining < absolute {
				absolute = *q.Remaining
			}
			if q.Capacity != nil && *q.Capacity > 0 {
				remaining = math.Min(remaining, *q.Remaining / *q.Capacity)
			}
		}
		relative = math.Min(relative, remaining)
		if !q.ResetAt.IsZero() && (reset.IsZero() || q.ResetAt.Before(reset)) {
			reset = q.ResetAt
		}
	}
	if math.IsInf(absolute, 1) {
		absolute = relative
	}
	return relative, absolute, reset, known
}
