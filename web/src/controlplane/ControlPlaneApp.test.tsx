import { act } from "react";
import {
  create,
  type ReactTestRenderer,
  type ReactTestInstance,
} from "react-test-renderer";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ControlPlaneApp } from "./ControlPlaneApp";
import { api, type State, type Rollup, type RequestLog } from "./api";
import { AccountProviderTabs } from "@/features/accounts/components/AccountProviderTabs";
import { SegmentedTabs } from "@/components/ui/SegmentedTabs";
import { AccountDetails } from "./AccountDetails";
import { OAuthSession } from "./OAuthSession";
import { RuntimeSecurity } from "./RuntimeSecurity";
import type { ReactNode } from "react";

vi.mock("./api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("./api")>()),
  api: vi.fn(),
}));
vi.mock("@/components/charts/EChartsView", () => ({
  EChartsView: () => <div />,
}));
vi.mock("@/components/ui/Drawer", () => ({
  Drawer: ({ children }: { children: ReactNode }) => (
    <section>{children}</section>
  ),
}));
vi.mock("@/components/ui/Modal", () => ({
  Modal: ({ children, footer }: { children: ReactNode; footer: ReactNode }) => (
    <section>
      {children}
      {footer}
    </section>
  ),
}));
vi.mock("@/stores/useThemeStore", () => ({
  useThemeStore: (select: (s: unknown) => unknown) =>
    select({ initializeTheme: () => () => {}, cycleTheme: () => {} }),
}));

