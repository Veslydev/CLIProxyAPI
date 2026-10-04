# Follow-up baseline and acceptance matrix

Baseline inspected on 2026-10-03 on `main`, with pre-existing uncommitted work.
Historical runs in `control-plane-verification.md` are not verification of this
follow-up. Status vocabulary: **implemented** (source exists), **locally verified**
(executed this follow-up), **live verified** (explicit upstream evidence),
**partial**, **missing**, **blocked**. These are separate dimensions, not a score.

| prompt.md requirement | Baseline implementation | Local evidence / tests | Live boundary / outstanding work |
| --- | --- | --- | --- |
| Runtime preserved; no Headroom (§core) | Implemented: existing runtime/translator, candidate policy extension | `sdk/cliproxy/auth/candidate_policy.go`; `runtime_test.go` | Partial compatibility matrix; no new upstream tests yet |
| Actual integrated manager UI (§dashboard) | Partial: actual source/primitives/styles adapted, eight production routes | `web/NOTICE.md`, `web/src/controlplane/*`; parity checklist below | Browser checks must be rerun; not byte-for-byte parity |
| SQLite WAL/FK/busy timeout/backups/indexes/retention (§SQLite) | Implemented v1–v3 | `store.go`, `performance_test.go`, migration tests in `controlplane_test.go` | Single process only; upgrade format requires backup |
| Durable entities, no OAuth secrets in SQLite | Implemented except pre-publication execution journal | `store.go`, `telemetry.go`; `TestTelemetryIdempotentRollupRedactionAndRetention` | Missing unresolved execution recording; invoice reconciliation blocked |
| Stable identity through refresh/reference/re-login/workspace (§identity) | Partial: native fingerprint or credential reference; audited fallback reassociation | `accounts.go`, `reassociate.go`, identity/reassociation tests | Missing target-bound principal validation before OAuth save; live re-login blocked pending explicit authorization |
| Pools/provider membership/model/reserve/sticky/health (§pools) | Implemented | `accounts.go`, `health.go`, `health_test.go` | Overlap uses first binding policy for routing; warmer overlap missing |
| Create/edit/revoke/one-time scoped API keys (§keys) | Implemented | `http.go`, `accounts.go`; scope/atomic admission tests | Token/cost ceiling partial by contract: running usage can overshoot |
| Scope through retries/429/failover/sticky/model/plugin/WS | Partial fixture coverage | `runtime_test.go`, `routing_security_test.go`, `server_controlplane_test.go` | More protocol-specific regressions needed; not upstream certification |
| Generic quota history/native evidence (§quota) | Implemented | `quota.go`, `quota_refresh.go`, adapter/isolation tests | Historical live Codex retrieval only; Claude live blocked |
| Codex 5h/weekly/monthly/additional; Claude native unknown | Implemented parsing | `TestProviderQuotaAdapters`, `TestClaudeQuotaCanonicalKeysAndModelIsolation` | Actual reset, plans/models not live tested exhaustively |
| Nine strategies and explainable durable decisions (§routing) | Implemented except assigned phase signal | `routing.go`, `Decision`; quota/sticky tests | Missing phase-aware signal; performance not measured end-to-end |
| Soft sticky/hard original-request ownership (§affinity) | Partial: response/conversation/file/resource/encrypted objects | `routing.go:objects`, `ObserveResponse`; hard-owner regressions | Explicit turn identifiers need inspection/coverage; uploaded objects outside proxy deliberately fail closed |
| Reset-confirmed warm (§warm) | Partial: confirmation queues future phase | `queueReset`, `warmup_phase_test.go` | Immediate policy missing; real transition blocked by elapsed time/authorization |
| Pool-aware phased warm and lifecycle/dedup/manual/history | Partial: one schedule/account, first eligible pool wins | `warmup.go`, `runtime_test.go`, `warmup_phase_test.go` | Missing independent overlapping-pool schedules/global retained cooldown |
| Requests/usage/cost/timing/reasons/rollups (§observability) | Implemented where reported | `telemetry.go`, `http_observation.go`, history/analytics tests | Missing pre-publication unresolved execution; estimates are not invoice |
| Overview/Monitoring/Analytics/Accounts/Keys/Pools/Routing/Settings | Partial required-page parity | Actual reference files listed below; local component tests | More reuse/filter ergonomics/empty/error/browser coverage needed |
| Codex/Claude OAuth UX (§auth) | Partial: existing handlers reused, remote callbacks/cancel/polling | `server_controlplane.go`, `OAuthSession.test.tsx` | Same-state callback is not same-principal proof; bound re-auth guard missing |
| Admin isolation/redaction/audit/server-side scope (§security) | Implemented | `http.go`, auth/redaction/reassociation tests | TLS remains deployment responsibility; no credentials used in follow-up |
| YAML/auth import/default pools (§compatibility) | Implemented | `config_v8.go`, `SyncAccounts`, default pool tests | Reference replacement caveats; no destructive auth migration |
| Multi-stage Docker/static UI/volume/health/restore (§build) | Implemented | `Dockerfile`, `docker-compose.control-plane.yml`, operational docs | Build/runtime/restart must be rerun after final diff |
| Hot-path snapshots/no stream locks (§performance) | Implemented snapshots; synchronous durable decisions/settlements | `store.go`, `routing.go`, `performance_test.go` | Missing realistic large history/concurrent streaming measurements |
| Full suites/build/race/lint/browser (§testing) | Historical only at baseline | Prior verification record | Current runs recorded separately, including failures |

