// Package controlplane implements the optional persistent local control plane.
package controlplane

import "time"

type Account struct {
	ID               string                       `json:"id"`
	Provider         string                       `json:"provider"`
	Label            string                       `json:"label"`
	Plan             string                       `json:"plan,omitempty"`
	Workspace        string                       `json:"workspace,omitempty"`
	Paused           bool                         `json:"paused"`
	Health           string                       `json:"health"`
	WarmEnabled      bool                         `json:"warm_enabled"`
	LastTraffic      time.Time                    `json:"last_traffic"`
	Requests         int64                        `json:"requests"`
	Failures         int64                        `json:"failures"`
	PoolHealth       map[string]PoolAccountHealth `json:"pool_health,omitempty"`
	IdentityEvidence string                       `json:"identity_evidence,omitempty"`
	ReplacedBy       string                       `json:"replaced_by,omitempty"`
}

type PoolAccountHealth struct {
	ConsecutiveFailures int64     `json:"consecutive_failures"`
	CooldownUntil       time.Time `json:"cooldown_until"`
	LastStatus          int       `json:"last_status"`
}

type HealthPolicy struct {
	RequireHealthy           bool  `json:"require_healthy"`
	MaxInFlight              int64 `json:"max_in_flight"`
	RateLimitCooldownSeconds int64 `json:"rate_limit_cooldown_seconds"`
	ErrorCooldownSeconds     int64 `json:"error_cooldown_seconds"`
	ConsecutiveFailureLimit  int64 `json:"consecutive_failure_limit"`
}

type WarmPolicy struct {
	Enabled         bool   `json:"enabled"`
	Model           string `json:"model"`
	Prompt          string `json:"prompt"`
	WindowSeconds   int64  `json:"window_seconds"`
	SpacingSeconds  int64  `json:"spacing_seconds"`
	IdleSeconds     int64  `json:"idle_seconds"`
	CooldownSeconds int64  `json:"cooldown_seconds"`
	JitterSeconds   int64  `json:"jitter_seconds"`
	ResetMode       string `json:"reset_mode,omitempty"`
}

type Pool struct {
	ID             string       `json:"id"`
	Name           string       `json:"name"`
	Provider       string       `json:"provider"`
	Accounts       []string     `json:"accounts"`
	Strategy       string       `json:"strategy"`
	Models         []string     `json:"models"`
	ReservePercent float64      `json:"reserve_percent"`
	Sticky         bool         `json:"sticky"`
	Warm           WarmPolicy   `json:"warm"`
	Health         HealthPolicy `json:"health"`
}

type Binding struct {
	Accounts []string `json:"accounts"`
	Pools    []string `json:"pools"`
}

type APIKey struct {
	ID           string             `json:"id"`
	Name         string             `json:"name"`
	Prefix       string             `json:"prefix"`
	Revoked      bool               `json:"revoked"`
	ExpiresAt    time.Time          `json:"expires_at"`
	Models       []string           `json:"models"`
	Bindings     map[string]Binding `json:"bindings"`
	RequestLimit int64              `json:"request_limit"`
	TokenLimit   int64              `json:"token_limit"`
	CostLimit    float64            `json:"cost_limit"`
	Requests     int64              `json:"requests"`
	Tokens       int64              `json:"tokens"`
	Cost         float64            `json:"cost"`
	LastUsed     time.Time          `json:"last_used"`
}

type QuotaWindow struct {
	Account         string    `json:"account"`
	Provider        string    `json:"provider"`
	Key             string    `json:"key"`
	RawKey          string    `json:"raw_key"`
	Model           string    `json:"model,omitempty"`
	UsedPercent     *float64  `json:"used_percent,omitempty"`
	Remaining       *float64  `json:"remaining,omitempty"`
	Capacity        *float64  `json:"capacity,omitempty"`
	ResetAt         time.Time `json:"reset_at"`
	ObservedAt      time.Time `json:"observed_at"`
	DurationSeconds int64     `json:"duration_seconds"`
	Source          string    `json:"source"`
}

