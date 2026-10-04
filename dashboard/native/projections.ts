import type { State, Quota, Rollup } from "../../web/src/controlplane/api";
import type { QuotaResponse } from "../src/lib/model-first-monitoring";
import { normalizeFraction } from "../src/lib/model-first-monitoring";

export interface AnalyticsPage {
  rows: Rollup[];
  totals: Rollup;
  groups: number;
  next_offset: number;
  daily?: Rollup[];
}
const knownTime = (value: string) => value && !value.startsWith("0001-") ? value : null;
function fraction(q: Quota) {
  if (typeof q.used_percent === "number") return normalizeFraction(1 - q.used_percent / 100);
  if (typeof q.remaining === "number" && typeof q.capacity === "number" && q.capacity > 0) return normalizeFraction(q.remaining / q.capacity);
  return null;
}
export function quotaProjection(state: State): QuotaResponse {
  return { accounts: state.accounts.filter((a) => !a.replaced_by).map((a) => {
    const evidence = state.quotas.filter((q) => q.account === a.id);
    const observed = evidence.map((q) => knownTime(q.observed_at)).filter((at): at is string => at !== null).sort();
    const freshAfter = Date.now() - state.settings.quota_fresh_seconds * 1000;
    return {
      auth_index: a.id, provider: a.provider, email: a.label, supported: evidence.length > 0,
      ...(evidence.length === 0 ? { error: "No provider quota evidence" } : {}),
      snapshotFetchedAt: observed[0] ?? null,
      snapshotStale: observed.length === 0 || Date.parse(observed[0]) < freshAfter,
      snapshotSource: [...new Set(evidence.map((q) => q.source))].join(", "),
      groups: evidence.map((q) => ({ id: q.key, label: q.raw_key || q.key, remainingFraction: fraction(q), resetTime: knownTime(q.reset_at), models: q.model ? [{ id: q.model, displayName: q.model, remainingFraction: fraction(q), resetTime: knownTime(q.reset_at) }] : [] })),
    };
  }) };
}
export function usageProjection(page: AnalyticsPage, state: State) {
  const summary = (requests = 0, tokens = 0, failures = 0) => ({ totalRequests: requests, totalTokens: tokens, successCount: requests - failures, failureCount: failures });
  // v4 rollups have total tokens, not input/output/cache tier splits. Keep
  // unavailable fields absent and disable product estimates rather than inventing 0s.
  const keys: Record<string, ReturnType<typeof summary> & { keyName: string; models: Record<string, ReturnType<typeof summary>> }> = {};
  const models = new Map<string, { model: string; requests: number; tokens: number }>();
  for (const row of page.rows) {
    const key = keys[row.api_key] ??= { keyName: state.keys.find((k) => k.id === row.api_key)?.name || row.api_key || "Unassigned", models: {}, ...summary() };
    key.totalRequests += row.requests; key.totalTokens += row.tokens; key.failureCount += row.failures; key.successCount += row.requests - row.failures;
    const model = key.models[row.model] ??= summary();
    model.totalRequests += row.requests; model.totalTokens += row.tokens; model.failureCount += row.failures; model.successCount += row.requests - row.failures;
    const aggregate = models.get(row.model) ?? { model: row.model, requests: 0, tokens: 0 };
    aggregate.requests += row.requests; aggregate.tokens += row.tokens; models.set(row.model, aggregate);
  }
  return {
    keys, totals: summary(page.totals.requests, page.totals.tokens, page.totals.failures),
    dailyBreakdown: (page.daily ?? []).map((r) => ({ date: r.day, requests: r.requests, tokens: r.tokens, success: r.requests - r.failures, failure: r.failures })),
    modelBreakdown: [...models.values()],
    truncated: page.next_offset >= 0, breakdownComplete: false,
  };
}