## Historical baseline UI parity inventory

Source of comparison: `reference/CPA-Manager-Plus/apps/web/src` (not screenshots
alone). External-manager plugin/resource/configuration pages are not acceptance
requirements unless directly needed by prompt.md.

| Page | Actual reference source | Reused / adapted | Partial / deliberate difference |
| --- | --- | --- | --- |
| Overview | `features/dashboard/DashboardPage.tsx`, `TrafficOverviewCard`, `UsageMetricsCard` | Original shell, branding, monitoring summary cards, ECharts | Different backend-bound summary layout; no separate collector status; provider/model breakdown via rollups rather than original widgets |
| Monitoring | `features/monitoring/MonitoringCenterPage.tsx`, `MonitoringFiltersPanel`, `MonitoringTabsBar`, `MonitoringShared` | Tabs, summary/chart/table styles, persistent latest/history feed | Exact-ID text filters instead of original option selectors; no trace bodies; no external JSONL import/export requirement |
| Analytics | `features/usage-analytics/UsageAnalyticsPage.tsx`, `UsageSummaryCards` | ECharts/card styling; SQL-filtered totals/date/groups | No original heatmap/drilldown/anomaly tabs; required dimensions present, latency mean only; finer token breakdown not in rollups |
| Accounts | `features/accounts/AccountsPage.tsx`, `components/accountDetail/*` | Metrics, provider tabs, input/select, quota cards, badges, copyable IDs, drawer, pagination | No native credential secret editors; re-auth target binding not yet implemented; table-only mode |
| API Keys | `components/config/ApiKeysCardEditor.tsx` | Button/card/form styling | Replaced plaintext YAML-key editor with hash-only scoped keys and one-time secret; required scope/limits are new local backend features |
| Pools | No first-class equivalent manager page | Original primitives/charts with local pool editor | Intentional new page; overlapping warm policies not yet independently visible |
| Routing | `components/config/VisualConfigEditor.tsx`, `utils/routingStrategy.ts` | Original primitives/styles; local strategies | New provider-agnostic controls; reset/phase signal controls missing |
| Settings | `components/config/*`, system and maintenance features | Segmented tabs/security/provider/pricing/backup controls | No fake TLS/secret browser editing; changes require environment/restart; no external collector configuration |

All eight routes need desktop/mobile native-fetch checks after the final bundle.
Loading, API errors, empty inventory/history and sign-out isolation are distinct
acceptance cases, not implied by a successful render.

## Final requirement matrix (2026-10-03)

The tables above preserve the pre-change findings. Their missing-code entries are
historical. The following table records the current source and executed checks.
Paths without a prefix refer to `internal/controlplane/`. **Live verified** below
means historical authorized Codex evidence from the verification record; this
completion follow-up made no new provider requests.

