import { act } from "react";
import { create, type ReactTestRenderer } from "react-test-renderer";
import { afterEach, describe, expect, it, vi } from "vitest";
import { AccountWorkspace } from "./AccountWorkspace";
import { Select } from "@/components/ui/Select";
import { Input } from "@/components/ui/Input";
import { PaginationControls } from "@/features/monitoring/components/MonitoringShared";
import type { State } from "./api";
import "@/i18n";

const state: State = {
  accounts: Array.from({ length: 60 }, (_, i) => ({
    id: `account-${i}`,
    label: `Account ${String(i).padStart(2, "0")}`,
    provider: "codex",
    health: "active",
    paused: i === 59,
    warm_enabled: false,
    last_traffic: "",
  })),
  pools: [],
  keys: [],
  quotas: [],
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
};
let view: ReactTestRenderer | undefined;
afterEach(async () => {
  if (view) await act(async () => view?.unmount());
  view = undefined;
  vi.unstubAllGlobals();
});
describe("original account workspace controls", () => {
  it("paginates, resets filters and clamps a shrinking refresh without losing search", async () => {
    vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
    const render = (value: State) => (
      <AccountWorkspace
        state={value}
        provider="all"
        onProviderChange={() => {}}
        resolvedTheme="light"
      >
        {(rows) => (
          <span
            data-workspace-rows
            data-visible={rows.map((a) => a.id).join(",")}
          />
        )}
      </AccountWorkspace>
    );
    await act(async () => {
      view = create(render(state));
    });
    const paging = () => view!.root.findByType(PaginationControls);
    expect(paging().props.count).toBe(60);
    expect(
      view!.root
        .findByProps({ "data-workspace-rows": true })
        .props["data-visible"].split(","),
    ).toHaveLength(25);
    await act(async () => paging().props.onPageChange(3));
    expect(paging().props.currentPage).toBe(3);
    await act(async () =>
      view!.root
        .findByType(Input)
        .props.onChange({ target: { value: "Account 5" } }),
    );
    expect(paging().props.currentPage).toBe(1);
    expect(paging().props.count).toBe(10);
    await act(async () =>
      view!.update(render({ ...state, accounts: state.accounts.slice(0, 51) })),
    );
    expect(view!.root.findByType(Input).props.value).toBe("Account 5");
    expect(paging().props.count).toBe(1);
    await act(async () =>
      view!.root
        .findAllByType(Select)
        .find((s) => s.props.ariaLabel === "Account status")!
        .props.onChange("disabled"),
    );
    expect(paging().props.count).toBe(0);
    expect(paging().props.currentPage).toBe(1);
  });
});