const state: State = {
  accounts: ["codex", "claude"].map((provider) => ({
    id: provider,
    provider,
    label: `${provider} account`,
    paused: false,
    health: "active",
    warm_enabled: false,
    last_traffic: "",
  })),
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

let renderer: ReactTestRenderer | undefined;
let pollers: (() => void)[] = [];
const textContent = (node: ReactTestInstance): string =>
  node.children
    .map((child) => (typeof child === "string" ? child : textContent(child)))
    .join("");
afterEach(async () => {
  if (renderer) await act(async () => renderer?.unmount());
  renderer = undefined;
  pollers = [];
  vi.unstubAllGlobals();
  vi.clearAllMocks();
});

async function login(rollups: Rollup[] = [], requests: RequestLog[] = []) {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  vi.stubGlobal("window", {
    setInterval: (callback: () => void) => {
      pollers.push(callback);
      return pollers.length;
    },
    clearInterval: () => {},
  });
  vi.mocked(api).mockImplementation(async (_secret, path) => {
    const params = new URLSearchParams(path.split("?")[1]);
    const filtered = rollups.filter(
      (r) =>
        (!params.get("until") || r.day <= params.get("until")!) &&
        (["provider", "model", "account", "pool", "api_key"] as const).every(
          (field) => !params.get(field) || r[field] === params.get(field),
        ),
    );
    return (
      path === "/state"
        ? state
        : path.startsWith("/analytics")
          ? {
              rows: filtered,
              totals: filtered.reduce(
                (v, r) => ({
                  ...v,
                  requests: v.requests + r.requests,
                  failures: v.failures + r.failures,
                  tokens: v.tokens + r.tokens,
                  cost: v.cost + r.cost,
                  latency_ms: v.latency_ms + r.latency_ms,
                }),
                {
                  day: "",
                  provider: "",
                  model: "",
                  account: "",
                  pool: "",
                  api_key: "",
                  requests: 0,
                  failures: 0,
                  tokens: 0,
                  cost: 0,
                  latency_ms: 0,
                },
              ),
              groups: filtered.length,
              next_offset: -1,
            }
          : path.startsWith("/requests") && !path.includes("before_id")
            ? requests
            : path.startsWith("/history")
              ? { rows: [], next: 0 }
              : []
    ) as never;
  });
  await act(async () => {
    renderer = create(<ControlPlaneApp />);
  });
  await act(async () =>
    renderer!.root
      .findByProps({ "aria-label": "Admin secret" })
      .props.onChange({ target: { value: "fixture-admin" } }),
  );
  await act(async () =>
    renderer!.root.findByType("form").props.onSubmit({ preventDefault() {} }),
  );
  return renderer!;
}

describe("production control-plane dashboard", () => {
  it("saves explicit reset policy and bounded phase preference through backend controls", async () => {
    const view = await login();
    const button = (label: string) =>
      view.root.findAllByType("button").find((b) => textContent(b) === label)!;
    await act(async () => button("Pools").props.onClick());
    await act(async () => button("Create pool").props.onClick());
    const reset = view.root
      .findAllByType("label")
      .find((node) => textContent(node).startsWith("Confirmed reset dispatch"))!
      .findByType("select");
    expect(reset.props.value).toBe("phased");
    await act(async () =>
      reset.props.onChange({ target: { value: "immediate" } }),
    );
    await act(async () =>
      view.root.findByType("form").props.onSubmit({ preventDefault() {} }),
    );
    expect(
      vi.mocked(api).mock.calls.find((call) => call[1] === "/pools")?.[3],
    ).toMatchObject({ warm: { reset_mode: "immediate" } });
    await act(async () => button("Routing Settings").props.onClick());
    const phase = view.root
      .findAllByType("label")
      .find((node) => textContent(node) === "phase_preference")!
      .findByType("input");
    expect(phase.props.max).toBe(0.05);
    await act(async () => phase.props.onChange({ target: { value: "0.03" } }));
    await act(async () =>
      view.root.findByType("form").props.onSubmit({ preventDefault() {} }),
    );
    expect(
      vi.mocked(api).mock.calls.find((call) => call[1] === "/settings")?.[3],
    ).toMatchObject({ phase_preference: 0.03 });
  });
  it("reports unresolved usage without fabricating tokens and clears late responses on sign-out", async () => {
    const view = await login();
    const original = vi.mocked(api).getMockImplementation()!;
    const row = {
      id: "unresolved-fixture",
      at: "2026-10-03T12:00:00Z",
      account: "codex",
      provider: "codex",
      model: "fixture-model",
      api_key: "key",
      state: "unresolved_restart",
    };
    vi.mocked(api).mockImplementation(async (...args) =>
      args[1] === "/executions/unresolved"
        ? ([row] as never)
        : original(...args),
    );
    await act(async () => pollers[0]());
    const button = (label: string) =>
      view.root.findAllByType("button").find((b) => textContent(b) === label)!;
    await act(async () => button("Monitoring").props.onClick());
    expect(textContent(view.root)).toContain("unresolved-fixture");
    expect(textContent(view.root)).toContain("Tokens and cost remain unknown");
    let finish!: (rows: unknown[]) => void;
    vi.mocked(api).mockImplementation((...args) =>
      args[1] === "/executions/unresolved"
        ? (new Promise((resolve) => {
            finish = resolve;
          }) as never)
        : original(...args),
    );
    await act(async () => pollers[0]());
    await act(async () => button("Sign out").props.onClick());
    await act(async () => finish([row]));
    expect(textContent(view.root)).not.toContain("unresolved-fixture");
  });
  it("targets account re-auth rather than launching an unbound add session", async () => {
    const view = await login();
    const original = vi.mocked(api).getMockImplementation()!;
    vi.mocked(api).mockImplementation(async (...args) =>
      args[1] === "/oauth/codex?account=codex"
        ? ({
            url: "https://auth.openai.com/authorize",
            state: "targeted-fixture",
          } as never)
        : original(...args),
    );
    const button = (label: string) =>
      view.root.findAllByType("button").find((b) => textContent(b) === label)!;
    await act(async () => button("Accounts").props.onClick());
    await act(async () => button("codex account").props.onClick());
    await act(async () =>
      view.root.findByType(AccountDetails).props.onStartOAuth("codex", "codex"),
    );
    expect(api).toHaveBeenCalledWith(
      "fixture-admin",
      "/oauth/codex?account=codex",
      "GET",
    );
    expect(view.root.findByType(OAuthSession).props.attempt.state).toBe(
      "targeted-fixture",
    );
  });
  it("cancels a pending provider session on sign-out without exposing its link to the next login", async () => {
    const view = await login();
    const original = vi.mocked(api).getMockImplementation()!;
    vi.mocked(api).mockImplementation(async (...args) =>
      args[1] === "/oauth/codex"
        ? ({
            url: "https://auth.openai.com/authorize",
            state: "pending-fixture",
          } as never)
        : args[1].startsWith("/oauth/status")
          ? ({ status: "wait" } as never)
          : original(...args),
    );
    const button = (label: string) =>
      view.root.findAllByType("button").find((b) => textContent(b) === label)!;
    await act(async () => button("Accounts").props.onClick());
    await act(async () => button("Add / re-auth codex").props.onClick());
    expect(view.root.findByType(OAuthSession).props.attempt.state).toBe(
      "pending-fixture",
    );
    await act(async () => button("Sign out").props.onClick());
    expect(api).toHaveBeenCalledWith(
      "fixture-admin",
      "/oauth/session?state=pending-fixture",
      "DELETE",
    );
    expect(view.root.findAllByType(OAuthSession)).toHaveLength(0);
  });
  it("cancels a launch that completes after sign-out instead of installing a stale dialog", async () => {
    const view = await login();
    const original = vi.mocked(api).getMockImplementation()!;
    let finish!: (result: unknown) => void;
    vi.mocked(api).mockImplementation((...args) =>
      args[1] === "/oauth/claude"
        ? (new Promise((resolve) => {
            finish = resolve;
          }) as never)
        : original(...args),
    );
    const button = (label: string) =>
      view.root.findAllByType("button").find((b) => textContent(b) === label)!;
    await act(async () => button("Accounts").props.onClick());
    await act(async () => button("Add / re-auth claude").props.onClick());
    await act(async () => button("Sign out").props.onClick());
    await act(async () =>
      finish({
        url: "https://claude.ai/oauth/authorize",
        state: "late-fixture",
      }),
    );
    expect(api).toHaveBeenCalledWith(
      "fixture-admin",
      "/oauth/session?state=late-fixture",
      "DELETE",
    );
    expect(view.root.findAllByType(OAuthSession)).toHaveLength(0);
    expect(
      view.root.findByProps({ "aria-label": "Admin secret" }).props.value,
    ).toBe("");
  });
  it("opens original account details on the selected logical account and clears them at sign out", async () => {
    const view = await login();
    const button = (label: string) =>
      view.root.findAllByType("button").find((b) => textContent(b) === label)!;
    await act(async () => button("Accounts").props.onClick());
    await act(async () => button("codex account").props.onClick());
    expect(view.root.findByType(AccountDetails).props.account.id).toBe("codex");
    const close = view.root.findByType(AccountDetails).props.onClose;
    await act(async () => button("Refresh").props.onClick());
    expect(view.root.findByType(AccountDetails).props.onClose).toBe(close);
    await act(async () => button("Sign out").props.onClick());
    expect(view.root.findAllByType(AccountDetails)).toHaveLength(0);
  });
  it("loads real security metadata through the original settings tabs", async () => {
    const view = await login();
    await act(async () =>
      view.root
        .findAllByType("button")
        .find((b) => textContent(b) === "Settings")!
        .props.onClick(),
    );
    const original = vi.mocked(api).getMockImplementation()!;
    const runtime = {
      admin_auth: "separate bearer credential",
      admin_secret_source: "CONTROL_PLANE_ADMIN_SECRET",
      admin_secret_min_bytes: 32,
      secret_rotation: "update environment and restart",
      tls_enabled: false,
      database: "fixture.sqlite",
      dashboard: "fixture.html",
      retention_days: 30,
      oauth_secrets_in_database: false,
      home_enabled: false,
    };
    vi.mocked(api).mockImplementation(async (...args) =>
      args[1] === "/runtime" ? (runtime as never) : original(...args),
    );
    await act(async () =>
      view.root.findByType(SegmentedTabs).props.onChange("security"),
    );
    expect(view.root.findByType(RuntimeSecurity).props.runtime).toEqual(
      runtime,
    );
    expect(
      view.root
        .findAllByType("button")
        .find((b) => textContent(b) === "Save settings")!.props.hidden,
    ).toBe(true);
    expect(api).toHaveBeenCalledWith(
      "fixture-admin",
      "/runtime",
      "GET",
      undefined,
    );
  });
  it("uses the original collapsible navigation and active-page semantics", async () => {
    const view = await login();
    const toggle = view.root.findByProps({ "aria-label": "Toggle navigation" });
    expect(toggle.props["aria-expanded"]).toBe(true);
    await act(async () => toggle.props.onClick());
    expect(toggle.props["aria-expanded"]).toBe(false);
    expect(
      view.root.findByProps({
        className: "app-shell cp-app sidebar-is-collapsed",
      }),
    ).toBeTruthy();
    const accounts = view.root
      .findAllByType("button")
      .find((b) => textContent(b) === "Accounts")!;
    await act(async () => accounts.props.onClick());
    expect(accounts.props["aria-current"]).toBe("page");
    expect(
      textContent(view.root.findByProps({ "aria-label": "Navigation" })),
    ).toBe("Accounts");
  });
  it("polls account snapshots without overlapping slow loads", async () => {
    await login();
    const original = vi.mocked(api).getMockImplementation()!;
    let resolve!: (snapshot: State) => void;
    const pending = new Promise<State>((r) => {
      resolve = r;
    });
    vi.mocked(api).mockImplementation(async (...args) =>
      args[1] === "/state" ? ((await pending) as never) : original(...args),
    );
    vi.mocked(api).mockClear();
    await act(async () => {
      pollers[0]();
      pollers[0]();
    });
    expect(
      vi.mocked(api).mock.calls.filter((call) => call[1] === "/state"),
    ).toHaveLength(1);
    await act(async () => resolve(state));
    await act(async () => pollers[0]());
    expect(
      vi.mocked(api).mock.calls.filter((call) => call[1] === "/state"),
    ).toHaveLength(2);
  });
  it("filters account rows through the original provider tabs", async () => {
    const view = await login();
    const accounts = view.root
      .findAllByType("button")
      .find((b) => textContent(b) === "Accounts")!;
    await act(async () => accounts.props.onClick());
    expect(view.root.findByType("tbody").findAllByType("tr")).toHaveLength(2);
    await act(async () =>
      view.root.findByType(AccountProviderTabs).props.onChange("codex"),
    );
    expect(view.root.findByType("tbody").findAllByType("tr")).toHaveLength(1);
    expect(textContent(view.root.findByType("tbody"))).toContain(
      "codex account",
    );
    expect(textContent(view.root.findByType("tbody"))).not.toContain(
      "claude account",
    );
  });
  it("does not restore previous-session history after sign out and sign in", async () => {
    const view = await login();
    const button = (text: string) =>
      view.root.findAllByType("button").find((b) => textContent(b) === text)!;
    await act(async () => button("Settings").props.onClick());
    let resolve!: (page: { rows: unknown[]; next: number }) => void;
    const pending = new Promise<{ rows: unknown[]; next: number }>((r) => {
      resolve = r;
    });
    const original = vi.mocked(api).getMockImplementation()!;
    vi.mocked(api).mockImplementation(async (...args) =>
      args[1].startsWith("/history/")
        ? ((await pending) as never)
        : original(...args),
    );
    await act(async () => button("Audit history").props.onClick());
    await act(async () => button("Sign out").props.onClick());
    await act(async () =>
      view.root
        .findByProps({ "aria-label": "Admin secret" })
        .props.onChange({ target: { value: "new-fixture-session" } }),
    );
    await act(async () =>
      view.root.findByType("form").props.onSubmit({ preventDefault() {} }),
    );
    await act(async () =>
      resolve({ rows: [{ action: "previous-session-evidence" }], next: 0 }),
    );
    expect(textContent(view.root)).not.toContain("previous-session-evidence");
    expect(
      view.root
        .findAllByType("button")
        .some((b) => textContent(b) === "Older history"),
    ).toBe(false);
  });

  it("pages audit history and clears pagination for runtime evidence", async () => {
    const view = await login();
    const button = (text: string) =>
      view.root.findAllByType("button").find((b) => textContent(b) === text)!;
    await act(async () => button("Settings").props.onClick());
    const original = vi.mocked(api).getMockImplementation()!;
    vi.mocked(api).mockImplementation(async (...args) =>
      args[1].startsWith("/history/audit/page")
        ? ({
            rows: [{ action: "fixture" }],
            next: args[1].includes("before=42") ? 0 : 42,
          } as never)
        : original(...args),
    );
    await act(async () => button("Audit history").props.onClick());
    expect(button("Newer history").props.disabled).toBe(true);
    await act(async () => button("Older history").props.onClick());
    expect(api).toHaveBeenCalledWith(
      "fixture-admin",
      "/history/audit/page?account=&before=42",
    );
    expect(button("Older history").props.disabled).toBe(true);
    await act(async () => button("Newer history").props.onClick());
    expect(button("Newer history").props.disabled).toBe(true);
    await act(async () =>
      button("Runtime security and deployment").props.onClick(),
    );
    expect(
      view.root
        .findAllByType("button")
        .some((b) => textContent(b) === "Older history"),
    ).toBe(false);
  });

  it("saves provider policy and retention through the settings backend", async () => {
    const view = await login();
    const button = (text: string) =>
      view.root.findAllByType("button").find((b) => textContent(b) === text)!;
    await act(async () => button("Settings").props.onClick());
    const retention = view.root
      .findAllByType("label")
      .find((l) => textContent(l).includes("Retention days"))!
      .findByType("input");
    await act(async () =>
      retention.props.onChange({ target: { value: "45" } }),
    );
    const disabled = view.root
      .findAllByType("label")
      .find(
        (l) =>
          textContent(l).includes("Disable provider dispatch") &&
          textContent(l.parent!).startsWith("codex"),
      )!
      .findByType("input");
    await act(async () =>
      disabled.props.onChange({ target: { checked: true } }),
    );
    await act(async () => button("Refresh").props.onClick());
    expect(retention.props.value).toBe(45);
    expect(disabled.props.checked).toBe(true);
    await act(async () => pollers[pollers.length - 2]());
    expect(retention.props.value).toBe(45);
    await act(async () =>
      view.root.findByType("form").props.onSubmit({ preventDefault() {} }),
    );
    expect(api).toHaveBeenCalledWith(
      "fixture-admin",
      "/settings",
      "PUT",
      expect.objectContaining({
        retention_days: 45,
        providers: expect.objectContaining({
          codex: { disabled: true, quota_refresh_seconds: 0 },
        }),
      }),
    );
  });

  it("discards unsaved settings explicitly rather than on live refresh", async () => {
    const view = await login();
    const button = (text: string) =>
      view.root.findAllByType("button").find((b) => textContent(b) === text)!;
    await act(async () => button("Settings").props.onClick());
    const retention = view.root
      .findAllByType("label")
      .find((l) => textContent(l).includes("Retention days"))!
      .findByType("input");
    await act(async () =>
      retention.props.onChange({ target: { value: "90" } }),
    );
    await act(async () => button("Refresh").props.onClick());
    expect(retention.props.value).toBe(90);
    await act(async () => button("Discard settings changes").props.onClick());
    expect(retention.props.value).toBe(0);
    expect(
      view.root
        .findAllByType("button")
        .some((b) => textContent(b) === "Discard settings changes"),
    ).toBe(false);
  });

  it("uses a timestamp/id cursor and resets request pagination after filter changes", async () => {
    const requests = Array.from({ length: 100 }, (_, i): RequestLog => ({
      id: `request-${i}`,
      at: "2026-10-03T00:00:00Z",
      provider: "codex",
      model: "model",
      actual_model: "model",
      account: "codex",
      pool: "pool",
      api_key: "key",
      status: 200,
      total_tokens: 1,
      input_tokens: 1,
      output_tokens: 0,
      reasoning_tokens: 0,
      cache_tokens: 0,
      latency_ms: 1,
      ttft_ms: 0,
    }));
    const view = await login([], requests);
    const button = (text: string) =>
      view.root.findAllByType("button").find((b) => textContent(b) === text)!;
    await act(async () => button("Monitoring").props.onClick());
    expect(button("Older requests").props.disabled).toBe(false);
    await act(async () => button("Older requests").props.onClick());
    expect(api).toHaveBeenCalledWith(
      "fixture-admin",
      expect.stringContaining("before_id=request-99"),
      "GET",
      undefined,
      expect.any(AbortSignal),
    );
    expect(textContent(view.root)).toContain("Page 2");
    expect(button("Older requests").props.disabled).toBe(true);
    await act(async () => button("Realtime feed").props.onClick());
    expect(textContent(view.root)).toContain("Page 1");
    expect(button("Older requests").props.disabled).toBe(true);
    expect(button("Persistent history").props["aria-selected"]).toBe(false);
    await act(async () => button("Persistent history").props.onClick());
    expect(button("Older requests").props.disabled).toBe(false);
    const provider = view.root
      .findAllByType("label")
      .find((l) => l.children.includes("provider"))!
      .findByType("input");
    await act(async () =>
      provider.props.onChange({ target: { value: "codex" } }),
    );
    expect(textContent(view.root)).toContain("Page 1");
    const paths = vi
      .mocked(api)
      .mock.calls.filter((call) => call[1].startsWith("/requests"))
      .map((call) => call[1]);
    expect(paths[paths.length - 1]).not.toContain("before_id");
  });
  it("filters analytics by provider and end date without fabricating quota", async () => {
    const view = await login(
      ["codex", "claude"].map((provider) => ({
        day: "2026-10-02",
        provider,
        model: "model",
        account: provider,
        pool: "pool",
        api_key: "key",
        requests: 1,
        failures: 0,
        tokens: 2,
        latency_ms: 10,
        cost: 0,
      })),
    );
    expect(textContent(view.root)).toContain("unknown/stale accounts");
    await act(async () =>
      view.root
        .findAllByType("button")
        .find((b) => textContent(b) === "Usage Analytics")!
        .props.onClick(),
    );
    const provider = view.root
      .findAllByType("label")
      .find((l) => l.children.includes("provider"))!
      .findByType("input");
    await act(async () =>
      provider.props.onChange({ target: { value: "codex" } }),
    );
    const rows = () => view.root.findByType("tbody").findAllByType("tr");
    expect(rows()).toHaveLength(1);
    expect(textContent(rows()[0])).toContain("codex account");
    const end = view.root
      .findAllByType("input")
      .filter((i) => i.props.type === "date")[1];
    await act(async () =>
      end.props.onChange({ target: { value: "2026-10-01" } }),
    );
    expect(rows()).toHaveLength(0);
  });
  it("loads the built-in API and discards admin state on sign out", async () => {
    const view = await login();
    expect(api).toHaveBeenCalledWith(
      "fixture-admin",
      "/state",
      "GET",
      undefined,
      expect.any(AbortSignal),
    );
    const signOut = view.root
      .findAllByType("button")
      .find((button) => textContent(button) === "Sign out")!;
    await act(async () => signOut.props.onClick());
    expect(
      view.root.findByProps({ "aria-label": "Admin secret" }).props.value,
    ).toBe("");
  });

  it("keeps Codex and Claude account selectors independent and sends explicit bindings", async () => {
    const view = await login();
    const click = async (text: string) => {
      const button = view.root
        .findAllByType("button")
        .find((b) => textContent(b) === text)!;
      await act(async () => button.props.onClick());
    };
    await click("API Keys");
    await click("Create key");
    const labels = view.root.findAllByType("label");
    const name = labels
      .find((label) => label.children.includes("Name"))!
      .findByType("input");
    await act(async () =>
      name.props.onChange({ target: { value: "scope-test" } }),
    );
    for (const provider of ["codex", "claude"]) {
      const input = labels
        .find((label) => label.children.includes(`${provider} account`))!
        .findByType("input");
      await act(async () =>
        input.props.onChange({ target: { checked: true } }),
      );
    }
    await act(async () =>
      view.root.findByType("form").props.onSubmit({ preventDefault() {} }),
    );
    expect(api).toHaveBeenCalledWith(
      "fixture-admin",
      "/keys",
      "POST",
      expect.objectContaining({
        name: "scope-test",
        bindings: {
          codex: { accounts: ["codex"], pools: [] },
          claude: { accounts: ["claude"], pools: [] },
        },
      }),
    );
  });
});
