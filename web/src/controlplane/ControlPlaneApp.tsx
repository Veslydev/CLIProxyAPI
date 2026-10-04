import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type FormEvent,
  type ReactNode,
} from "react";
import { Card } from "@/components/ui/Card";
import { HistoryTable } from "./HistoryTable";
import { WarmerTimeline } from "./WarmerTimeline";
import { AccountDetails } from "./AccountDetails";
import { OAuthSession } from "./OAuthSession";
import { safeOAuthLink, type OAuthAttempt } from "./oauthSession";
import { RuntimeSecurity } from "./RuntimeSecurity";
import { isRuntimeInfo, type RuntimeInfo } from "./runtimeInfo";
import { Button } from "@/components/ui/Button";
import { SegmentedTabs } from "@/components/ui/SegmentedTabs";
import { SummaryCard } from "@/features/monitoring/components/MonitoringShared";
import { useMediaQuery } from "@/hooks/useMediaQuery";
import { EChartsView } from "@/components/charts/EChartsView";
import { QuotaWindowCard } from "@/features/accounts/components/QuotaWindowCard";
import { AccountHealthBadge } from "@/features/accounts/components/AccountHealthBadge";
import { CopyableText } from "@/features/accounts/components/CopyableText";
import { AccountWorkspace } from "./AccountWorkspace";
import { pages, useDashboardNavigation } from "./navigation";
import { quotaWindowView } from "./quotaWindowView";
import "@/i18n";
import { MonitoringTabsBar } from "@/features/monitoring/components/MonitoringTabsBar";
import { useThemeStore } from "@/stores/useThemeStore";
import {
  CPAMP_SYMBOL_COLOR_PNG_URL,
  CPAMP_WORDMARK_COLOR_PNG_URL,
  CPAMP_WORDMARK_ON_DARK_PNG_URL,
} from "@/assets/brand";
import {
  IconSidebarDashboard,
  IconSidebarMonitor,
  IconSidebarUsage,
  IconSidebarAuthFiles,
  IconSidebarConfig,
  IconSidebarSystem,
} from "@/components/ui/icons";
import {
  api,
  emptyWarm,
  type State,
  type APIKey,
  type Pool,
  type Rollup,
  type RequestLog,
  type Execution,
} from "./api";
import "./controlplane.scss";

const icons: ReactNode[] = [
  <IconSidebarDashboard />,
  <IconSidebarMonitor />,
  <IconSidebarUsage />,
  <IconSidebarAuthFiles />,
  <IconSidebarConfig />,
  <IconSidebarAuthFiles />,
  <IconSidebarConfig />,
  <IconSidebarSystem />,
];
const date = (value: string) =>
  value && !value.startsWith("0001")
    ? new Date(value).toLocaleString()
    : "Unknown";