type RequestLog struct {
	ID            string    `json:"id"`
	At            time.Time `json:"at"`
	Provider      string    `json:"provider"`
	Model         string    `json:"model"`
	ActualModel   string    `json:"actual_model"`
	Account       string    `json:"account"`
	Pool          string    `json:"pool"`
	APIKey        string    `json:"api_key"`
	Status        int       `json:"status"`
	Input         int64     `json:"input_tokens"`
	Output        int64     `json:"output_tokens"`
	Reasoning     int64     `json:"reasoning_tokens"`
	Cache         int64     `json:"cache_tokens"`
	Total         int64     `json:"total_tokens"`
	Cost          *float64  `json:"cost,omitempty"`
	LatencyMS     int64     `json:"latency_ms"`
	TTFTMS        int64     `json:"ttft_ms"`
	Retries       int       `json:"retries"`
	ErrorCategory string    `json:"error_category,omitempty"`
	Phase         string    `json:"phase,omitempty"`
	Route         string    `json:"route,omitempty"`
	Routing       *Decision `json:"routing,omitempty"`
}

type Decision struct {
	At               time.Time         `json:"at"`
	Account          string            `json:"account"`
	Pool             string            `json:"pool"`
	Model            string            `json:"model"`
	Strategy         string            `json:"strategy"`
	Reason           string            `json:"reason"`
	Skipped          map[string]string `json:"skipped"`
	APIKey           string            `json:"api_key,omitempty"`
	QuotaKnown       bool              `json:"quota_known"`
	RemainingPercent *float64          `json:"remaining_percent,omitempty"`
	ResetAt          time.Time         `json:"reset_at"`
	InFlight         int64             `json:"in_flight"`
	WarmPhase        *PhaseEvidence    `json:"warm_phase,omitempty"`
}

// Planned phase is scheduling evidence, never an upstream reset or capacity.
type PhaseEvidence struct {
	Pool         string    `json:"pool"`
	Next         time.Time `json:"next"`
	PhaseSeconds float64   `json:"phase_seconds"`
	Bonus        float64   `json:"bonus"`
}

type Schedule struct {
	Account        string    `json:"account"`
	Pool           string    `json:"pool"`
	Next           time.Time `json:"next"`
	Last           time.Time `json:"last"`
	Reason         string    `json:"reason"`
	Result         string    `json:"result"`
	Claimed        bool      `json:"claimed"`
	PolicyHash     string    `json:"policy_hash"`
	WindowSeconds  int64     `json:"window_seconds"`
	SpacingSeconds float64   `json:"spacing_seconds"`
	PhaseSeconds   float64   `json:"phase_seconds"`
	Active         bool      `json:"active"`
	PlannedAt      time.Time `json:"planned_at"`
	Suspended      string    `json:"suspended,omitempty"`
	ClaimID        string    `json:"claim_id,omitempty"`
	ResetAt        time.Time `json:"reset_confirmed_at"`
	Effective      bool      `json:"effective"`
}

type WarmAccount struct {
	Account       string    `json:"account"`
	Last          time.Time `json:"last"`
	CooldownUntil time.Time `json:"cooldown_until"`
	ClaimID       string    `json:"claim_id,omitempty"`
	Pool          string    `json:"pool,omitempty"`
	Result        string    `json:"result,omitempty"`
	ResetAt       time.Time `json:"reset_confirmed_at"`
}

type Settings struct {
	Strategy                  string                    `json:"strategy"`
	QuotaFreshSeconds         int64                     `json:"quota_fresh_seconds"`
	StickySeconds             int64                     `json:"sticky_seconds"`
	InputPricePerMillion      float64                   `json:"input_price_per_million"`
	OutputPricePerMillion     float64                   `json:"output_price_per_million"`
	StickyMinRemainingPercent float64                   `json:"sticky_min_remaining_percent"`
	FailurePenalty            float64                   `json:"failure_penalty"`
	InFlightPenalty           float64                   `json:"in_flight_penalty"`
	RetentionDays             int                       `json:"retention_days"`
	Providers                 map[string]ProviderPolicy `json:"providers,omitempty"`
	Pricing                   []ModelPrice              `json:"pricing,omitempty"`
	PhasePreference           float64                   `json:"phase_preference"`
}

type ProviderPolicy struct {
	Disabled            bool  `json:"disabled"`
	QuotaRefreshSeconds int64 `json:"quota_refresh_seconds"`
}

type ModelPrice struct {
	Provider         string  `json:"provider"`
	Model            string  `json:"model"`
	InputPerMillion  float64 `json:"input_per_million"`
	OutputPerMillion float64 `json:"output_per_million"`
}

var Strategies = []string{"capacity_weighted", "relative_availability", "usage_weighted", "round_robin", "fill_first", "sequential_drain", "reset_drain", "single_account", "native"}
