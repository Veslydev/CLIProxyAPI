import type { AccountDetailQuotaWindow } from "@/features/accounts/model/accountDetailViewModel";
import type { Quota } from "./api";

function timestamp(value: string): number | null {
  const at = Date.parse(value);
  return Number.isFinite(at) && at > 0 ? at : null;
}

export function quotaWindowView(
  quota: Quota,
  now: number,
  freshSeconds: number,
): AccountDetailQuotaWindow {
  const reset = timestamp(quota.reset_at);
  const observed = timestamp(quota.observed_at);
  const used =
    typeof quota.used_percent === "number" &&
    Number.isFinite(quota.used_percent)
      ? quota.used_percent
      : null;
  return {
    key: quota.key,
    label: quota.key,
    kind: quota.key.startsWith("5h")
      ? "five_hour"
      : quota.key.startsWith("weekly") || quota.key.startsWith("7d")
        ? "weekly"
        : quota.key.startsWith("monthly")
          ? "monthly"
          : "unknown",
    remainingPercent:
      used === null ? null : Math.min(100, Math.max(0, 100 - used)),
    usedPercent: used,
    resetLabel: reset === null ? "Unknown" : quota.reset_at,
    resetAtMs: reset,
    resetAccuracy: reset === null ? "unknown" : "exact",
    limitWindowSeconds: quota.duration_seconds ?? null,
    fromMs: null,
    toMs: null,
    observedAtMs: observed,
    stale: observed === null || now - observed > freshSeconds * 1000,
    amountLabel:
      quota.remaining === undefined
        ? undefined
        : `${quota.remaining} remaining`,
    description: quota.source,
    source:
      quota.provider === "codex" || quota.provider === "claude"
        ? quota.provider
        : "summary",
    usage: null,
    currentUsage: null,
    previousUsage: null,
    previousPeriod: null,
    forecast: null,
  };
}
