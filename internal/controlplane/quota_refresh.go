package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

func ParseQuotaBody(provider, account string, body []byte, now time.Time) ([]QuotaWindow, error) {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(body, &root); err != nil {
		return nil, errors.New("invalid provider quota JSON")
	}
	out := []QuotaWindow{}
	if provider == "codex" {
		var limits map[string]json.RawMessage
		_ = json.Unmarshal(root["rate_limit"], &limits)
		parse := func(rawKey string, raw json.RawMessage) {
			var v struct {
				Used       *float64 `json:"used_percent"`
				Window     int64    `json:"limit_window_seconds"`
				Reset      int64    `json:"reset_at"`
				ResetAfter int64    `json:"reset_after_seconds"`
			}
			if json.Unmarshal(raw, &v) != nil || v.Used == nil {
				return
			}
			key := rawKey
			if rawKey == "primary_window" {
				key = "primary"
			} else if rawKey == "secondary_window" {
				key = "secondary"
			}
			if rawKey == "primary_window" && v.Window == 18000 {
				key = "5h"
			} else if rawKey == "secondary_window" && v.Window == 604800 {
				key = "weekly"
			} else if rawKey == "secondary_window" && v.Window >= 2419200 && v.Window <= 2678400 {
				key = "monthly"
			}
			var reset time.Time
			if v.Reset > 0 {
				reset = time.Unix(v.Reset, 0).UTC()
			} else if v.ResetAfter > 0 {
				reset = now.Add(time.Duration(v.ResetAfter) * time.Second)
			}
			out = append(out, QuotaWindow{Account: account, Provider: provider, Key: key, RawKey: rawKey, UsedPercent: v.Used, ResetAt: reset, ObservedAt: now, DurationSeconds: v.Window, Source: "codex_wham_usage"})
		}
		for _, slot := range []string{"primary_window", "secondary_window"} {
			parse(slot, limits[slot])
		}
		var additional []struct {
			Name  string                     `json:"limit_name"`
			Model string                     `json:"metered_feature"`
			Rate  map[string]json.RawMessage `json:"rate_limit"`
		}
		_ = json.Unmarshal(root["additional_rate_limits"], &additional)
		for _, v := range additional {
			name := v.Name
			if name == "" {
				name = v.Model
			}
			if name == "" {
				continue
			}
			for _, slot := range []string{"primary_window", "secondary_window"} {
				before := len(out)
				parse("additional:"+name+":"+slot, v.Rate[slot])
				if len(out) > before {
					out[len(out)-1].Model = v.Model
				}
			}
		}
	} else if provider == "claude" {
		for key, raw := range root {
			if key != "five_hour" && !strings.HasPrefix(key, "seven_day") {
				continue
			}
			var v struct {
				Used  *float64 `json:"utilization"`
				Reset string   `json:"resets_at"`
			}
			if json.Unmarshal(raw, &v) != nil || v.Used == nil {
				continue
			}
			canonical := strings.ReplaceAll(strings.ReplaceAll(key, "five_hour", "5h"), "seven_day", "7d")
			canonical = strings.ReplaceAll(canonical, "_", "-")
			model := ""
			if strings.HasPrefix(canonical, "7d-") {
				model = strings.TrimPrefix(canonical, "7d-")
			}
			duration := int64(604800)
			if key == "five_hour" {
				duration = 18000
			}
			out = append(out, QuotaWindow{Account: account, Provider: provider, Key: canonical, RawKey: key, Model: model, UsedPercent: v.Used, ResetAt: timestamp(v.Reset), ObservedAt: now, DurationSeconds: duration, Source: "claude_oauth_usage"})
		}
	}
	return out, nil
}

func (s *Store) RefreshQuota(ctx context.Context, manager *auth.Manager, accountID string) error {
	account, ok := s.state.Load().Accounts[accountID]
	if !ok {
		return errors.New("unknown account")
	}
	if s.state.Load().Settings.Providers[account.Provider].Disabled {
		return errors.New("provider probes are disabled")
	}
	endpoint := ""
	switch account.Provider {
	case "codex":
		endpoint = "https://chatgpt.com/backend-api/wham/usage"
	case "claude":
		endpoint = "https://api.anthropic.com/api/oauth/usage"
	default:
		return errors.New("provider does not expose a supported quota endpoint")
	}
	var credential *auth.Auth
	for _, a := range manager.List() {
		if s.state.Load().Credentials[credentialRef(a.Provider, a.ID)] == accountID && !a.Disabled && a.AuthKind() != "api_key" {
			credential = a
			break
		}
	}
	if credential == nil {
		return errors.New("active OAuth credential unavailable")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if account.Provider == "claude" {
		req.Header.Set("anthropic-beta", "oauth-2025-04-20")
	}
	response, err := manager.HttpRequest(ctx, credential, req)
	if err != nil {
		return errors.New("provider quota request failed")
	}
	if response == nil || response.Body == nil {
		return errors.New("empty provider quota response")
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("provider quota endpoint returned HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return errors.New("provider quota response read failed")
	}
	windows, err := ParseQuotaBody(account.Provider, accountID, body, s.now())
	if err != nil {
		return err
	}
	if len(windows) == 0 {
		return errors.New("provider quota evidence unavailable")
	}
	return s.ObserveQuota(windows)
}
