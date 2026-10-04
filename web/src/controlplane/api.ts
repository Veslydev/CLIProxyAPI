export interface Account {
  id: string;
  provider: string;
  label: string;
  plan?: string;
  workspace?: string;
  paused: boolean;
  health: string;
  warm_enabled: boolean;
  last_traffic: string;
  identity_evidence?: string;
  replaced_by?: string;
  requests?: number;
  failures?: number;
  pool_health?: Record<
    string,
    {
      consecutive_failures: number;
      cooldown_until: string;
      last_status: number;
    }
  >;
}
export interface WarmPolicy {
  enabled: boolean;
  model: string;
  prompt: string;
  window_seconds: number;
  spacing_seconds: number;
  idle_seconds: number;
  cooldown_seconds: number;
  jitter_seconds: number;
  reset_mode?: "phased" | "immediate";
}
export interface Pool {
  id: string;
  name: string;
  provider: string;
  accounts: string[];
  strategy: string;
  models: string[];
  reserve_percent: number;
  sticky: boolean;
  warm: WarmPolicy;
  health?: {
    require_healthy: boolean;
    max_in_flight: number;
    rate_limit_cooldown_seconds: number;
    error_cooldown_seconds: number;
    consecutive_failure_limit: number;
  };
}
export interface Binding {
  accounts: string[];
  pools: string[];
}
export interface APIKey {
  id: string;
  name: string;
  prefix: string;
  revoked: boolean;
  expires_at: string;
  models: string[];
  bindings: Record<string, Binding>;
  request_limit: number;
  token_limit: number;
  cost_limit: number;
  requests: number;
  tokens: number;
  cost: number;
  last_used: string;
}
export interface Settings {
  strategy: string;
  quota_fresh_seconds: number;
  sticky_seconds: number;
  input_price_per_million: number;
  output_price_per_million: number;
  sticky_min_remaining_percent: number;
  failure_penalty: number;
  in_flight_penalty: number;
  phase_preference?: number;
  retention_days?: number;
  providers?: Record<
    string,
    { disabled: boolean; quota_refresh_seconds: number }
  >;
  pricing?: {
    provider: string;
    model: string;
    input_per_million: number;
    output_per_million: number;
  }[];
}
export interface Quota {
  account: string;
  provider: string;
  key: string;
  raw_key?: string;
  model?: string;
  capacity?: number;
  used_percent?: number;
  remaining?: number;
  reset_at: string;
  observed_at: string;
  source: string;
  duration_seconds?: number;
}
export interface Schedule {
  account: string;
  pool: string;
  next: string;
  last: string;
  reason: string;
  result: string;
  claimed: boolean;
  window_seconds?: number;
  spacing_seconds?: number;
  phase_seconds?: number;
  active?: boolean;
  effective?: boolean;
  planned_at?: string;
  suspended?: string;
  reset_confirmed_at?: string;
}
export interface State {
  accounts: Account[];
  pools: Pool[];
  keys: APIKey[];
  quotas: Quota[];
  settings: Settings;
  strategies: string[];
  schedules: Schedule[];
  warm_accounts?: Record<
    string,
    { last: string; cooldown_until: string; pool?: string; result?: string }
  >;
}
export interface RequestLog {
  id: string;
  at: string;
  provider: string;
  model: string;
  actual_model: string;
  account: string;
  pool: string;
  api_key: string;
  status: number;
  total_tokens: number;
  input_tokens: number;
  output_tokens: number;
  reasoning_tokens: number;
  cache_tokens: number;
  cost?: number;
  latency_ms: number;
  ttft_ms: number;
  retries?: number;
  error_category?: string;
  phase?: string;
  route?: string;
  routing?: {
    strategy: string;
    reason: string;
    remaining_percent?: number;
    quota_known: boolean;
    skipped: Record<string, string>;
  };
}
export interface Execution {
  id: string;
  at: string;
  account: string;
  provider: string;
  model: string;
  api_key: string;
  state: "unresolved_restart" | "unresolved_no_usage";
}
export interface Rollup {
  day: string;
  provider: string;
  model: string;
  account: string;
  pool: string;
  api_key: string;
  requests: number;
  failures: number;
  tokens: number;
  cost: number;
  latency_ms: number;
}

export async function api<T>(
  secret: string,
  path: string,
  method = "GET",
  body?: unknown,
  signal?: AbortSignal,
): Promise<T> {
  const response = await fetch(`/api/control-plane${path}`, {
    method,
    signal,
    headers: {
      Authorization: `Bearer ${secret}`,
      "Content-Type": "application/json",
    },
    ...(body === undefined ? {} : { body: JSON.stringify(body) }),
  });
  const result: unknown = await response.json();
  if (!response.ok)
    throw new Error(
      typeof result === "object" && result !== null && "error" in result
        ? String(result.error)
        : `HTTP ${response.status}`,
    );
  return result as T;
}

export const emptyWarm: WarmPolicy = {
  enabled: false,
  model: "",
  prompt: "Reply with OK.",
  window_seconds: 0,
  spacing_seconds: 0,
  idle_seconds: 3600,
  cooldown_seconds: 1800,
  jitter_seconds: 30,
};
