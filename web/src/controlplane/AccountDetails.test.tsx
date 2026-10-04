import { act, type ReactNode } from "react";
import { create, type ReactTestRenderer } from "react-test-renderer";
import { afterEach, describe, expect, it, vi } from "vitest";
import { AccountDetails } from "./AccountDetails";
import { Button } from "@/components/ui/Button";
import type { Account, State } from "./api";

vi.mock("@/components/ui/Drawer", () => ({
  Drawer: ({ children }: { children: ReactNode }) => (
    <section>{children}</section>
  ),
}));
vi.mock("@/features/accounts/components/QuotaWindowCard", () => ({
  QuotaWindowCard: () => <div />,
}));

const account: Account = {
  id: "original",
  provider: "claude",
  label: "Original",
  paused: false,
  health: "active",
  warm_enabled: false,
  last_traffic: "",
  identity_evidence: "credential_reference",
};
const state: State = {
  accounts: [
    account,
    { ...account, id: "new", label: "New", paused: true },
    { ...account, id: "verified", identity_evidence: "provider", paused: true },
    { ...account, id: "used", paused: true, requests: 1 },
    { ...account, id: "other", provider: "codex", paused: true },
  ],
  pools: [],
  keys: [],
  quotas: [],
  schedules: [],
  strategies: ["native"],
  settings: {
    strategy: "native",
    quota_fresh_seconds: 900,
    sticky_seconds: 3600,
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

describe("original account drawer integration", () => {
  it("requires exact old identity confirmation and only offers unused fallback replacements", async () => {
    vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
    const mutate = vi.fn().mockResolvedValue({ ok: true });
    await act(async () => {
      view = create(
        <AccountDetails
          account={account}
          state={state}
          busy={false}
          onClose={() => {}}
          mutate={mutate}
        />,
      );
    });
    await act(async () =>
      view!.root
        .findByProps({
          role: "tab",
          id: "control-plane-account-details-reauth",
        })
        .props.onClick(),
    );
    expect(
      view!.root.findAllByType("option").map((o) => o.props.value),
    ).toEqual(["", "new"]);
    await act(async () =>
      view!.root
        .findByType("select")
        .props.onChange({ target: { value: "new" } }),
    );
    const submit = () =>
      view!.root
        .findAllByType("button")
        .find((b) => b.props.type === "submit")!;
    expect(submit().props.disabled).toBe(true);
    await act(async () =>
      view!.root
        .findByType("input")
        .props.onChange({ target: { value: "original" } }),
    );
    expect(submit().props.disabled).toBe(false);
    await act(async () =>
      view!.root.findByType("form").props.onSubmit({ preventDefault() {} }),
    );
    expect(mutate).toHaveBeenCalledWith(
      "/accounts/original/reassociate",
      "POST",
      { source: "new", confirmation: "original" },
    );
    expect(view!.root.findByType("select").props.value).toBe("");
  });
  it("keeps native quota refresh on the selected logical account", async () => {
    vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
    const mutate = vi.fn().mockResolvedValue({ ok: true });
    await act(async () => {
      view = create(
        <AccountDetails
          account={account}
          state={state}
          busy={false}
          onClose={() => {}}
          mutate={mutate}
        />,
      );
    });
    await act(async () =>
      view!.root
        .findByProps({ role: "tab", id: "control-plane-account-details-quota" })
        .props.onClick(),
    );
    await act(async () =>
      view!.root
        .findAllByType(Button)
        .find((b) => b.props.children === "Refresh native quota")!
        .props.onClick(),
    );
    expect(mutate).toHaveBeenCalledWith("/accounts/original/quota");
  });
});
