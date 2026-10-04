import type { Account, Quota, State } from "./api";
import type { AccountMetrics } from "@/features/accounts/model/accountRows";

export interface AccountFilters {
  search: string;
  health: string;
  plan: string;
  pool: string;
  sort: string;
}
export const emptyAccountFilters: AccountFilters = {
  search: "",
  health: "all",
  plan: "",
  pool: "",
  sort: "name",
};

export function freshAccountQuotas(
  account: Account,
  state: State,
  now: number,
): Quota[] {
  return state.quotas.filter(
    (q) =>
      q.account === account.id &&
      !q.model &&
      Number.isFinite(Date.parse(q.observed_at)) &&
      now - Date.parse(q.observed_at) <=
        state.settings.quota_fresh_seconds * 1000 &&
      (q.reset_at.startsWith("0001") ||
        !q.reset_at ||
        Date.parse(q.reset_at) > now),
  );
}

export function accountQuotaPercent(
  account: Account,
  state: State,
  now: number,
): number | undefined {
  const values = freshAccountQuotas(account, state, now).flatMap((q) =>
    typeof q.used_percent === "number" && Number.isFinite(q.used_percent)
      ? [Math.max(0, Math.min(100, 100 - q.used_percent))]
      : [],
  );
  return values.length ? Math.min(...values) : undefined;
}

export function accountStatus(
  account: Account,
  state: State,
  now: number,
): string {
  if (account.replaced_by) return "replaced";
  if (account.paused || account.health === "disabled") return "disabled";
  if (account.health !== "active") return "problem";
  const fresh = freshAccountQuotas(account, state, now);
  const percent = accountQuotaPercent(account, state, now);
  if (
    percent === 0 ||
    fresh.some((q) => q.remaining !== undefined && q.remaining <= 0)
  )
    return "exhausted";
  if (percent !== undefined && percent < 20) return "low";
  if (
    percent === undefined &&
    !fresh.some(
      (q) => typeof q.remaining === "number" && Number.isFinite(q.remaining),
    )
  )
    return "unconfirmed";
  return "available";
}

export function accountMetrics(
  accounts: Account[],
  state: State,
  now: number,
): AccountMetrics {
  const statuses = accounts.map((a) => accountStatus(a, state, now));
  return {
    total: accounts.length,
    available: statuses.filter((s) => s === "available").length,
    needsAttention: statuses.filter((s) =>
      ["problem", "exhausted", "low"].includes(s),
    ).length,
    quotaRisk: statuses.filter((s) => ["exhausted", "low"].includes(s)).length,
    disabled: statuses.filter((s) => s === "disabled" || s === "replaced")
      .length,
    unconfirmed: statuses.filter((s) => s === "unconfirmed").length,
    needsInspectionAction: 0,
  };
}

export function filterAccountWorkspace(
  state: State,
  provider: string,
  filters: AccountFilters,
  now: number,
): Account[] {
  const query = filters.search.trim().toLocaleLowerCase();
  const rows = state.accounts.filter(
    (a) =>
      (provider === "all" || a.provider === provider) &&
      (!query ||
        [a.id, a.label, a.provider, a.workspace, a.plan].some((v) =>
          v?.toLocaleLowerCase().includes(query),
        )) &&
      (!filters.plan || a.plan === filters.plan) &&
      (!filters.pool ||
        state.pools.some(
          (p) => p.id === filters.pool && p.accounts.includes(a.id),
        )) &&
      (filters.health === "all" ||
        accountStatus(a, state, now) === filters.health),
  );
  const nextReset = (a: Account) =>
    Math.min(
      ...freshAccountQuotas(a, state, now)
        .map((q) => Date.parse(q.reset_at))
        .filter((t) => Number.isFinite(t) && t > now),
    );
  return rows.sort((a, b) => {
    let order = 0;
    switch (filters.sort) {
      case "quota":
        order =
          (accountQuotaPercent(a, state, now) ?? Infinity) -
          (accountQuotaPercent(b, state, now) ?? Infinity);
        break;
      case "reset":
        order = nextReset(a) - nextReset(b);
        break;
      case "recent":
        order =
          (Date.parse(b.last_traffic) || 0) - (Date.parse(a.last_traffic) || 0);
        break;
      case "requests":
        order = (b.requests ?? 0) - (a.requests ?? 0);
        break;
    }
    return (
      (Number.isNaN(order) ? 0 : order) ||
      a.label.localeCompare(b.label) ||
      a.id.localeCompare(b.id)
    );
  });
}
