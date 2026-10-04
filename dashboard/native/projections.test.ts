import { describe, expect, it } from "vitest";
import { quotaProjection, usageProjection } from "./projections";
import { emptyState, rollup } from "./fixtures.test-support";

describe("native evidence projections", () => {
  it("keeps unobserved quota unknown and excludes replaced accounts", () => {
    const state = emptyState();
    state.accounts.push({ id: "a", provider: "codex", label: "fixture", health: "healthy", paused: false, warm_enabled: false, last_traffic: "" });
    state.accounts.push({ ...state.accounts[0], id: "old", replaced_by: "a" });
    const data = quotaProjection(state);
    expect(data.accounts).toHaveLength(1);
    expect(data.accounts[0]).toMatchObject({ supported: false, snapshotStale: true, groups: [], snapshotFetchedAt: null });
  });
  it("maps observed fractions, preserves unknown fractions and stale evidence", () => {
    const state = emptyState();
    state.accounts.push({ id: "a", provider: "codex", label: "fixture", health: "healthy", paused: false, warm_enabled: false, last_traffic: "" });
    const quota = { account: "a", provider: "codex", key: "week", reset_at: "0001-01-01T00:00:00Z", observed_at: "2000-01-01T00:00:00Z", source: "fixture" };
    state.quotas.push({ ...quota, used_percent: 25 }, { ...quota, key: "unknown" }, { ...quota, key: "window", capacity: 100, remaining: 20 });
    const account = quotaProjection(state).accounts[0];
    expect(account.snapshotStale).toBe(true);
    expect(account.groups?.map((g) => g.remainingFraction)).toEqual([0.75, null, 0.2]);
    expect(account.groups?.[0].resetTime).toBeNull();
  });
  it("keeps full-range totals independent of bounded breakdowns; invents no token tiers", () => {
    const result = usageProjection({ rows: [rollup()], totals: rollup({ requests: 20, tokens: 100, failures: 2 }), next_offset: 100, groups: 200, daily: [rollup()] }, emptyState());
    expect(result.totals).toEqual({ totalRequests: 20, totalTokens: 100, failureCount: 2, successCount: 18 });
    expect(result.keys.key.totalRequests).toBe(2);
    expect(result.totals).not.toHaveProperty("inputTokens");
    expect(result.keys.key.models.gpt).not.toHaveProperty("cachedTokens");
    expect(result.truncated).toBe(true);
    expect(result.breakdownComplete).toBe(false);
  });
});
