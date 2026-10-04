import { describe, expect, it } from "vitest";
import { quotaWindowView } from "./quotaWindowView";
import type { Quota } from "./api";

const quota: Quota = {
  account: "a",
  provider: "codex",
  key: "5h",
  used_percent: 25,
  observed_at: "2026-10-03T00:00:00Z",
  reset_at: "2026-10-03T05:00:00Z",
  source: "codex_headers",
  duration_seconds: 18000,
};

describe("native quota to original window-card adapter", () => {
  it("uses native percentages, reset and observation evidence without inventing cycles", () => {
    const view = quotaWindowView(quota, Date.parse(quota.observed_at), 900);
    expect(view).toMatchObject({
      remainingPercent: 75,
      usedPercent: 25,
      kind: "five_hour",
      limitWindowSeconds: 18000,
      resetAccuracy: "exact",
      stale: false,
      usage: null,
      currentUsage: null,
      previousUsage: null,
      forecast: null,
    });
    expect(view.currentCycle).toBeUndefined();
    expect(view.fromMs).toBeNull();
  });
  it("keeps amount-only quota and missing reset unknown", () => {
    const view = quotaWindowView(
      {
        ...quota,
        key: "tokens",
        used_percent: undefined,
        remaining: 100,
        reset_at: "0001-01-01T00:00:00Z",
      },
      Date.parse(quota.observed_at) + 901000,
      900,
    );
    expect(view.remainingPercent).toBeNull();
    expect(view.resetAtMs).toBeNull();
    expect(view.resetAccuracy).toBe("unknown");
    expect(view.amountLabel).toBe("100 remaining");
    expect(view.stale).toBe(true);
  });
  it("recognizes Claude model-specific weekly windows", () => {
    expect(
      quotaWindowView({ ...quota, provider: "claude", key: "7d-opus" }, 0, 900),
    ).toMatchObject({ kind: "weekly", source: "claude" });
  });
});
