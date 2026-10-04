import { describe, expect, it } from "vitest";
import {
  accountMetrics,
  accountStatus,
  emptyAccountFilters,
  filterAccountWorkspace,
} from "./accountWorkspace";
import type { Account, State } from "./api";

const now = Date.parse("2026-10-03T12:00:00Z");
const account = (id: string, extra: Partial<Account> = {}): Account => ({
  id,
  label: id,
  provider: "codex",
  health: "active",
  paused: false,
  warm_enabled: false,
  last_traffic: "",
  ...extra,
});
const state: State = {
  accounts: [
    account("unknown"),
    account("fresh", { plan: "plus", workspace: "work" }),
    account("spent"),
    account("paused", { paused: true }),
    account("model-only"),
    account("stale"),
    account("claude", { provider: "claude" }),
  ],
  keys: [],
  pools: [
    {
      id: "personal",
      name: "Personal",
      provider: "codex",
      accounts: ["fresh"],
      strategy: "native",
      models: [],
      reserve_percent: 0,
      sticky: false,
      warm: {
        enabled: false,
        model: "",
        prompt: "",
        window_seconds: 0,
        spacing_seconds: 0,
        idle_seconds: 0,
        cooldown_seconds: 0,
        jitter_seconds: 0,
      },
    },
  ],
  schedules: [],
  strategies: [],
  settings: {
    strategy: "native",
    quota_fresh_seconds: 900,
    sticky_seconds: 60,
    input_price_per_million: 0,
    output_price_per_million: 0,
    sticky_min_remaining_percent: 0,
    failure_penalty: 1,
    in_flight_penalty: 1,
  },
  quotas: [
    {
      account: "fresh",
      provider: "codex",
      key: "5h",
      used_percent: 10,
      observed_at: "2026-10-03T11:59:00Z",
      reset_at: "2026-10-03T14:00:00Z",
      source: "native",
    },
    {
      account: "spent",
      provider: "codex",
      key: "5h",
      used_percent: 100,
      observed_at: "2026-10-03T11:59:00Z",
      reset_at: "2026-10-03T13:00:00Z",
      source: "native",
    },
    {
      account: "model-only",
      provider: "codex",
      key: "5h:model",
      model: "restricted-model",
      used_percent: 100,
      observed_at: "2026-10-03T11:59:00Z",
      reset_at: "2026-10-03T13:00:00Z",
      source: "native",
    },
    {
      account: "stale",
      provider: "codex",
      key: "5h",
      used_percent: 100,
      observed_at: "2026-10-03T10:00:00Z",
      reset_at: "2026-10-03T14:00:00Z",
      source: "native",
    },
  ],
};

describe("native account workspace adaptation", () => {
  it("never promotes stale, unknown or model-only evidence to account-wide quota", () => {
    const statuses = Object.fromEntries(
      state.accounts.map((a) => [a.id, accountStatus(a, state, now)]),
    );
    expect(statuses).toMatchObject({
      unknown: "unconfirmed",
      fresh: "available",
      spent: "exhausted",
      paused: "disabled",
      "model-only": "unconfirmed",
      stale: "unconfirmed",
    });
    expect(accountMetrics(state.accounts, state, now)).toMatchObject({
      total: 7,
      available: 1,
      quotaRisk: 1,
      disabled: 1,
      unconfirmed: 4,
    });
  });
  it("combines provider, identity search, plan and pool without changing inventory", () => {
    expect(
      filterAccountWorkspace(
        state,
        "codex",
        {
          ...emptyAccountFilters,
          search: "work",
          pool: "personal",
          plan: "plus",
        },
        now,
      ).map((a) => a.id),
    ).toEqual(["fresh"]);
    expect(
      filterAccountWorkspace(
        state,
        "claude",
        { ...emptyAccountFilters, pool: "personal" },
        now,
      ),
    ).toEqual([]);
    expect(state.accounts[0].id).toBe("unknown");
  });
  it("sorts known quota/reset first and keeps deterministic ties", () => {
    expect(
      filterAccountWorkspace(
        state,
        "codex",
        { ...emptyAccountFilters, sort: "quota" },
        now,
      )
        .slice(0, 2)
        .map((a) => a.id),
    ).toEqual(["spent", "fresh"]);
    expect(
      filterAccountWorkspace(
        state,
        "codex",
        { ...emptyAccountFilters, health: "exhausted" },
        now,
      ).map((a) => a.id),
    ).toEqual(["spent"]);
    expect(
      filterAccountWorkspace(
        state,
        "codex",
        { ...emptyAccountFilters, sort: "reset" },
        now,
      )[0].id,
    ).toBe("spent");
  });
  it("treats passed resets as unknown, not fabricated fresh capacity", () => {
    expect(
      accountStatus(
        state.accounts[2],
        state,
        Date.parse("2026-10-03T13:01:00Z"),
      ),
    ).toBe("unconfirmed");
  });
});