| prompt.md scope | Current state and source/test evidence | Remaining boundary |
| --- | --- | --- |
| Core runtime; no Headroom, lines 14–29 | Implemented, locally verified: native executors/translators retained; `candidate_policy.go` integrates in `sdk/cliproxy/auth`; full Go suite passes | Provider/model/protocol acceptance remains partial; see [compatibility](control-plane-compatibility.md) |
| Actual manager integration/license, lines 31–53 | Implemented, locally verified: `web/NOTICE.md`, license, original shell/styles/primitives and eight same-origin routes; full frontend and browser smoke | Adaptations below; external-manager resource/plugin pages are outside required scope |
| SQLite durability/maintenance, lines 55–92 | Implemented, locally verified: `store.go`, `executions.go`, `telemetry.go`; v4 migration/backup/restart, WAL/FK/private files and Docker smoke | One proxy process; unresolved attempts lack exact usage until trustworthy publication |
| Stable identity, lines 94–105 | Implemented, locally verified: `accounts.go`, `reauth_guard.go`, `reassociate.go`, `reauth_guard_test.go`; principal/workspace guard precedes save | Identity-less continuity remains an audited operator assertion; live re-login/rotation blocked |
| Pools, lines 107–129 | Implemented, locally verified: `accounts.go`, `health.go`, `health_test.go`; independent warm schedules in `warmup.go` | Request routing uses first matching pool in binding order; overlapping warm policies use explicit arbitration |
| Strong scoped keys, lines 131–176 | Implemented, locally verified: create/edit/revoke, one-time secret, expiration/model/provider/scope/limits; `controlplane_test.go`, `runtime_test.go`, `server_controlplane_load_test.go` | Token/cost ceilings apply after settlement; in-flight work can overshoot |
| Scope across failure paths, lines 164–174 | Implemented common candidate filtering; locally verified retry/429/plugin, pool updates, sticky/hard owner, HTTP and WS reconnect cases | Partial dedicated protocol-specific fallback/429 coverage; common policy tests do not certify every provider/transport combination |
| Native quota, lines 178–215 | Implemented, locally verified: `quota.go`, `quota_refresh.go`, provider adapters and model-isolation tests; historical live Codex WHAM evidence | Claude live blocked; no live exhaustive monthly/additional/plan/model coverage |
| Nine strategies/evidence, lines 217–254 | Implemented, locally verified: `routing.go`, `routing_phase_test.go`; fresh selected-pool phase preference bounded 0–0.05, defaults off | Phase is scheduling locality, not upstream capacity; no production throughput certification |
| Sticky/original hard ownership, lines 256–282 | Implemented, locally verified response/conversation/file/resource/encrypted ownership from original request; `routing_security_test.go` | Partial provider-native turn ownership evidence; arbitrary client `turn_id` is not hard affinity; unknown external objects fail closed |
| Reset-confirmed warm, lines 284–301 | Implemented, locally verified advanced native reset evidence, `phased` default and explicit `immediate`; `warmup_phase_test.go`, `warmup_overlap_test.go` | Real reset transition blocked by elapsed time/authorization; immediate dispatch waits for eligibility and next 30-second worker pass |
| Stagger/overlap/lifecycle, lines 303–348 | Implemented, locally verified independent pool/account rows, global claim/cooldown, captured policy, retained inactive/suspended history, restart/interrupted recovery | Historical live single-account manual warm only; several authorized accounts and elapsed time needed for upstream rolling-capacity evidence |
| Observability/rollups, lines 350–378 | Implemented where runtime reports data, locally verified: `telemetry.go`, `http_observation.go`, `executions.go`; durable exactly-once settlement and latest-100 unresolved UI/API | Partial analytics token detail: rollups contain total tokens, not every token category; estimated cost is not billing; older unresolved rows need a future paginated recovery export |
| Eight dashboard pages, lines 380–449 | Implemented, locally verified: `web/src/controlplane/*`; 11 files/46 focused tests and desktop/mobile native-fetch smoke | Source-backed parity checklist below records deliberate visual/interaction differences |
| Codex/Claude auth UX, lines 451–464 | Implemented, locally verified launch/poll/callback/cancel, target retained by drawer, one-time expiring principal validation | Live token exchange/re-login/rotation blocked; cancellation cannot roll back saving already started |
| Security, lines 466–482 | Implemented, locally verified separate admin, secret redaction, server scope, audit, sign-out isolation | Deployment operator supplies HTTPS/reverse proxy; no production security certification |
| YAML/auth/backward compatibility, lines 484–498 | Implemented, locally verified config migration, non-destructive auth discovery/default pools/native selector | v4 downgrade needs pre-upgrade backup and loses later mutations |
| Build/Docker, lines 500–513 | Implemented, locally verified single integrated image, health, v4/WAL/0600/integrity/restart/backup/compaction smoke | No production deploy; retained isolated backup evidence |
| Regression/testing, lines 515–563 | Locally verified full Go, six-package race, HTTP/WS repeated race, 263 frontend files/4,215 tests, typecheck, control-plane lint/build, Docker/browser | Full frontend lint fails: 189 errors/1 warning; [exact inventory](control-plane-lint.md) |
| Performance/snapshots, lines 565–580 | Implemented, locally verified snapshots, indexes, rollups, bounded HTTP stream/history measurements; `performance_test.go`, API load fixture | Partial instrumentation: DB connection-pool waits measured for history only; no SQLite busy/lock or streaming wait histogram; no hot-path SQL-count profiler |
| Follow-up operational limits (§6) | Implemented honest ceilings/selection pressure and unresolved journal; no fabricated generic reservations or usage recovery | Invoice reconciliation blocked; no trustworthy generic upper token/cost bound or verified per-attempt billing recovery API |
| Documentation/acceptance, lines 582–629 | Implemented: architecture, operations, baseline/current matrix, parity, compatibility, exact lint inventory and verification record | Overall acceptance remains partial for the explicit boundaries above; local passes do not mean 1:1 live completion |