function Table({ headers, rows }: { headers: string[]; rows: ReactNode[][] }) {
  return (
    <div className="cp-table">
      <table>
        <thead>
          <tr>
            {headers.map((h) => (
              <th key={h}>{h}</th>
            ))}
          </tr>
        </thead>
        <tbody>
          {rows.map((row, i) => (
            <tr key={i}>
              {row.map((cell, j) => (
                <td key={j}>{cell}</td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
      {rows.length === 0 && <p>No records.</p>}
    </div>
  );
}

export function ControlPlaneApp() {
  const initializeTheme = useThemeStore((s) => s.initializeTheme);
  const cycleTheme = useThemeStore((s) => s.cycleTheme);
  useEffect(() => initializeTheme(), [initializeTheme]);
  const [secret, setSecret] = useState("");
  const [draftSecret, setDraftSecret] = useState("");
  const [state, setState] = useState<State>();
  const [page, setPage] = useDashboardNavigation();
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [version, setVersion] = useState(0);
  const [logs, setLogs] = useState<RequestLog[]>([]);
  const [unresolved, setUnresolved] = useState<Execution[]>([]);
  const [rawRollups, setRollups] = useState<Rollup[]>([]);
  const [analyticsEnd, setAnalyticsEnd] = useState("");
  const [analyticsPage, setAnalyticsPage] = useState<{
    filter: typeof filter;
    end: string;
    offset: number;
  }>();
  const [analyticsResult, setAnalyticsResult] = useState<{
    totals: Rollup;
    groups: number;
    next_offset: number;
    daily?: Rollup[];
  }>();
  const [filter, setFilter] = useState({
    provider: "",
    model: "",
    account: "",
    pool: "",
    api_key: "",
    status: "",
    since: "",
  });
  const [requestPages, setRequestPages] = useState<{
    filter: typeof filter;
    cursors: { before: number; id: string }[];
  }>({ filter, cursors: [] });
  const [monitoringTab, setMonitoringTab] = useState<"history" | "live">(
    "history",
  );
  const requestCursors =
    requestPages.filter === filter ? requestPages.cursors : [];
  const requestCursor = requestCursors[requestCursors.length - 1];
  const [loadedRequestPage, setLoadedRequestPage] = useState<{
    filter: typeof filter;
    cursor: typeof requestCursor;
  }>();
  const [keyDraft, setKeyDraft] = useState<APIKey>();
  const [accountProvider, setAccountProvider] = useState("all");
  const [detailAccountID, setDetailAccountID] = useState<string>();
  const closeAccountDetails = useCallback(
    () => setDetailAccountID(undefined),
    [],
  );
  const [sidebarCollapsed, setSidebarCollapsed] = useState(false);
  const [sidebarOpen, setSidebarOpen] = useState(false);
  const mobile = useMediaQuery("(max-width: 768px)");
  useEffect(() => {
    if (!sidebarOpen || typeof document === "undefined") return;
    const close = (event: KeyboardEvent) => {
      if (event.key === "Escape") setSidebarOpen(false);
    };
    document.addEventListener("keydown", close);
    return () => document.removeEventListener("keydown", close);
  }, [sidebarOpen]);
  const resolvedTheme = useThemeStore((s) => s.resolvedTheme);
  const [poolDraft, setPoolDraft] = useState<Pool>();
  const [newSecret, setNewSecret] = useState("");
  const [history, setHistory] = useState<unknown[]>([]);
  const [runtimeInfo, setRuntimeInfo] = useState<RuntimeInfo>();
  const [settingsTab, setSettingsTab] = useState<
    "all" | "providers" | "pricing" | "security" | "database"
  >("all");
  const [historyPage, setHistoryPage] = useState<{
    kind: string;
    account: string;
    next: number;
    cursors: number[];
  }>();
  const evidenceHistory = (rows: unknown[]) => {
    setHistory(rows);
    setHistoryPage(undefined);
  };
  const [oauth, setOauth] = useState<OAuthAttempt>();
  const oauthLaunch = useRef<number | undefined>(undefined);
  const closeOAuth = useCallback(() => setOauth(undefined), []);
  const settingsRevision = useRef(0);
  const sessionEpoch = useRef(0);
  const savedSettingsRevision = useRef(0);
  const [settingsDirty, setSettingsDirty] = useState(false);
  const editSettings = (next: State) => {
    settingsRevision.current++;
    setSettingsDirty(true);
    setState(next);
  };
  const refresh = useCallback(() => setVersion((v) => v + 1), []);
  const act = async (path: string, method = "POST", body?: unknown) => {
    setBusy(true);
    setError("");
    const revision = settingsRevision.current;
    const epoch = sessionEpoch.current;
    try {
      const result = await api<unknown>(secret, path, method, body);
      if (epoch !== sessionEpoch.current) return undefined;
      if (path === "/settings" && method === "PUT") {
        savedSettingsRevision.current = revision;
        setSettingsDirty(settingsRevision.current !== revision);
      }
      refresh();
      return result;
    } catch (e) {
      if (epoch !== sessionEpoch.current) return undefined;
      setError(e instanceof Error ? e.message : String(e));
      return undefined;
    } finally {
      if (epoch === sessionEpoch.current) setBusy(false);
    }
  };
  const startOAuth = async (provider: string, account?: string) => {
    const epoch = sessionEpoch.current;
    if (oauth || oauthLaunch.current === epoch) return;
    oauthLaunch.current = epoch;
    const credential = secret;
    setBusy(true);
    setError("");
    try {
      const result = await api<unknown>(
        credential,
        `/oauth/${provider}${account ? `?account=${encodeURIComponent(account)}` : ""}`,
        "GET",
      );
      const state =
        result &&
        typeof result === "object" &&
        "state" in result &&
        typeof result.state === "string"
          ? result.state
          : "";
      if (epoch !== sessionEpoch.current) {
        // A launch can finish after sign-out. Cancel its backend waiter without
        // importing the old operation's link or errors into the next session.
        if (state)
          await api(
            credential,
            `/oauth/session?state=${encodeURIComponent(state)}`,
            "DELETE",
          );
        return;
      }
      if (
        result &&
        typeof result === "object" &&
        "url" in result &&
        typeof result.url === "string" &&
        safeOAuthLink(result.url) &&
        state
      ) {
        setOauth({ provider, url: result.url, state });
      } else {
        if (state)
          await api(
            credential,
            `/oauth/session?state=${encodeURIComponent(state)}`,
            "DELETE",
          );
        setError("Provider did not return a valid secure OAuth session.");
      }
    } catch {
      if (epoch === sessionEpoch.current)
        setError(
          "Unable to start provider sign-in. Verify the proxy OAuth configuration and retry.",
        );
    } finally {
      if (oauthLaunch.current === epoch) oauthLaunch.current = undefined;
      if (epoch === sessionEpoch.current) setBusy(false);
    }
  };
  useEffect(() => {
    if (!secret) return;
    const controller = new AbortController();
    const analyticsParams = new URLSearchParams();
    if (filter.since) analyticsParams.set("since", filter.since);
    if (analyticsEnd) analyticsParams.set("until", analyticsEnd);
    const offset =
      analyticsPage?.filter === filter && analyticsPage.end === analyticsEnd
        ? analyticsPage.offset
        : 0;
    analyticsParams.set("offset", String(offset));
    analyticsParams.set("limit", "100");
    for (const field of [
      "provider",
      "model",
      "account",
      "pool",
      "api_key",
    ] as const) {
      if (filter[field]) analyticsParams.set(field, filter[field]);
    }
    let loading = false;
    const load = () => {
      if (loading || controller.signal.aborted) return;
      loading = true;
      void Promise.all([
        api<State>(secret, "/state", "GET", undefined, controller.signal),
        api<Execution[]>(
          secret,
          "/executions/unresolved",
          "GET",
          undefined,
          controller.signal,
        ),
        api<{
          rows: Rollup[];
          totals: Rollup;
          groups: number;
          next_offset: number;
          daily?: Rollup[];
        }>(
          secret,
          `/analytics/page?${analyticsParams}`,
          "GET",
          undefined,
          controller.signal,
        ),
      ])
        .then(([s, pending, r]) => {
          if (controller.signal.aborted) return;
          setState((previous) =>
            previous &&
            settingsRevision.current !== savedSettingsRevision.current
              ? { ...s, settings: previous.settings }
              : s,
          );
          setRollups(r.rows);
          setUnresolved(pending);
          setAnalyticsResult(r);
          setError("");
        })
        .catch((e) => {
          if (!controller.signal.aborted) setError(String(e.message));
        })
        .finally(() => {
          loading = false;
        });
    };
    load();
    const timer = window.setInterval(load, 5000);
    return () => {
      controller.abort();
      window.clearInterval(timer);
    };
  }, [secret, version, filter, analyticsEnd, analyticsPage]);
  useEffect(() => {
    if (!secret) return;
    const controller = new AbortController();
    const params = new URLSearchParams();
    Object.entries(filter).forEach(([k, v]) => {
      if (v)
        params.set(k, k === "since" ? String(new Date(v).getTime() / 1000) : v);
    });
    params.set("limit", "100");
    if (requestCursor) {
      params.set("before", String(requestCursor.before));
      params.set("before_id", requestCursor.id);
    }
    let loading = false;
    const load = () => {
      if (loading) return;
      loading = true;
      return api<RequestLog[]>(
        secret,
        `/requests?${params}`,
        "GET",
        undefined,
        controller.signal,
      )
        .then((rows) => {
          if (controller.signal.aborted) return;
          setLogs(rows);
          setLoadedRequestPage({ filter, cursor: requestCursor });
        })
        .catch((e) => {
          if (!controller.signal.aborted) setError(String(e.message));
        })
        .finally(() => {
          loading = false;
        });
    };
    void load();
    const timer = window.setInterval(() => {
      void load();
    }, 5000);
    return () => {
      controller.abort();
      window.clearInterval(timer);
    };
  }, [secret, version, filter, requestCursor]);
  const rollups = useMemo(
    () =>
      rawRollups.filter(
        (r) =>
          (!analyticsEnd || r.day <= analyticsEnd) &&
          (!filter.provider || r.provider === filter.provider) &&
          (!filter.model || r.model === filter.model) &&
          (!filter.account || r.account === filter.account) &&
          (!filter.pool || r.pool === filter.pool) &&
          (!filter.api_key || r.api_key === filter.api_key),
      ),
    [rawRollups, analyticsEnd, filter],
  );
  const totals = useMemo(
    () =>
      analyticsResult
        ? {
            requests: analyticsResult.totals.requests,
            failures: analyticsResult.totals.failures,
            tokens: analyticsResult.totals.tokens,
            cost: analyticsResult.totals.cost,
            latency: analyticsResult.totals.latency_ms,
          }
        : rollups.reduce(
            (v, r) => ({
              requests: v.requests + r.requests,
              failures: v.failures + r.failures,
              tokens: v.tokens + r.tokens,
              cost: v.cost + r.cost,
              latency: v.latency + r.latency_ms,
            }),
            { requests: 0, failures: 0, tokens: 0, cost: 0, latency: 0 },
          ),
    [rollups, analyticsResult],
  );
  const providers = [
    ...new Set(state?.accounts.map((a) => a.provider) ?? []),
  ].sort();
  const accountName = (id: string) =>
    state?.accounts.find((a) => a.id === id)?.label ?? id;
  const quotaSummary = (ids: string[]) => {
    const windows = state?.quotas.filter((q) => ids.includes(q.account)) ?? [];
    const fresh = windows.filter(
      (q) =>
        Date.now() - new Date(q.observed_at).getTime() <=
          (state?.settings.quota_fresh_seconds ?? 0) * 1000 &&
        (q.reset_at.startsWith("0001") ||
          new Date(q.reset_at).getTime() > Date.now()),
    );
    const exhausted = new Set(
      fresh
        .filter((q) =>
          q.used_percent !== undefined
            ? q.used_percent >= 100
            : q.remaining !== undefined && q.remaining <= 0,
        )
        .map((q) => q.account),
    );
    return `${fresh.length} fresh windows · ${exhausted.size} accounts with exhausted windows · ${ids.filter((id) => !fresh.some((q) => q.account === id)).length} unknown/stale accounts`;
  };
  const authenticate = (e: FormEvent) => {
    e.preventDefault();
    sessionEpoch.current++;
    setError("");
    setSecret(draftSecret);
    setDraftSecret("");
  };
  const showHistory = async (
    kind: string,
    account = "",
    cursors: number[] = [],
  ) => {
    const epoch = sessionEpoch.current;
    try {
      const result = await api<{ rows: unknown[]; next: number }>(
        secret,
        `/history/${kind}/page?account=${encodeURIComponent(account)}&before=${cursors[cursors.length - 1] || 0}`,
      );
      if (epoch !== sessionEpoch.current) return;
      setHistory(result.rows);
      setHistoryPage({ kind, account, next: result.next, cursors });
    } catch (e) {
      if (epoch === sessionEpoch.current) setError(String(e));
    }
  };
  const detailAccount = state?.accounts.find((a) => a.id === detailAccountID);
  if (!secret)
    return (
      <div className="cp-login">
        <Card title="CLIProxyAPI · CPA Manager Plus">
          <form onSubmit={authenticate}>
            <p>
              Built-in control plane. Use the separate admin secret, not an
              inference API key.
            </p>
            {error && (
              <p role="alert" className="cp-error">
                {error}
              </p>
            )}
            <input
              aria-label="Admin secret"
              type="password"
              autoComplete="off"
              value={draftSecret}
              onChange={(e) => setDraftSecret(e.target.value)}
              required
            />
            <Button type="submit">Sign in</Button>
          </form>
        </Card>
      </div>
    );
  return (
    <div
      className={`app-shell cp-app ${sidebarCollapsed ? "sidebar-is-collapsed" : ""}`}
    >
      <header className="main-header">
        <div className="navbar">
          <div className="navbar-left">
            <button
              type="button"
              className="hamburger-container"
              aria-label="Toggle navigation"
              aria-expanded={mobile ? sidebarOpen : !sidebarCollapsed}
              aria-controls="control-plane-sidebar"
              onClick={() =>
                mobile
                  ? setSidebarOpen((value) => !value)
                  : setSidebarCollapsed((value) => !value)
              }
            >
              <svg
                width="20"
                height="20"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                aria-hidden="true"
              >
                <rect x="3" y="4" width="18" height="16" rx="2" />
                <path d="M9 4v16" />
                <path
                  d={sidebarCollapsed ? "m13 9 3 3-3 3" : "m16 9-3 3 3 3"}
                />
              </svg>
            </button>
            <nav className="app-breadcrumb" aria-label="Navigation">
              <span className="breadcrumb-item">{page}</span>
            </nav>
          </div>
          <div className="navbar-right">
            <strong>CLIProxyAPI · Control Plane</strong>
            <Button variant="ghost" onClick={cycleTheme}>
              Theme
            </Button>
            <Button variant="ghost" onClick={refresh}>
              Refresh
            </Button>
            <Button
              variant="ghost"
              onClick={() => {
                const logoutEpoch = sessionEpoch.current + 1;
                if (oauth)
                  void api(
                    secret,
                    `/oauth/session?state=${encodeURIComponent(oauth.state)}`,
                    "DELETE",
                  ).catch(() => {
                    if (sessionEpoch.current === logoutEpoch)
                      setError(
                        "Signed out, but OAuth cancellation could not be confirmed. The provider session may remain pending until it expires.",
                      );
                  });
                setSecret("");
                setSidebarOpen(false);
                sessionEpoch.current++;
                setBusy(false);
                setState(undefined);
                settingsRevision.current = 0;
                savedSettingsRevision.current = 0;
                setSettingsDirty(false);
                setNewSecret("");
                setKeyDraft(undefined);
                setAccountProvider("all");
                setDetailAccountID(undefined);
                setPoolDraft(undefined);
                setLogs([]);
                setUnresolved([]);
                setRequestPages({ filter, cursors: [] });
                setMonitoringTab("history");
                setLoadedRequestPage(undefined);
                setRollups([]);
                setAnalyticsResult(undefined);
                setAnalyticsPage(undefined);
                evidenceHistory([]);
                setRuntimeInfo(undefined);
                setSettingsTab("all");
                setOauth(undefined);
                setError("");
              }}
            >
              Sign out
            </Button>
          </div>
        </div>
      </header>
      <div className="main-body">
        <button
          type="button"
          className={`sidebar-backdrop ${sidebarOpen ? "visible" : ""}`}
          aria-label="Close navigation"
          aria-hidden={!sidebarOpen}
          tabIndex={sidebarOpen ? 0 : -1}
          onClick={() => setSidebarOpen(false)}
        />
        <aside
          id="control-plane-sidebar"
          className={`sidebar ${sidebarOpen ? "open" : ""} ${sidebarCollapsed ? "collapsed" : ""}`}
          inert={mobile && !sidebarOpen}
        >
          <div className="sidebar-brand">
            <div className="sidebar-brand-main">
              <img
                src={CPAMP_SYMBOL_COLOR_PNG_URL}
                alt=""
                className="sidebar-brand-symbol"
              />
              <img
                src={
                  resolvedTheme === "dark"
                    ? CPAMP_WORDMARK_ON_DARK_PNG_URL
                    : CPAMP_WORDMARK_COLOR_PNG_URL
                }
                alt="CPA Manager Plus"
                className="sidebar-brand-wordmark"
              />
            </div>
          </div>
          <div className="nav-section">
            {pages.map((p, i) => (
              <button
                key={p}
                className={`nav-item ${p === page ? "active" : ""}`}
                title={p}
                aria-current={p === page ? "page" : undefined}
                onClick={() => {
                  setPage(p);
                  setSidebarOpen(false);
                  evidenceHistory([]);
                }}
              >
                <span className="nav-icon">{icons[i]}</span>
                <span className="nav-label">{p}</span>
              </button>
            ))}
          </div>
        </aside>
        <div className="content">
          <main className="main-content">
            <h1>{page}</h1>
            {error && (
              <div className="cp-error" role="alert">
                {error}
              </div>
            )}
            {!state ? (
              <p>Loading…</p>
            ) : (
              <>
                {(page === "Overview" || page === "Usage Analytics") && (
                  <>
                    <label>
                      From{" "}
                      <input
                        type="date"
                        value={filter.since}
                        onChange={(e) =>
                          setFilter({ ...filter, since: e.target.value })
                        }
                      />
                    </label>
                    <label>
                      To{" "}
                      <input
                        type="date"
                        value={analyticsEnd}
                        onChange={(e) => setAnalyticsEnd(e.target.value)}
                      />
                    </label>
                    {page === "Usage Analytics" && (
                      <div className="cp-filters">
                        {(
                          [
                            "provider",
                            "model",
                            "account",
                            "pool",
                            "api_key",
                          ] as const
                        ).map((field) => (
                          <label key={field}>
                            {field}
                            <input
                              value={filter[field]}
                              onChange={(e) =>
                                setFilter({
                                  ...filter,
                                  [field]: e.target.value,
                                })
                              }
                            />
                          </label>
                        ))}
                        <small>
                          Exact identifiers; totals include every matching
                          retained daily rollup.
                        </small>
                      </div>
                    )}
                    <div className="cp-stats">
                      {[
                        ["Requests", totals.requests],
                        [
                          "Success rate",
                          totals.requests
                            ? `${((1 - totals.failures / totals.requests) * 100).toFixed(1)}%`
                            : "Unknown",
                        ],
                        ["Tokens", totals.tokens],
                        [
                          "Estimated cost",
                          state.settings.input_price_per_million ||
                          state.settings.output_price_per_million ||
                          state.settings.pricing?.length
                            ? `$${totals.cost.toFixed(4)}`
                            : "Unknown",
                        ],
                        [
                          "Mean latency",
                          totals.requests
                            ? `${Math.round(totals.latency / totals.requests)} ms`
                            : "Unknown",
                        ],
                        [
                          "Active / limited accounts",
                          `${state.accounts.filter((a) => !a.paused && a.health === "active").length} / ${state.accounts.filter((a) => a.health === "limited").length}`,
                        ],
                      ].map(([title, value], index) => (
                        <SummaryCard
                          key={String(title)}
                          label={String(title)}
                          value={String(value)}
                          meta={
                            index === 3
                              ? "Priced usage only · operator estimate, not an invoice"
                              : "Matching retained evidence"
                          }
                          icon={
                            (
                              [
                                "calls",
                                "success",
                                "tokens",
                                "cost",
                                "sampled",
                                "credential",
                              ] as const
                            )[index]
                          }
                          accent={
                            (
                              [
                                "blue",
                                "green",
                                "cyan",
                                "violet",
                                "amber",
                                "indigo",
                              ] as const
                            )[index]
                          }
                        />
                      ))}
                    </div>
                    <Card title="Quota summary">
                      {providers.map((provider) => (
                        <p key={provider}>
                          <strong>{provider}</strong>:{" "}
                          {quotaSummary(
                            state.accounts
                              .filter((a) => a.provider === provider)
                              .map((a) => a.id),
                          )}
                        </p>
                      ))}
                      <small>
                        Model-specific exhaustion does not imply account-wide
                        unavailability.
                      </small>
                    </Card>
                    <Card title="Request traffic · matching daily totals">
                      <EChartsView
                        ariaLabel="Requests by day"
                        style={{ height: 280 }}
                        option={{
                          tooltip: { trigger: "axis" },
                          xAxis: {
                            type: "category",
                            data: [
                              ...new Set(
                                (analyticsResult?.daily ?? rollups).map(
                                  (r) => r.day,
                                ),
                              ),
                            ].sort(),
                          },
                          yAxis: { type: "value" },
                          series: [
                            {
                              type: "bar",
                              data: [
                                ...new Set(
                                  (analyticsResult?.daily ?? rollups).map(
                                    (r) => r.day,
                                  ),
                                ),
                              ]
                                .sort()
                                .map((day) =>
                                  (analyticsResult?.daily ?? rollups)
                                    .filter((r) => r.day === day)
                                    .reduce((n, r) => n + r.requests, 0),
                                ),
                              itemStyle: { color: "#5a78ed" },
                            },
                          ],
                        }}
                      />
                    </Card>
                    <Card title="Usage breakdown">
                      <Table
                        headers={[
                          "Day",
                          "Provider",
                          "Model",
                          "Account",
                          "Pool",
                          "API key",
                          "Requests",
                          "Failures",
                          "Tokens",
                          "Cost",
                          "Mean latency",
                        ]}
                        rows={rollups.map((r) => [
                          r.day,
                          r.provider,
                          r.model,
                          accountName(r.account),
                          r.pool,
                          r.api_key,
                          r.requests,
                          r.failures,
                          r.tokens,
                          r.cost.toFixed(4),
                          r.requests
                            ? `${Math.round(r.latency_ms / r.requests)} ms`
                            : "Unknown",
                        ])}
                      />
                      <div className="cp-actions">
                        <Button
                          disabled={
                            !analyticsPage ||
                            analyticsPage.filter !== filter ||
                            analyticsPage.end !== analyticsEnd ||
                            analyticsPage.offset === 0
                          }
                          onClick={() =>
                            setAnalyticsPage({
                              filter,
                              end: analyticsEnd,
                              offset: Math.max(
                                0,
                                (analyticsPage?.offset ?? 0) - 100,
                              ),
                            })
                          }
                        >
                          Previous breakdown
                        </Button>
                        <Button
                          disabled={
                            !analyticsResult || analyticsResult.next_offset < 0
                          }
                          onClick={() =>
                            setAnalyticsPage({
                              filter,
                              end: analyticsEnd,
                              offset: analyticsResult?.next_offset ?? 0,
                            })
                          }
                        >
                          Next breakdown
                        </Button>
                        <span>
                          {analyticsResult?.groups ?? 0} matching groups ·
                          totals cover all pages
                        </span>
                      </div>
                    </Card>
                  </>
                )}
                {page === "Monitoring" && (
                  <>
                    <Card title="Request monitoring · refreshes every 5 seconds">
                      <MonitoringTabsBar
                        tabs={[
                          {
                            id: "live",
                            label: "Realtime feed",
                            icon: "realtime",
                          },
                          {
                            id: "history",
                            label: "Persistent history",
                            icon: "realtime",
                          },
                        ]}
                        activeTab={monitoringTab}
                        onChange={(tab) => {
                          setMonitoringTab(tab);
                          if (tab === "live")
                            setRequestPages({ filter, cursors: [] });
                        }}
                        ariaLabel="Request monitoring views"
                        idBase="control-plane-request-views"
                      />
                      <div className="cp-filters">
                        {Object.entries(filter).map(([k, v]) => (
                          <label key={k}>
                            {k}
                            <input
                              value={v}
                              type={k === "since" ? "date" : "text"}
                              onChange={(e) =>
                                setFilter({ ...filter, [k]: e.target.value })
                              }
                            />
                          </label>
                        ))}
                      </div>
                      <Table
                        headers={[
                          "Time",
                          "Provider",
                          "Requested / actual model",
                          "Account",
                          "Pool",
                          "API key",
                          "Status",
                          "Retries / error",
                          "Routing",
                          "Tokens",
                          "TTFT / latency",
                        ]}
                        rows={logs.map((r) => [
                          date(r.at),
                          r.provider ||
                            (r.phase === "http_rejection"
                              ? "Local rejection"
                              : "Unknown"),
                          r.model
                            ? `${r.model} / ${r.actual_model}`
                            : "Not dispatched",
                          r.account ? accountName(r.account) : "Not selected",
                          r.pool || "Not selected",
                          r.api_key || "Unknown / legacy",
                          r.status,
                          `${r.retries ?? 0} / ${r.error_category || "None"}`,
                          r.routing ? (
                            <Button
                              size="sm"
                              onClick={() => evidenceHistory([r.routing])}
                            >
                              {r.routing.strategy} · {r.routing.reason}
                            </Button>
                          ) : r.phase === "http_rejection" ? (
                            r.route
                          ) : (
                            "Native / unknown"
                          ),
                          r.phase === "http_rejection"
                            ? "No usage"
                            : r.total_tokens,
                          `${r.phase === "http_rejection" ? "Not dispatched" : r.ttft_ms} / ${r.latency_ms} ms`,
                        ])}
                      />
                      <Button onClick={() => void showHistory("decisions")}>
                        Routing evidence
                      </Button>
                      <div className="cp-actions">
                        <Button
                          disabled={!requestCursor}
                          onClick={() =>
                            setRequestPages({
                              filter,
                              cursors: requestCursors.slice(0, -1),
                            })
                          }
                        >
                          Newer requests
                        </Button>
                        <Button
                          disabled={!requestCursor}
                          onClick={() =>
                            setRequestPages({ filter, cursors: [] })
                          }
                        >
                          Latest requests
                        </Button>
                        <Button
                          disabled={
                            monitoringTab === "live" ||
                            logs.length < 100 ||
                            loadedRequestPage?.filter !== filter ||
                            loadedRequestPage?.cursor !== requestCursor
                          }
                          onClick={() => {
                            const last = logs[logs.length - 1];
                            if (last)
                              setRequestPages({
                                filter,
                                cursors: [
                                  ...requestCursors,
                                  {
                                    before: Math.floor(
                                      new Date(last.at).getTime() / 1000,
                                    ),
                                    id: last.id,
                                  },
                                ],
                              });
                          }}
                        >
                          Older requests
                        </Button>
                        <span>
                          Page {requestCursors.length + 1} · up to 100 requests
                        </span>
                      </div>
                    </Card>
                    <Card title="Unresolved execution usage">
                      <p>
                        Latest 100 unresolved attempts, independent of request
                        filters. Final provider usage was not published before
                        release or restart. Tokens and cost remain unknown;
                        these rows do not count as settled usage. Recovery
                        evidence survives raw telemetry retention.
                      </p>
                      <Table
                        headers={[
                          "Time",
                          "Execution",
                          "Provider / model",
                          "Account",
                          "API key",
                          "State",
                        ]}
                        rows={unresolved.map((row) => [
                          date(row.at),
                          row.id,
                          `${row.provider} / ${row.model || "Unknown"}`,
                          accountName(row.account),
                          row.api_key || "Legacy / unknown",
                          row.state,
                        ])}
                      />
                    </Card>
                  </>
                )}
                {page === "Accounts" && (
                  <>
                    <Card
                      title="Provider accounts"
                      extra={
                        <div>
                          {["codex", "claude"].map((p) => (
                            <Button
                              key={p}
                              disabled={busy || !!oauth}
                              onClick={() => void startOAuth(p)}
                            >
                              Add / re-auth {p}
                            </Button>
                          ))}
                        </div>
                      }
                    >
                      <AccountWorkspace
                        state={state}
                        provider={accountProvider}
                        onProviderChange={setAccountProvider}
                        resolvedTheme={resolvedTheme}
                      >
                        {(accountRows) => (
                          <Table
                            headers={[
                              "Account",
                              "Provider / workspace / plan",
                              "Health",
                              "Quotas",
                              "Assigned pools",
                              "Warm-up",
                              "Actions",
                            ]}
                            rows={accountRows.map((a) => {
                              const schedule =
                                state.schedules.find(
                                  (s) => s.account === a.id && s.effective,
                                ) ??
                                state.schedules.find(
                                  (s) =>
                                    s.account === a.id &&
                                    s.active !== false &&
                                    !s.suspended,
                                );
                              return [
                                <div>
                                  <Button
                                    size="sm"
                                    variant="ghost"
                                    onClick={() => setDetailAccountID(a.id)}
                                  >
                                    {a.label}
                                  </Button>
                                  <br />
                                  <CopyableText
                                    value={a.id}
                                    ariaLabel={`Copy logical account ID ${a.id}`}
                                  />
                                </div>,
                                `${a.provider} / ${a.workspace || "Unknown"} / ${a.plan || "Unknown"}`,
                                <AccountHealthBadge
                                  severity={
                                    a.paused || a.health === "disabled"
                                      ? "disabled"
                                      : a.health === "active"
                                        ? "ok"
                                        : a.health === "error"
                                          ? "critical"
                                          : a.health === "limited"
                                            ? "warning"
                                            : "unknown"
                                  }
                                  label={
                                    a.replaced_by
                                      ? "Replaced"
                                      : a.paused
                                        ? "Paused"
                                        : a.health
                                  }
                                  size="sm"
                                />,
                                <div>
                                  {state.quotas
                                    .filter((q) => q.account === a.id)
                                    .map((q) => (
                                      <div key={q.key}>
                                        <QuotaWindowCard
                                          window={quotaWindowView(
                                            q,
                                            Date.now(),
                                            state.settings.quota_fresh_seconds,
                                          )}
                                          variant="compact"
                                          locale="en"
                                        />
                                        <strong>{q.key}</strong>:{" "}
                                        {q.used_percent === undefined
                                          ? q.remaining === undefined
                                            ? "Unknown"
                                            : `${q.remaining} remaining`
                                          : `${q.used_percent.toFixed(1)}% used`}{" "}
                                        · reset {date(q.reset_at)} ·{" "}
                                        {Date.now() -
                                          new Date(q.observed_at).getTime() >
                                        state.settings.quota_fresh_seconds *
                                          1000
                                          ? "Stale"
                                          : "Fresh"}
                                      </div>
                                    ))}
                                  {!state.quotas.some(
                                    (q) => q.account === a.id,
                                  ) && "Unknown"}
                                </div>,
                                state.pools
                                  .filter((p) => p.accounts.includes(a.id))
                                  .map((p) => p.name)
                                  .join(", ") || "None",
                                <div>
                                  Next:{" "}
                                  {schedule
                                    ? date(schedule.next)
                                    : "Not scheduled"}
                                  <br />
                                  Last:{" "}
                                  {schedule
                                    ? date(schedule.last)
                                    : "Never"} · {schedule?.result || "None"}
                                </div>,
                                <div className="cp-actions">
                                  <Button
                                    disabled={busy || !!a.replaced_by}
                                    size="sm"
                                    onClick={() =>
                                      void act(`/accounts/${a.id}`, "PUT", {
                                        ...a,
                                        paused: !a.paused,
                                      })
                                    }
                                  >
                                    {a.paused ? "Resume" : "Pause"}
                                  </Button>
                                  <Button
                                    disabled={busy || !!a.replaced_by}
                                    size="sm"
                                    onClick={() =>
                                      void act(`/accounts/${a.id}`, "PUT", {
                                        ...a,
                                        warm_enabled: !a.warm_enabled,
                                      })
                                    }
                                  >
                                    Warm-up {a.warm_enabled ? "off" : "on"}
                                  </Button>
                                  <Button
                                    size="sm"
                                    disabled={
                                      busy || !schedule || !!a.replaced_by
                                    }
                                    onClick={() =>
                                      void act(`/accounts/${a.id}/warm`)
                                    }
                                  >
                                    Warm now
                                  </Button>
                                  <Button
                                    size="sm"
                                    onClick={() =>
                                      void showHistory("quota", a.id)
                                    }
                                  >
                                    Quota history
                                  </Button>
                                  <Button
                                    size="sm"
                                    disabled={
                                      busy ||
                                      !!a.replaced_by ||
                                      !["codex", "claude"].includes(a.provider)
                                    }
                                    onClick={() =>
                                      void act(`/accounts/${a.id}/quota`)
                                    }
                                  >
                                    Refresh quota
                                  </Button>
                                </div>,
                              ];
                            })}
                          />
                        )}
                      </AccountWorkspace>
                    </Card>
                  </>
                )}
                {page === "API Keys" && (
                  <>
                    <Card
                      title="Downstream API keys"
                      extra={
                        <Button
                          onClick={() =>
                            setKeyDraft({
                              id: "",
                              name: "",
                              prefix: "",
                              revoked: false,
                              expires_at: "0001-01-01T00:00:00Z",
                              models: [],
                              bindings: {},
                              request_limit: 0,
                              token_limit: 0,
                              cost_limit: 0,
                              requests: 0,
                              tokens: 0,
                              cost: 0,
                              last_used: "0001-01-01T00:00:00Z",
                            })
                          }
                        >
                          Create key
                        </Button>
                      }
                    >
                      <p>
                        Request ceilings are transactional admissions.
                        Token/cost ceilings stop later admission after usage
                        settles; already-running requests can overshoot. Cost is
                        an estimate, not an invoice.
                      </p>
                      <Table
                        headers={[
                          "Name / prefix",
                          "Providers",
                          "Requests / tokens / cost",
                          "Last used",
                          "Status",
                          "Actions",
                        ]}
                        rows={state.keys.map((k) => [
                          <>
                            {k.name}
                            <br />
                            <code>{k.prefix}…</code>
                          </>,
                          Object.keys(k.bindings).join(", "),
                          `${k.requests} / ${k.tokens} / ${k.cost.toFixed(4)}`,
                          date(k.last_used),
                          k.revoked ? "Revoked" : "Active",
                          <>
                            <Button
                              size="sm"
                              onClick={() => setKeyDraft(structuredClone(k))}
                            >
                              Edit
                            </Button>
                            <Button
                              variant="danger"
                              size="sm"
                              disabled={busy || k.revoked}
                              onClick={() =>
                                void act(`/keys/${k.id}`, "DELETE")
                              }
                            >
                              Revoke
                            </Button>
                          </>,
                        ])}
                      />
                    </Card>
                    {newSecret && (
                      <Card title="Copy this key now. It cannot be retrieved again.">
                        <code className="cp-secret">{newSecret}</code>
                        <Button onClick={() => setNewSecret("")}>
                          Dismiss
                        </Button>
                      </Card>
                    )}
                    {keyDraft && (
                      <Card
                        title={keyDraft.id ? "Edit API key" : "Create API key"}
                      >
                        <form
                          onSubmit={async (e) => {
                            e.preventDefault();
                            const r = await act(
                              keyDraft.id ? `/keys/${keyDraft.id}` : "/keys",
                              keyDraft.id ? "PUT" : "POST",
                              keyDraft,
                            );
                            if (r) {
                              if (
                                typeof r === "object" &&
                                "secret" in r &&
                                typeof r.secret === "string"
                              )
                                setNewSecret(r.secret);
                              setKeyDraft(undefined);
                            }
                          }}
                        >
                          <div className="cp-form">
                            <label>
                              Name
                              <input
                                required
                                value={keyDraft.name}
                                onChange={(e) =>
                                  setKeyDraft({
                                    ...keyDraft,
                                    name: e.target.value,
                                  })
                                }
                              />
                            </label>
                            <label>
                              Expires
                              <input
                                type="datetime-local"
                                value={
                                  keyDraft.expires_at.startsWith("0001")
                                    ? ""
                                    : keyDraft.expires_at.slice(0, 16)
                                }
                                onChange={(e) =>
                                  setKeyDraft({
                                    ...keyDraft,
                                    expires_at: e.target.value
                                      ? new Date(e.target.value).toISOString()
                                      : "0001-01-01T00:00:00Z",
                                  })
                                }
                              />
                            </label>
                            <label>
                              Allowed models (comma-separated)
                              <input
                                value={keyDraft.models.join(",")}
                                onChange={(e) =>
                                  setKeyDraft({
                                    ...keyDraft,
                                    models: e.target.value
                                      .split(",")
                                      .map((v) => v.trim())
                                      .filter(Boolean),
                                  })
                                }
                              />
                            </label>
                            {(
                              [
                                "request_limit",
                                "token_limit",
                                "cost_limit",
                              ] as const
                            ).map((field) => (
                              <label key={field}>
                                {field}
                                <input
                                  type="number"
                                  min="0"
                                  value={keyDraft[field]}
                                  onChange={(e) =>
                                    setKeyDraft({
                                      ...keyDraft,
                                      [field]: Number(e.target.value),
                                    })
                                  }
                                />
                              </label>
                            ))}
                          </div>
                          {providers.map((provider) => (
                            <fieldset key={provider}>
                              <legend>{provider} · account/pool scope</legend>
                              {state.pools
                                .filter((p) => p.provider === provider)
                                .map((p) => (
                                  <label className="cp-check" key={p.id}>
                                    <input
                                      type="checkbox"
                                      checked={
                                        keyDraft.bindings[
                                          provider
                                        ]?.pools.includes(p.id) || false
                                      }
                                      onChange={(e) => {
                                        const b = keyDraft.bindings[
                                          provider
                                        ] || { accounts: [], pools: [] };
                                        const updated = {
                                          ...b,
                                          pools: e.target.checked
                                            ? [...b.pools, p.id]
                                            : b.pools.filter(
                                                (id) => id !== p.id,
                                              ),
                                        };
                                        const bindings = {
                                          ...keyDraft.bindings,
                                        };
                                        if (
                                          updated.accounts.length +
                                          updated.pools.length
                                        )
                                          bindings[provider] = updated;
                                        else delete bindings[provider];
                                        setKeyDraft({ ...keyDraft, bindings });
                                      }}
                                    />
                                    Pool: {p.name}
                                  </label>
                                ))}
                              {state.accounts
                                .filter(
                                  (a) =>
                                    a.provider === provider && !a.replaced_by,
                                )
                                .map((a) => (
                                  <label className="cp-check" key={a.id}>
                                    <input
                                      type="checkbox"
                                      checked={
                                        keyDraft.bindings[
                                          provider
                                        ]?.accounts.includes(a.id) || false
                                      }
                                      onChange={(e) => {
                                        const b = keyDraft.bindings[
                                          provider
                                        ] || { accounts: [], pools: [] };
                                        const updated = {
                                          ...b,
                                          accounts: e.target.checked
                                            ? [...b.accounts, a.id]
                                            : b.accounts.filter(
                                                (id) => id !== a.id,
                                              ),
                                        };
                                        const bindings = {
                                          ...keyDraft.bindings,
                                        };
                                        if (
                                          updated.accounts.length +
                                          updated.pools.length
                                        )
                                          bindings[provider] = updated;
                                        else delete bindings[provider];
                                        setKeyDraft({ ...keyDraft, bindings });
                                      }}
                                    />
                                    {a.label}
                                  </label>
                                ))}
                            </fieldset>
                          ))}
                          <Button type="submit" disabled={busy}>
                            Save
                          </Button>
                          <Button
                            variant="ghost"
                            onClick={() => setKeyDraft(undefined)}
                          >
                            Cancel
                          </Button>
                        </form>
                      </Card>
                    )}
                  </>
                )}
                {page === "Pools" && (
                  <>
                    <Card
                      title="Provider-specific pools"
                      extra={
                        <Button
                          onClick={() =>
                            setPoolDraft({
                              id: "",
                              name: "",
                              provider: providers[0] || "codex",
                              accounts: [],
                              strategy: "capacity_weighted",
                              models: [],
                              reserve_percent: 0,
                              sticky: true,
                              warm: { ...emptyWarm },
                            })
                          }
                        >
                          Create pool
                        </Button>
                      }
                    >
                      <Table
                        headers={[
                          "Pool",
                          "Provider",
                          "Strategy",
                          "Accounts",
                          "Health / quota",
                          "Warm-up",
                          "Actions",
                        ]}
                        rows={state.pools.map((p) => [
                          p.name,
                          p.provider,
                          p.strategy,
                          p.accounts.map(accountName).join(", "),
                          <div>
                            {
                              p.accounts.filter((id) => {
                                const a = state.accounts.find(
                                  (a) => a.id === id,
                                );
                                return a && !a.paused && a.health === "active";
                              }).length
                            }{" "}
                            active / {p.accounts.length} members
                            <br />
                            {quotaSummary(p.accounts)}
                            {state.settings.providers?.[p.provider]
                              ?.disabled && <p>Provider disabled</p>}
                            {p.accounts
                              .filter(
                                (id) =>
                                  Date.parse(
                                    state.accounts.find((a) => a.id === id)
                                      ?.pool_health?.[p.id]?.cooldown_until ??
                                      "",
                                  ) > Date.now(),
                              )
                              .map((id) => (
                                <p key={id}>
                                  {accountName(id)} · cooling until{" "}
                                  {
                                    state.accounts.find((a) => a.id === id)
                                      ?.pool_health?.[p.id]?.cooldown_until
                                  }
                                </p>
                              ))}
                          </div>,
                          p.warm.enabled
                            ? state.schedules.some(
                                (s) => s.pool === p.id && s.window_seconds,
                              )
                              ? `${state.schedules.find((s) => s.pool === p.id)?.spacing_seconds} second spacing · ${state.schedules.find((s) => s.pool === p.id)?.window_seconds} second window`
                              : "Waiting for eligible accounts and fresh primary-window duration evidence"
                            : "Off",
                          <Button
                            size="sm"
                            onClick={() => setPoolDraft(structuredClone(p))}
                          >
                            Edit
                          </Button>,
                        ])}
                      />
                    </Card>
                    {poolDraft && (
                      <Card title="Pool configuration">
                        <form
                          onSubmit={async (e) => {
                            e.preventDefault();
                            const r = await act(
                              poolDraft.id
                                ? `/pools/${poolDraft.id}`
                                : "/pools",
                              poolDraft.id ? "PUT" : "POST",
                              poolDraft,
                            );
                            if (r) setPoolDraft(undefined);
                          }}
                        >
                          <div className="cp-form">
                            <label>
                              Name
                              <input
                                required
                                value={poolDraft.name}
                                onChange={(e) =>
                                  setPoolDraft({
                                    ...poolDraft,
                                    name: e.target.value,
                                  })
                                }
                              />
                            </label>
                            <label>
                              Provider
                              <select
                                disabled={!!poolDraft.id}
                                value={poolDraft.provider}
                                onChange={(e) =>
                                  setPoolDraft({
                                    ...poolDraft,
                                    provider: e.target.value,
                                    accounts: [],
                                  })
                                }
                              >
                                {providers.map((p) => (
                                  <option key={p}>{p}</option>
                                ))}
                              </select>
                            </label>
                            <label>
                              Strategy
                              <select
                                value={poolDraft.strategy}
                                onChange={(e) =>
                                  setPoolDraft({
                                    ...poolDraft,
                                    strategy: e.target.value,
                                  })
                                }
                              >
                                {state.strategies.map((s) => (
                                  <option key={s}>{s}</option>
                                ))}
                              </select>
                            </label>
                            <label>
                              Reserve %
                              <input
                                type="number"
                                min="0"
                                max="99"
                                value={poolDraft.reserve_percent}
                                onChange={(e) =>
                                  setPoolDraft({
                                    ...poolDraft,
                                    reserve_percent: Number(e.target.value),
                                  })
                                }
                              />
                            </label>
                            <label>
                              Enabled models
                              <input
                                value={poolDraft.models.join(",")}
                                onChange={(e) =>
                                  setPoolDraft({
                                    ...poolDraft,
                                    models: e.target.value
                                      .split(",")
                                      .map((s) => s.trim())
                                      .filter(Boolean),
                                  })
                                }
                              />
                            </label>
                          </div>
                          <fieldset>
                            <legend>Members</legend>
                            {state.accounts
                              .filter(
                                (a) =>
                                  a.provider === poolDraft.provider &&
                                  !a.replaced_by,
                              )
                              .map((a) => (
                                <label className="cp-check" key={a.id}>
                                  <input
                                    type="checkbox"
                                    checked={poolDraft.accounts.includes(a.id)}
                                    onChange={(e) =>
                                      setPoolDraft({
                                        ...poolDraft,
                                        accounts: e.target.checked
                                          ? [...poolDraft.accounts, a.id]
                                          : poolDraft.accounts.filter(
                                              (id) => id !== a.id,
                                            ),
                                      })
                                    }
                                  />
                                  {a.label}
                                </label>
                              ))}
                          </fieldset>
                          <label className="cp-check">
                            <input
                              type="checkbox"
                              checked={poolDraft.sticky}
                              onChange={(e) =>
                                setPoolDraft({
                                  ...poolDraft,
                                  sticky: e.target.checked,
                                })
                              }
                            />
                            Soft sticky
                          </label>
                          <fieldset>
                            <legend>Pool health and cooldowns</legend>
                            <p>
                              max_in_flight is selection pressure, not an atomic
                              concurrency cap. Concurrent admissions can exceed
                              it.
                            </p>
                            <label className="cp-check">
                              <input
                                type="checkbox"
                                checked={
                                  poolDraft.health?.require_healthy ?? false
                                }
                                onChange={(e) =>
                                  setPoolDraft({
                                    ...poolDraft,
                                    health: {
                                      ...{
                                        require_healthy: false,
                                        max_in_flight: 0,
                                        rate_limit_cooldown_seconds: 0,
                                        error_cooldown_seconds: 0,
                                        consecutive_failure_limit: 0,
                                      },
                                      ...poolDraft.health,
                                      require_healthy: e.target.checked,
                                    },
                                  })
                                }
                              />
                              Require healthy account
                            </label>
                            {(
                              [
                                "max_in_flight",
                                "rate_limit_cooldown_seconds",
                                "error_cooldown_seconds",
                                "consecutive_failure_limit",
                              ] as const
                            ).map((field) => (
                              <label key={field}>
                                {field}
                                <input
                                  type="number"
                                  min="0"
                                  value={poolDraft.health?.[field] ?? 0}
                                  onChange={(e) =>
                                    setPoolDraft({
                                      ...poolDraft,
                                      health: {
                                        require_healthy: false,
                                        max_in_flight: 0,
                                        rate_limit_cooldown_seconds: 0,
                                        error_cooldown_seconds: 0,
                                        consecutive_failure_limit: 0,
                                        ...poolDraft.health,
                                        [field]: Number(e.target.value),
                                      },
                                    })
                                  }
                                />
                              </label>
                            ))}
                          </fieldset>
                          <label className="cp-check">
                            <input
                              type="checkbox"
                              checked={poolDraft.warm.enabled}
                              onChange={(e) =>
                                setPoolDraft({
                                  ...poolDraft,
                                  warm: {
                                    ...poolDraft.warm,
                                    enabled: e.target.checked,
                                  },
                                })
                              }
                            />
                            Enable warm-up (real upstream requests)
                          </label>
                          <p>
                            Set window_seconds to 0 to derive the window from
                            fresh native quota evidence. Automatic planning
                            waits when eligible members have unknown or
                            incompatible durations. spacing_seconds = 0 divides
                            that duration by eligible account count. Existing
                            active windows and real traffic cannot be shifted by
                            the proxy.
                          </p>
                          <label>
                            Confirmed reset dispatch
                            <select
                              value={poolDraft.warm.reset_mode ?? "phased"}
                              onChange={(e) =>
                                setPoolDraft({
                                  ...poolDraft,
                                  warm: {
                                    ...poolDraft.warm,
                                    reset_mode:
                                      e.target.value === "immediate"
                                        ? "immediate"
                                        : "phased",
                                  },
                                })
                              }
                            >
                              <option value="phased">
                                Next assigned phase (default)
                              </option>
                              <option value="immediate">
                                As soon as eligible after confirmation
                              </option>
                            </select>
                          </label>
                          <p>
                            Both reset modes obey account cooldown, health,
                            scope, quota reserve and recent real traffic. Reset
                            and idle share one account claim; a planned phase is
                            not a native reset timestamp.
                          </p>
                          <div className="cp-form">
                            {(["model", "prompt"] as const).map((k) => (
                              <label key={k}>
                                Warm {k}
                                <input
                                  value={poolDraft.warm[k]}
                                  onChange={(e) =>
                                    setPoolDraft({
                                      ...poolDraft,
                                      warm: {
                                        ...poolDraft.warm,
                                        [k]: e.target.value,
                                      },
                                    })
                                  }
                                />
                              </label>
                            ))}
                            {(
                              [
                                "window_seconds",
                                "spacing_seconds",
                                "idle_seconds",
                                "cooldown_seconds",
                                "jitter_seconds",
                              ] as const
                            ).map((k) => (
                              <label key={k}>
                                {k}
                                <input
                                  type="number"
                                  min="0"
                                  value={poolDraft.warm[k]}
                                  onChange={(e) =>
                                    setPoolDraft({
                                      ...poolDraft,
                                      warm: {
                                        ...poolDraft.warm,
                                        [k]: Number(e.target.value),
                                      },
                                    })
                                  }
                                />
                              </label>
                            ))}
                          </div>
                          <Button type="submit" disabled={busy}>
                            Save pool
                          </Button>
                          <Button
                            variant="ghost"
                            onClick={() => setPoolDraft(undefined)}
                          >
                            Cancel
                          </Button>
                        </form>
                      </Card>
                    )}
                    {state.pools
                      .filter((p) => p.warm.enabled)
                      .map((p) => (
                        <Card key={p.id} title={`${p.name} · warm-up timeline`}>
                          <WarmerTimeline
                            schedules={state.schedules.filter(
                              (s) => s.pool === p.id,
                            )}
                            accounts={state.accounts}
                          />
                        </Card>
                      ))}
                    <Card title="Warm-up timeline">
                      <p>
                        Overlapping pools retain independent models, prompts and
                        phases. Earliest eligible slot wins; simultaneous slots
                        prefer manual, then confirmed reset, then pool ID. One
                        account has one upstream window and one global claim. A
                        probe applies the longest enabled-pool cooldown and
                        suppresses duplicate probes in other pools.
                      </p>
                      <Table
                        headers={[
                          "Account",
                          "Pool",
                          "Window / spacing / phase",
                          "Next",
                          "Reason",
                          "Last",
                          "Result",
                          "Effective arbitration",
                        ]}
                        rows={state.schedules.map((s) => [
                          accountName(s.account),
                          s.pool,
                          s.window_seconds
                            ? `${s.window_seconds}s / ${s.spacing_seconds}s / ${s.phase_seconds}s`
                            : "Unknown",
                          date(s.next),
                          s.reason,
                          date(s.last),
                          s.result || "Pending",
                          s.suspended ||
                            (s.active === false
                              ? "Inactive"
                              : s.effective
                                ? "Next eligible policy"
                                : "Retained competing policy"),
                        ])}
                      />
                      <Button onClick={() => void showHistory("warm")}>
                        Probe history
                      </Button>
                    </Card>
                  </>
                )}
                {(page === "Routing Settings" || page === "Settings") && (
                  <Card
                    title={
                      page === "Settings"
                        ? "Accounting and maintenance"
                        : "Routing policy"
                    }
                  >
                    <form
                      onSubmit={(e) => {
                        e.preventDefault();
                        void act("/settings", "PUT", state.settings);
                      }}
                    >
                      {page === "Settings" && (
                        <SegmentedTabs
                          items={[
                            { id: "all", label: "All settings" },
                            { id: "providers", label: "Providers" },
                            { id: "pricing", label: "Pricing" },
                            { id: "security", label: "Security" },
                            { id: "database", label: "Database" },
                          ]}
                          activeTab={settingsTab}
                          onChange={(tab) => {
                            setSettingsTab(tab);
                            if (tab === "security")
                              void act("/runtime", "GET").then((result) => {
                                if (isRuntimeInfo(result))
                                  setRuntimeInfo(result);
                              });
                          }}
                          ariaLabel="Settings sections"
                          idBase="control-plane-settings"
                        />
                      )}
                      <div
                        className="cp-form"
                        hidden={
                          page === "Settings" &&
                          settingsTab !== "all" &&
                          settingsTab !== "pricing"
                        }
                      >
                        <label
                          hidden={page === "Settings" && settingsTab !== "all"}
                        >
                          Default strategy
                          <select
                            value={state.settings.strategy}
                            onChange={(e) =>
                              editSettings({
                                ...state,
                                settings: {
                                  ...state.settings,
                                  strategy: e.target.value,
                                },
                              })
                            }
                          >
                            {state.strategies.map((s) => (
                              <option key={s}>{s}</option>
                            ))}
                          </select>
                        </label>
                        {(
                          [
                            "quota_fresh_seconds",
                            "sticky_seconds",
                            "sticky_min_remaining_percent",
                            "failure_penalty",
                            "in_flight_penalty",
                            "phase_preference",
                            "input_price_per_million",
                            "output_price_per_million",
                          ] as const
                        )
                          .filter(
                            (k) =>
                              page !== "Settings" ||
                              settingsTab === "all" ||
                              (settingsTab === "pricing" &&
                                k.includes("price")),
                          )
                          .map((k) => (
                            <label key={k}>
                              {k}
                              <input
                                type="number"
                                min="0"
                                step="any"
                                max={
                                  k === "phase_preference" ? 0.05 : undefined
                                }
                                value={state.settings[k] ?? 0}
                                onChange={(e) =>
                                  editSettings({
                                    ...state,
                                    settings: {
                                      ...state.settings,
                                      [k]: Number(e.target.value),
                                    },
                                  })
                                }
                              />
                            </label>
                          ))}
                      </div>
                      {page === "Routing Settings" && (
                        <p>
                          phase_preference (0–0.05, default off) is a bounded
                          scheduling-locality hint for capacity/usage/reset
                          scoring. Unknown quota or missing/stale schedules
                          receive no bonus. Native quota/reset evidence remains
                          separate and authoritative; affinity and eligibility
                          are never bypassed.
                        </p>
                      )}
                      {page === "Settings" && (
                        <>
                          <label
                            hidden={
                              settingsTab !== "all" &&
                              settingsTab !== "database"
                            }
                          >
                            Retention days (0 uses deployment default)
                            <input
                              type="number"
                              min="0"
                              value={state.settings.retention_days ?? 0}
                              onChange={(e) =>
                                editSettings({
                                  ...state,
                                  settings: {
                                    ...state.settings,
                                    retention_days: Number(e.target.value),
                                  },
                                })
                              }
                            />
                          </label>
                          <fieldset
                            hidden={
                              settingsTab !== "all" &&
                              settingsTab !== "providers"
                            }
                          >
                            <legend>Provider policy</legend>
                            {providers.map((provider) => {
                              const policy = state.settings.providers?.[
                                provider
                              ] ?? {
                                disabled: false,
                                quota_refresh_seconds: 0,
                              };
                              return (
                                <div key={provider}>
                                  <strong>{provider}</strong>
                                  <label>
                                    <input
                                      type="checkbox"
                                      checked={policy.disabled}
                                      onChange={(e) =>
                                        editSettings({
                                          ...state,
                                          settings: {
                                            ...state.settings,
                                            providers: {
                                              ...state.settings.providers,
                                              [provider]: {
                                                ...policy,
                                                disabled: e.target.checked,
                                              },
                                            },
                                          },
                                        })
                                      }
                                    />
                                    Disable provider dispatch and background
                                    probes
                                  </label>
                                  <label>
                                    Quota refresh seconds (0 = 300)
                                    <input
                                      type="number"
                                      min="0"
                                      max="86400"
                                      value={policy.quota_refresh_seconds}
                                      onChange={(e) =>
                                        editSettings({
                                          ...state,
                                          settings: {
                                            ...state.settings,
                                            providers: {
                                              ...state.settings.providers,
                                              [provider]: {
                                                ...policy,
                                                quota_refresh_seconds: Number(
                                                  e.target.value,
                                                ),
                                              },
                                            },
                                          },
                                        })
                                      }
                                    />
                                  </label>
                                </div>
                              );
                            })}
                          </fieldset>
                          <fieldset
                            hidden={
                              settingsTab !== "all" && settingsTab !== "pricing"
                            }
                          >
                            <legend>Model prices per million tokens</legend>
                            {(state.settings.pricing ?? []).map(
                              (price, index) => (
                                <div className="cp-form" key={index}>
                                  {(
                                    [
                                      "provider",
                                      "model",
                                      "input_per_million",
                                      "output_per_million",
                                    ] as const
                                  ).map((field) => (
                                    <label key={field}>
                                      {field}
                                      <input
                                        type={
                                          field.endsWith("per_million")
                                            ? "number"
                                            : "text"
                                        }
                                        min="0"
                                        step="any"
                                        value={price[field]}
                                        onChange={(e) =>
                                          editSettings({
                                            ...state,
                                            settings: {
                                              ...state.settings,
                                              pricing: (
                                                state.settings.pricing ?? []
                                              ).map((p, i) =>
                                                i === index
                                                  ? {
                                                      ...p,
                                                      [field]: field.endsWith(
                                                        "per_million",
                                                      )
                                                        ? Number(e.target.value)
                                                        : e.target.value,
                                                    }
                                                  : p,
                                              ),
                                            },
                                          })
                                        }
                                      />
                                    </label>
                                  ))}
                                  <Button
                                    type="button"
                                    onClick={() =>
                                      editSettings({
                                        ...state,
                                        settings: {
                                          ...state.settings,
                                          pricing:
                                            state.settings.pricing?.filter(
                                              (_, i) => i !== index,
                                            ),
                                        },
                                      })
                                    }
                                  >
                                    Remove price
                                  </Button>
                                </div>
                              ),
                            )}
                            <Button
                              type="button"
                              onClick={() =>
                                editSettings({
                                  ...state,
                                  settings: {
                                    ...state.settings,
                                    pricing: [
                                      ...(state.settings.pricing ?? []),
                                      {
                                        provider: "",
                                        model: "",
                                        input_per_million: 0,
                                        output_per_million: 0,
                                      },
                                    ],
                                  },
                                })
                              }
                            >
                              Add model price
                            </Button>
                          </fieldset>
                        </>
                      )}
                      <Button
                        type="submit"
                        disabled={busy}
                        hidden={
                          page === "Settings" && settingsTab === "security"
                        }
                      >
                        Save settings
                      </Button>
                      {settingsDirty && (
                        <>
                          <span>
                            Unsaved settings · automatic refresh preserves this
                            draft
                          </span>
                          <Button
                            type="button"
                            disabled={busy}
                            onClick={() => {
                              savedSettingsRevision.current =
                                settingsRevision.current;
                              setSettingsDirty(false);
                              refresh();
                            }}
                          >
                            Discard settings changes
                          </Button>
                        </>
                      )}
                    </form>
                    {page === "Settings" && (
                      <div
                        className="cp-actions"
                        hidden={
                          !["all", "database", "security"].includes(settingsTab)
                        }
                      >
                        <Button
                          hidden={settingsTab === "security"}
                          disabled={busy}
                          onClick={async () => {
                            const r = await act("/backup");
                            if (r) evidenceHistory([r]);
                          }}
                        >
                          Create SQLite backup
                        </Button>
                        <Button
                          hidden={settingsTab === "security"}
                          disabled={busy}
                          onClick={async () => {
                            if (
                              window.confirm(
                                "Create a backup and compact SQLite? Database access pauses during compaction.",
                              )
                            ) {
                              const result = await act("/compact");
                              if (result) evidenceHistory([result]);
                            }
                          }}
                        >
                          Backup and compact SQLite
                        </Button>
                        <Button
                          hidden={settingsTab === "security"}
                          disabled={busy}
                          onClick={() =>
                            void act("/retention", "POST", {
                              days: state.settings.retention_days || 30,
                            })
                          }
                        >
                          Apply configured raw telemetry retention
                        </Button>
                        <Button onClick={() => void showHistory("audit")}>
                          Audit history
                        </Button>
                        <Button
                          onClick={async () => {
                            const result = await act("/runtime", "GET");
                            if (result !== undefined) evidenceHistory([]);
                            if (isRuntimeInfo(result)) setRuntimeInfo(result);
                          }}
                        >
                          Runtime security and deployment
                        </Button>
                        <p>
                          Admin credentials are held only in memory. Token/cost
                          limits stop new requests after settled usage reaches
                          the limit; concurrent requests can overshoot. Prices
                          are explicit operator estimates, not provider
                          invoices.
                        </p>
                      </div>
                    )}
                  </Card>
                )}
                {page === "Settings" &&
                  (settingsTab === "all" || settingsTab === "security") &&
                  runtimeInfo && <RuntimeSecurity runtime={runtimeInfo} />}
                {(history.length > 0 || historyPage) && (
                  <Card title="Evidence / history">
                    {historyPage ? (
                      <HistoryTable kind={historyPage.kind} rows={history} />
                    ) : (
                      <pre className="cp-history">
                        {JSON.stringify(history, null, 2)}
                      </pre>
                    )}
                    {historyPage && (
                      <div className="cp-actions">
                        <Button
                          disabled={!historyPage.cursors.length}
                          onClick={() =>
                            void showHistory(
                              historyPage.kind,
                              historyPage.account,
                              historyPage.cursors.slice(0, -1),
                            )
                          }
                        >
                          Newer history
                        </Button>
                        <Button
                          disabled={!historyPage.next}
                          onClick={() =>
                            void showHistory(
                              historyPage.kind,
                              historyPage.account,
                              [...historyPage.cursors, historyPage.next],
                            )
                          }
                        >
                          Older history
                        </Button>
                      </div>
                    )}
                    <Button variant="ghost" onClick={() => evidenceHistory([])}>
                      Close
                    </Button>
                  </Card>
                )}
              </>
            )}
          </main>
        </div>
      </div>
      {state && detailAccount && (
        <AccountDetails
          key={detailAccount.id}
          account={detailAccount}
          state={state}
          busy={busy}
          mutate={act}
          onClose={closeAccountDetails}
          onStartOAuth={(provider, account) => void startOAuth(provider, account)}
        />
      )}
      {oauth && (
        <OAuthSession
          key={oauth.state}
          attempt={oauth}
          secret={secret}
          onClose={closeOAuth}
          onComplete={refresh}
        />
      )}
    </div>
  );
}
