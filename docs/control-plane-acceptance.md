# Prompt acceptance audit

Current acceptance: 2026-10-03, after the completion follow-up. Work remains
uncommitted on `main`; no push or production deploy occurred. The frontend adapts
the actual CPA-Manager-Plus source into one process with local account/pool and
SQLite APIs. See the [baseline/current requirement and page parity matrix](control-plane-followup-matrix.md)
and [provider/model/protocol matrix](control-plane-compatibility.md).

| Requirement | Implementation/evidence | Remaining boundary |
| --- | --- | --- |
| Existing runtime compatibility | Existing executors/translators retained; full Go suite, common scoped retry/plugin/stream policy and local WS reconnect regressions | Dedicated protocol-specific scoped fallback/429 coverage remains partial; live matrix mostly unverified |
| No Headroom | No service or integration added | None |
| Actual CPA-Manager-Plus source | Requested Overview/Monitoring/Analytics/Accounts/Keys/Pools/Routing/Settings page set wired locally; original shell/off-canvas mobile sidebar, styles, branding, summary cards, account metrics/filters/pagination, shared UI/charts, monitoring/provider tabs, quota cards, health badges, copyable identity, account drawer and segmented settings integrated in one bundle; hash navigation supports direct page links and Back/Forward | External-manager-only plugin/config/resource routes are not exposed; this is an adapted control plane, not byte-for-byte parity with every manager-product page |
| SQLite | WAL, FK, schema v1–v4, online pre-upgrade backups, private files, restart/maintenance tests | Single process only; downgrade requires old compatible binary plus pre-upgrade backup, losing subsequent mutations |
| Stable logical identity | Fresh provider subject/workspace guard before target-bound OAuth save; changed-reference regression preserves ID/pools/flags/scope/history/owner; audited fallback reassociation for unused paused imports | Identity-less changed IDs require operator assertion; no email heuristic. Live re-login/rotation blocked |
| OAuth management UX | Original modal/styles, target-bound drawer launch, automatic non-overlapping polling, cancel, remote same-state callback and sign-out isolation; one-time five-minute save/cancel guard; local/browser regressions | Live OAuth completion unverified; cancellation cannot roll back once saving begins |
| Account pools/key bindings | Provider-specific membership, hashed keys, models/limits, strict failover/plugin scope | Token/cost ceilings stop later admissions after settlement; already-running usage can exceed ceilings |
| Pool health/cooldown | Per-pool persisted failures/cooldown, healthy-only and in-flight selection thresholds, restart tests | In-flight threshold is not an atomic semaphore |
| Provider quota | Codex native windows, Claude native headers/OAuth usage, model isolation/history/refresh; authorized Codex Plus live WHAM refresh passed | Claude live access and actual reset transitions/window phasing unverified |
| Routing/affinity | Nine strategies; optional bounded phase locality with fresh selected-pool schedule/known quota; original-request response/conversation/file/resource/encrypted ownership and durable pins | Provider-native turn ownership remains unverified; arbitrary client turn IDs are not hard affinity; no production throughput certification |
| Warm-up | Independent pool/account schedules, deterministic phases, account-wide claim/longest enabled-pool cooldown, deterministic arbitration, captured policy and retained lifecycle evidence; advanced-native-reset confirmation with `phased` default or explicit `immediate`; overlap/restart/concurrency regressions; historical live manual warm/cooldown/opt-out | No observed live reset transition or multi-account upstream phasing. Immediate waits for eligibility and next worker pass; proxy cannot move fixed/active windows |
| Durable accounting | Synchronous published settlement/durable deduplication/failure gate; v4 independent stream attempt journals retain unresolved release/restart evidence and accept trustworthy late publication; historical live Codex totals reconciled | No exact recovery before publication or invoice reconciliation. Journal failure gates later admissions, not the starting attempt; observed attempt may precede an upstream call |
| Monitoring/history | Persistent requests with timestamp/ID pagination; quota/decision/warm/audit ID pages and structured tables; known inference routes also persist HTTP failures without published executor usage, including invalid keys and admission rejection | HTTP rejections are explicitly marked local; unavailable provider/model/account/usage is not guessed. They contribute to request/failure analytics, never billing/affinity counters |
| Analytics | Filtered complete totals, daily aggregation and paginated group breakdown; 100k-row/10k-group local measurement | Offset pages are not frozen exports; daily chart bounded to 3,660 dates; rollups contain total tokens and mean latency, not all token categories |
| Settings | Backend-enforced provider enable/refresh, retention, global/model prices, backups/compaction; original segmented tabs and structured runtime security panel | Secret/TLS changes require deployment config and restart, not fake browser controls |
| Security | Separate admin credential, one-time inference secrets, no OAuth secret serialization, audit and server-side scope | Internet deployment still requires correctly configured TLS/reverse proxy |
| Docker | Multi-stage integrated build, persistent volume, healthcheck, isolated smoke | No production deployment performed |
| Performance | 100k-row history and 64-attempt/16-concurrent local SSE load, exact settlements/unresolved disconnects and no leaked flights | History DB connection-pool waits measured, not SQLite lock waits; streaming wait counters and hot-path SQL-count profiler absent |

The required local management flows have implementation and regression coverage.
The authorized Codex account passed live quota, short inference and usage-counter
reconciliation checks; browser re-login and invoice comparison remain unverified.
Native provider identity wins over manual linkage if a later refresh exposes a
different principal. Replaced imports stay as read-only evidence; their existing
keys, history and affinity are never silently migrated.

## Verification still requiring authorization or external evidence

- Codex live quota and two short inference protocols now have authorized-account
  evidence. Browser sign-in/re-login, Claude live access, invoice comparison and
  actual upstream reset-confirmed/stagger phasing still lack evidence.
- The previously reproduced Claude shared-metadata race is now fixed by using
  synchronized boolean reads alongside synchronized profile writes. The expanded
  executor/control-plane/API/auth/usage/helpers race suite passes.
- The final browser run used native frontend fetch against the local fixture,
  without API interception. Earlier transport-adapted runs remain historical
  evidence only.
- Current full source-wide lint fails with **189 errors / 1 warning across 44
  files**. Forty-two diagnostic files match the reference byte-for-byte; two have
  surgical adaptations, with clean-reference reproduction. The [exact inventory](control-plane-lint.md)
  preserves locations, severity and duplicate messages. No rules were disabled.
- Final local results: full Go and six-package race suite, repeated HTTP/WS race,
  **263 frontend files / 4,215 tests**, typecheck, control-plane lint and production
  build passed; Docker v4 and eight-route desktop/mobile native-fetch smoke passed.
  See [commands, failures and timings](control-plane-verification.md).
- This completion follow-up made no live provider calls or credential mutations.
  Live Claude, targeted OAuth completion/rotation, real resets/multi-account
  phasing and billing remain blocked or unverified. Protocol-specific scope/turn
  evidence, finer rollup metrics, full unresolved export and performance
  instrumentation remain partial. Do not claim 1:1 prompt acceptance.