## Final required-page parity checklist

Comparison uses the actual reference tree at
`/root/cpa/reference/CPA-Manager-Plus/apps/web/src`, revision in `web/NOTICE.md`.
All eight routes passed desktop and 390px mobile native-fetch checks against the
local API fixture, including no horizontal document overflow. The reference
component suite and control-plane tests cover error/loading/empty states; the
browser smoke did not inject every API error or loading state.

| Page | Source reuse and required behavior checked | Deliberate adaptation / partial parity |
| --- | --- | --- |
| Overview | `features/dashboard/DashboardPage.tsx` shell/styles; original monitoring summary cards, chart wrapper; request/failure/token/cost/latency and quota/account summaries | Local rollup/provider/model layout; no separate collector status |
| Monitoring | `MonitoringTabsBar`, `MonitoringShared`, original summary/table/chart styles; latest/history, time/provider/model/account/pool/key/status filters, cursor navigation, routing evidence, unresolved rows | Exact-ID inputs instead of manager option selectors; redacted evidence instead of raw traces; no JSONL import/export |
| Usage Analytics | `UsageSummaryCards`/ECharts styling; date and provider/model/account/pool/key filters, complete totals and paginated breakdown | No original heatmap/anomaly/drilldown tabs; mean latency and total-token rollups only |
| Accounts | `AccountMetricsGrid`, `AccountProviderTabs`, `Input`, `Select`, pagination, badges, quota cards, copyable IDs, original drawer; filters/sorts, pools, next/last/competing warm, pause/resume, quota inspect, targeted re-auth launch/cancel | Table view; no raw credential secret editor; identity-less re-auth remains explicit operator confirmation |
| API Keys | `ApiKeysCardEditor` and original primitives/styles; create/edit/revoke, Codex/Claude provider-specific account/pool selectors, models/limits/usage, one-time secret | Hash-only local keys replace plaintext YAML key editor; ceiling warning reflects backend behavior |
| Pools | Original forms/charts/primitives; provider membership/strategy/model/reserve/health, independent warm policies, effective/competing schedules, reset mode and timeline | New first-class page; reference manager has no equivalent pool entity |
| Routing Settings | `VisualConfigEditor`/`utils/routingStrategy.ts` reference styles/primitives; native/quota strategy, sticky, quota/health/pressure and bounded phase preference save | Provider-agnostic controls; scheduling phase and native reset remain distinct evidence |
| Settings | Original segmented tabs/config/maintenance primitives; security inspection, provider settings, retention, pricing, backup/compaction and unsaved-draft preservation | TLS/admin secrets remain deployment operations; no external collector/plugin configuration |

Browser smoke also checked direct links/Back/Forward, off-canvas closure, drawer
closure, sign-out and invalid admin isolation. Provider login links stayed closed;
OAuth completion is outside this evidence.
