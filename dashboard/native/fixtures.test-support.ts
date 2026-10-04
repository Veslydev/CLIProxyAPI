import type { State, Rollup } from "../../web/src/controlplane/api";

export const emptyState = (): State => ({
  accounts: [], pools: [], keys: [], quotas: [], schedules: [], strategies: [],
  settings: { strategy: "balanced", quota_fresh_seconds: 300, sticky_seconds: 300, input_price_per_million: 0, output_price_per_million: 0, sticky_min_remaining_percent: 10, failure_penalty: 1, in_flight_penalty: 1 },
});
export const rollup = (overrides: Partial<Rollup> = {}): Rollup => ({ day: "2026-10-04", api_key: "key", provider: "codex", model: "gpt", account: "a", pool: "", requests: 2, failures: 1, tokens: 10, latency_ms: 200, cost: 0, ...overrides });
