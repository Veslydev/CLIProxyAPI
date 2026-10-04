# Verification record

Verified on Linux on 2026-10-02 and 2026-10-03. Earlier runs used local fixtures.
The authorized Codex-account follow-up below used one user-supplied credential
in an isolated localhost instance. No production service was changed. Changes
are on `main`; no commit, push or production deployment was performed.

## Completion follow-up: current verification (2026-10-03)

This section supersedes the local counts/schema/remaining-code claims in the
historical sections below. Those sections preserve earlier failures and authorized
live evidence. Current source: schema v4, target-bound re-auth, overlapping warm
schedules, bounded phase preference and independent unresolved attempt journals.
No new live provider request, credential access/rotation or production deploy
occurred during this completion follow-up. All new runtime/browser/load checks
used synthetic accounts and isolated state. Changes remain uncommitted on `main`.

### Commands and actual outcomes

Backend commands ran from the repository root; frontend commands from `web`.
This host needs `/usr/local/go/bin/go` and `/usr/local/go/bin/gofmt`; bare `go`
is absent from PATH. Passing cached packages below are Go test-cache results,
not fresh uncached executions.

| Command | Observed result / evidence under `/tmp/opencode/` |
| --- | --- |
| `/usr/local/go/bin/go test ./...` | PASS after fixture fixes; `cpa-followup-go-full-final.log` |
| `/usr/local/go/bin/go test -race ./internal/controlplane ./internal/api ./sdk/cliproxy/auth ./sdk/cliproxy/usage ./internal/runtime/executor ./internal/runtime/executor/helps` | PASS; API ran in 9.624s, other packages cached; `cpa-followup-race-final.log` |
| `CP_PERF_SMOKE=1 /usr/local/go/bin/go test -race ./internal/api -run 'TestControlPlane(ConcurrentHTTPStreamLoad\|WebsocketReconnect)' -count=10` | PASS, 17.674s; `cpa-followup-http-ws-race-final.log` |
| `/usr/local/go/bin/go test ./internal/runtime/executor -run '^TestCodexWebsockets_KeepalivePingDuringUpload_' -count=1000` | Each of three fixed fixtures passes 1,000 repeats; `cpa-followup-keepalive-fixed.log` |
| `CP_PERF_SMOKE=1 /usr/local/go/bin/go test ./internal/controlplane -run '^TestControlPlaneLargeHistoryMeasurement$' -count=1 -v` | PASS, 4.399s; `cpa-followup-history-perf-final.log` |
| `CP_PERF_SMOKE=1 /usr/local/go/bin/go test ./internal/api -run 'TestControlPlane(ConcurrentHTTPStreamLoad\|WebsocketReconnect)' -count=1 -v` | PASS; transport/measurement details below; `cpa-followup-http-ws-final.log` |
| `npm run test -- src/controlplane --maxWorkers=2` | PASS, 11 files / 46 tests; `cpa-followup-frontend-controls-final.log` |
| `npm run test -- src/features/accounts/AccountsPage.test.tsx --maxWorkers=2` | PASS, 357 tests; `cpa-followup-accounts-regression-final.log` |
| `npm run test -- --maxWorkers=2` | PASS, 263 files / 4,215 tests, 92.51s; `cpa-followup-frontend-full-final.log` |
| `npm run type-check` | PASS; `cpa-followup-typecheck-final.log` |
| `npx eslint src/controlplane` | PASS; empty `cpa-followup-controlplane-lint-final.log` |
| `npm run lint -- --format json --output-file /tmp/opencode/cpa-followup-lint-final.json` | FAIL, exit 1: 189 errors / 1 warning / 44 files; [exact residual inventory](control-plane-lint.md) |
| `npm run build` | PASS, TypeScript plus Vite; 3,186.14 kB single-file HTML (gzip 953.47 kB); `cpa-followup-build-final.log` |
| `/usr/local/go/bin/go build -o /tmp/opencode/cpa-followup-server-compile ./cmd/server` | PASS; final required confirmation also passed with output `cpa-followup-server-compile-final` |
| `/usr/local/go/bin/gofmt -l .` | PASS: no files, including final confirmation |
| `docker build -t cli-proxy-api-control-plane:followup .` | PASS; `cpa-followup-docker-build-final.log` |
| `python3 /tmp/opencode/cpa-followup-docker-smoke.py` | PASS: production bundle/health/admin isolation/v4/WAL/0600/integrity/phase bounds/settings restart/unresolved API/invalid re-auth target/backup/compaction; `cpa-followup-docker-smoke-final.log` |

Vite warns about `__dirname` compatibility with a future native config loader;
the current build passed without suppressing the warning. A combined verification
chain earlier timed out during build; the standalone production build above passed.

### Failures reproduced and fixed

- New warmer regressions first failed for overlap discarding two of four
  memberships and opt-out/re-enable erasing cooldown. The v4 schedule/guard split
  fixes both. Later selected-pool reserve coverage exposed a stale planner snapshot
  during mutations; planning now reads its snapshot inside the serialized mutation.
  Claim-time policy capture and stale-completion tests protect concurrent edits.
- The reset/phase frontend test initially inspected payload without submitting the
  settings form. Correct form submission now verifies saved `immediate` and
  `phase_preference`. The final focused and full suites passed.
- Stream-release initially used the account lease ID to finish an attempt whose
  execution ID was created later. An SDK completion observer now closes each
  independent stream attempt journal. Release without publication stays unresolved;
  the local stream and repeated-race HTTP load fixtures verify it.
- An initial WS test incorrectly expected cross-connection replay through the
  native SSE-backed WS handler. The corrected fixture requires explicit 409
  `previous_response_not_found` on unavailable continuation and verifies that the
  two fresh reconnected requests remain on the scoped account. It does not claim
  provider-side continuation replay.
- Full Go initially failed in keepalive fixtures. A clean `git archive HEAD`
  reproduction produced **18 failures / 1,000 repeats**, establishing baseline
  flakiness (`cpa-followup-keepalive-head.log`). The test server sent its terminal
  response before consuming the request and closed while the client wrote. Three
  fixture variants now wait for the request after the Pong-during-upload checks;
  protocol assertions remain. The fixed repeated run and final full suite passed.
- The new HTTP load fixture initially raced while constructing `UsageReporter`
  in a worker against the manager cloning the same auth. Reporter initialization
  now precedes worker startup. The final ten-repeat race run passed. This was a
  newly introduced fixture defect, not inherited runtime debt.
- Typecheck initially rejected three typed reference-test callbacks. Inferred
  callback parameter types fix the mismatch; the 357-test AccountsPage regression,
  typecheck and full frontend suite passed.
- Source-wide lint still fails. Forty-two diagnostic files match the actual
  reference; two contain narrow memo/declaration-order adaptations. Untouched
  copies of those two reproduce their original errors with the same lint tools
  (`cpa-followup-reference-lint.json`). Rules were not relaxed; residual locations
  include duplicate messages.

Initial failure logs remain alongside final logs (`cpa-followup-warmer-before.log`,
`cpa-followup-warmer-after.log`, `cpa-followup-frontend-focused-final.log`,
`cpa-followup-load.log`, `cpa-followup-load-rerun.log`, `cpa-followup-go-full.log`).

### Bounded performance measurements

Linux amd64, 12 KVM vCPUs, Intel Haswell. The history fixture creates 100,000 raw
rows and 10,000 rollup groups (DB/WAL/SHM total **109,777,912 bytes**), then performs
25 measurements per query. Final rerun:

| Query | p50 | p95 | max |
| --- | ---: | ---: | ---: |
| Unfiltered 100-row history page | 0.962ms | 1.978ms | 2.036ms |
| Account/model-filtered history page | 1.650ms | 3.599ms | 6.382ms |
| Analytics complete totals/daily/100 groups | 14.489ms | 22.052ms | 27.114ms |

The test verifies exact page/totals sizes and reports zero unexpected errors.
`database/sql` connection-pool wait count/duration deltas were 0 / 0s. These are
not SQLite busy/lock counters. Earlier lower timing measurements remain historical.

The HTTP SSE fixture uses the real local handler with a synthetic executor: four
waves of 16 concurrent streams, **64 attempts**, **16 deliberate disconnects**,
**48 exact settlements / 96 fixture tokens**, 16 unresolved journals, zero leaked
in-flight leases and no scope escape. End-to-end latency including barrier:
p50 **69.518ms**, p95 **94.093ms**, max **97.928ms**. No unexpected errors. Streaming
SQLite waits were not separately instrumented. Both fixtures clean their temporary
databases/listeners; neither is production or upstream throughput certification.
Snapshot source inspection confirms no lifetime stream lock and indexed/rollup
queries; a hot-path SQL-count profiler has not been run.

### Native-fetch browser and Docker scope

CloakBrowser used native frontend fetch, with no API interception or response
fabrication. All eight required routes passed desktop and 390px mobile checks,
including navigation/history, no document overflow, off-canvas/drawer closure,
account pause/resume, targeted re-auth launch/cancel, scoped key create/edit/revoke,
overlapping pools and saved reset/phase controls, backup, sign-out and invalid
admin isolation. The browser did not open provider login links. Component tests
cover loading/error/empty states; this smoke did not inject every such state.

The isolated fixture listened on `127.0.0.1:18347`; a test-only same-origin stdio
bridge in `cloak` listened on `127.0.0.1:18357`. No firewall changes. It used fake
`http-a`/`http-b` credentials and `gpt-test`. One attempt without usage appeared as
`unresolved_no_usage`. The fixture finished via native `POST /test/finish` (204);
the browser and new bridge closed cleanly. Selector/animation timing mistakes in
the first interactions were corrected, then actual closure checks passed.
Evidence: `cpa-followup-browser-fixture.log`, `cpa-followup-browser-bridge.log` and
mobile/overlap screenshots. Screenshots may reside on the browser server.

Docker used synthetic config only. The smoke container stopped and was removed;
rollback DB/backup evidence remains in `cpa-followup-docker-1g1yk70t`. The earlier
source snapshot remains `cpa-followup-source-baseline.tar.gz`; do not discard
rollback evidence merely because the final smoke passed.

### Acceptance limits

Final inspection confirmed `main`, preserved the uncommitted baseline and added
explicit `.gitignore` exceptions for the requirement/parity, compatibility and
lint records so Git does not silently omit them. The exact lint inventory matches
all 190 JSON messages, severity and duplicate locations; local documentation
links exist. Server compilation and empty `gofmt -l .` passed again after the code
changes; a further 1,000-repeat keepalive run passed in 3.762s.

The untracked-file scan found no build binaries, databases or private-key files
in the nonignored change set. High-confidence content markers matched three
byte-identical reference test files: two explicit test/example API-key strings
and one `abc` service-account PEM fixture. Values were suppressed; those existing
fixtures were preserved. This is a bounded pattern scan, not proof that arbitrary
confidential text is absent. No credential directories/files were read. Both new
fixture ports (18347/18357) have no listener and the smoke container is absent.

`git diff --check` passed for tracked changes. An additional `/dev/null` no-index
whitespace check covered all 938 untracked files: no follow-up whitespace defects,
but 25 byte-identical reference files retain trailing whitespace or extra EOF
blank lines. They were not reformatted as unrelated cleanup. The first audit
script treated no-index's normal exit 1 (files differ) as an error; the corrected
check separates that status from actual whitespace diagnostics. Both own compile
binaries were removed from `/tmp/opencode`; source/DB/backup evidence remains.

Historical live Codex results below remain historical. No supplied credential
was read or used during this completion follow-up. Claude live, targeted OAuth
completion/rotation, future real reset transitions, multi-account upstream phasing
and billing reconciliation still require authorization or external evidence.
Dedicated protocol-specific scoped fallback, provider-owned turn evidence,
finer rollup token metrics, paginated unresolved recovery and some performance
instrumentation remain partial. Full frontend lint fails. See the
[current matrix](control-plane-followup-matrix.md), [compatibility matrix](control-plane-compatibility.md)
and [acceptance audit](control-plane-acceptance.md) rather than treating local test
passes as full live acceptance.

## Historical reset/stagger acceptance follow-up (2026-10-03)

- Scheduler regressions reproduced six original failures before implementation:
  automatic native-duration support, phase-preserving traffic deferral, missed-slot
  burst prevention, stale-policy claim rejection, reset evidence retained through
  cooldown and active-primary-window idle deferral. They pass after the changes.
- Additional coverage checks selected warm-pool reserve enforcement, preservation
  of cooldown evidence while automatic duration is unknown, bounded jitter,
  advanced/reset timestamp requirements, exclusion of unrelated model reset data,
  and five simultaneous reset confirmations retaining distinct pool phases.
  Existing five-account/5h hourly phases, membership changes, durable claims and
  crash recovery remain covered. Clock-driven cases use controlled time, not sleeps.
- Automatic window duration is now an evidence-derived default (`window_seconds=0`),
  not a silent hardcoded five hours. Existing positive configurations remain explicit
  overrides. Resolved duration, spacing and phase persist in schedule JSON entities;
  no new SQL migration is required beyond schema v3. The dashboard displays those
  backend values, reset/stagger reason and the unknown-duration boundary.
- Live Codex warm execution initially exposed missing routing evidence: the usage
  reporter generated an ID different from the execution lease. The reporter now
  shares its attempt ID with SDK execution evidence; stream bootstrap/model/refresh
  retries receive distinct attempt IDs. Side-model settlements retain distinct IDs
  while inheriting bounded parent dispatch evidence. A local warm settlement test
  checks both base and side-model pool/token evidence.
- Fixed live warm smoke used a new isolated localhost instance with the supplied
  test credential: native 18,000-second automatic window/spacing, one successful
  316-token manual warm on the exact account/pool, immediate repeat rejected by
  cooldown (400), then pool opt-out removed schedules. The source credential
  contents remained unchanged and the process stopped. Initial and fixed warm
  runs together reported 632 tokens; no reset timestamp was forged.
- The previously recorded Claude race was fixed: setup-token boolean reads now
  use the metadata lock already used by profile writes. The focused shared-account
  test passed under `-race -count=20`; the broad six-package race suite passed.
- Full `go test ./...`, full bounded-worker frontend suite, production frontend
  build, required server build and control-plane ESLint passed. Multi-stage Docker
  build and isolated runtime/SQLite/restart/backup smoke passed. An initial executor
  integration run failed in the unchanged XAI sessionless keepalive fixture with
  a closed connection; its focused ten-repeat rerun and subsequent full suite passed.
- Evidence: `/tmp/opencode/control-plane-warmer-*.log` and the private live warm
  reports under `/tmp/opencode/control-plane-live-account-*`. Real future reset
  transitions and multi-account upstream stagger still require elapsed time and
  multiple authorized accounts. Controlled five-account fixtures demonstrate
  scheduler behavior, not control over provider reset semantics.

## Authorized Codex account follow-up (2026-10-03)

- Imported the user-supplied `test_account.json` into a private auth directory,
  with no other credentials or storage backends. Codex Plus imported as one
  active account with provider identity. The source contents remained unchanged;
  its permissions were tightened from 0644 to 0600. Tokens and identity details
  were excluded from public output.
- Live WHAM quota refresh returned HTTP 200: five-hour utilization 0%, weekly
  utilization 10%, with native duration/reset evidence. This verifies current
  quota retrieval, not observation of a future reset transition.
- The first discovery run stopped before inference because the preferred older
  mini-model IDs were absent. The local registry exposed `gpt-6-luna`, which was
  selected for subsequent short tests. A local model listing is not proof that
  every listed model is available upstream.
- Live Responses non-stream and Chat Completions SSE both returned HTTP 200 and
  exactly `OK`; the SSE completed with `[DONE]`. Each verification pair reported
  330 and 316 tokens respectively. Two pairs ran (initial reproduction and fixed
  build confirmation), totaling 1,292 provider-reported tokens; no tools were
  invoked. No billing invoice or provider monetary total was compared.
- Initial non-stream persistence incorrectly recorded zero tokens. The existing
  Codex non-stream executor invoked image-tool publication before base usage;
  that helper's `EnsurePublished()` finalized a zero-token base record. A local
  regression failed before the fix for zero and nonzero image-tool usage.
  Publishing base usage first fixes the loss and preserves additional image
  usage. The fixed live pair reconciled response totals, request rows and key
  token counters at **646 tokens**.
- A single-account scoped key admitted two requests, then rejected a third
  locally with 429. Both upstream records used the authorized account with zero
  retries. Revocation returned 401. Restart preserved the logical account ID,
  revoked key and token counter. SQLite integrity was `ok`, file mode 0600.
- Full `go test ./...` and required server build passed. Focused executor,
  control-plane, API and usage tests passed. The new Codex regression passed
  under `-race -count=10`, including additional image-usage preservation.
- The expanded five-package race suite failed in the unchanged
  `TestClaudeExecutorPrepareRequestAuthIsRaceFreeOnSharedCredential`:
  `StoreMetadataString` writes race with `isClaudeSetupToken` reads.
  The same race reproduced from a clean `git archive HEAD` baseline using
  `go test -race ./internal/runtime/executor -run '^TestClaudeExecutorPrepareRequestAuthIsRaceFreeOnSharedCredential$' -count=20`.
  This is an independently confirmed existing issue, not a passing broad race result.
- Only isolated processes were started/restarted/stopped. Private evidence and
  sanitized reports remain under `/tmp/opencode/control-plane-live-account-*`;
  logs include the before-fix regression, full suite and baseline race evidence.
  All test processes stopped. Browser OAuth re-login, refresh-token rotation,
  Claude live access, future reset/stagger behavior and invoice reconciliation
  remain unverified.

## OAuth UX follow-up (2026-10-03)

- Existing Codex/Claude OAuth handlers now also receive remote callbacks through
  the separately authenticated, size-bounded `POST /api/control-plane/oauth/callback`.
  Backend regressions cover admin separation, callback-store reuse, completed-state
  replay rejection and audit redaction. No provider token exchange was mocked as
  successful authentication.
- The adapted original OAuth modal supports non-overlapping automatic status
  polling, cancellation, same-session remote callback submission, account-drawer
  re-auth launch and sign-out/late-launch isolation. StrictMode effect replay does
  not cancel a live session. Local component and integration regressions pass.
- Full `go test ./...`, four-package control-plane/API/auth/usage race suite,
  required `go build -o test-output ./cmd/server`, production frontend build and
  `npx eslint src/controlplane` passed. Full frontend suite: **263 files / 4,211 tests**.
- Native-fetch browser smoke exercised Codex and Claude launch/wait/cancel and
  wrong-state callback rejection against the local fixture without API interception.
  An initial load used an older cached bundle; the smoke was repeated with a
  cache-busting dashboard URL and the new modal. Provider links were not opened;
  real OAuth completion, inference and billing remain unverified.
- The fixture completed via native `POST /test/finish` (204); the new bridge and
  browser were closed. Logs are under `/tmp/opencode/control-plane-oauth-*.log`.

## Runtime and storage

- Focused backend suite passed:
  `go test ./internal/controlplane ./internal/api ./sdk/cliproxy/auth ./sdk/api/handlers`.
- After isolating the WebRTC bridge fixture, full `go test ./...` passed.
- Relevant race suite passed:
  `go test -race ./internal/controlplane ./sdk/cliproxy/auth ./internal/api ./sdk/api/handlers ./internal/client/codex/live`.
- `go build -o test-output ./cmd/server` passed; the generated binary was removed.
- Multi-stage Docker build passed as
  `cli-proxy-api-control-plane:verification`.
- An isolated container with fake config verified `/healthz`, the production
  dashboard bundle, missing/inference admin credentials rejected with 401,
  authorized state/settings operations, restart persistence, online backup,
  backup-first compaction, schema v3, WAL, database mode 0600 and
  `PRAGMA integrity_check = ok`. The container was stopped and removed; isolated
  database evidence remains outside the repository.
- HTTP fixture tests cover inference key scope across handler contexts,
  request admission, standalone Codex Alpha Search admission/scope/history and
  in-flight cleanup, and separation from admin credentials.

## Frontend

- Earlier full `npm run test -- --maxWorkers=2`: 254 test files, 4,176 tests passed.
- After history/settings additions, full suite passed with 255 files / 4,181 tests.
- Latest full suite after timeline, draft/session isolation and integrated
  monitoring tabs: 256 files / 4,185 tests passed.
- After original account/provider/quota components and snapshot polling:
  257 files / 4,190 tests passed.
- Final navigation-polish suite: 257 files / 4,191 tests passed.
- Final account-detail/settings integration and drawer-close regression suite:
  259 files / 4,197 tests passed, including a second full rerun after the browser fix.
- Earlier focused `npm run test -- src/controlplane`: 4 files, 16 tests passed,
  including warmer timeline, unsaved-settings refresh/discard and late-session
  history isolation.
- The later focused suite additionally covers native quota view-model adaptation,
  original provider-tab filtering, collapse/breadcrumb semantics and non-overlapping
  snapshot polling; its lint and production build passed.
- `npm run build` passed (TypeScript and single-file Vite production bundle).
- Focused ESLint for `src/controlplane` passed. The original frontend flat
  configuration is retained as `web/eslint.config.js`.
- Full `npm run lint` reports 196 errors and 4 warnings in 46 original
  CPA-Manager-Plus files. Every diagnostic file was byte-compared against
  `reference/CPA-Manager-Plus/apps/web` and is unchanged. These original pages
  are not production routes; their source-wide lint debt is not hidden by
  disabling lint rules.
- Browser fixture smoke previously verified key create/revoke and scoped
  inference, account pause, pool edit, backup, sign-out, dark theme and a 390px
  mobile layout. A later interactive fixture checked the reused provider tabs,
  native quota cards (90% remaining), rendered warm-up timeline, 390px layout,
  automatic snapshot refresh preserving settings drafts, saved settings, audit
  tables, realtime/history tabs and sign-out.
  CloakBrowser blocked native loopback frontend fetches with
  `ERR_INSUFFICIENT_RESOURCES`; API requests in this later render smoke used
  Playwright request transport and route fulfillment against the real local
  fixture. This is component/render verification, not certification of the
  browser's native fetch path. The browser, new test bridge and fixture were
  closed cleanly. No production network policy was modified.
  The first interactive fixture hit Go's default ten-minute timeout during
  debugging; the clean opt-in rerun used a fifteen-minute interactive budget
  and completed through its finish endpoint. Ordinary suite timeouts are unchanged.
- One unrestricted concurrent suite run timed out in the unchanged reference
  `AccountsPage` full-history test; the bounded-worker full rerun passed.
- A later four-worker run concurrent with Docker/build verification timed out
  in the unchanged reference `demoPersistIsolation` test. The final two-worker
  run without competing builds passed; neither timeout was hidden by raising
  test timeouts or skipping tests.

## WebRTC test isolation and performance boundary

Earlier `go test ./...` runs failed in
`TestPionMediaRelayBridgesAudioAndDataChannel` with
`upstream DataChannel was not created`, including against clean HEAD. The
in-process bridge test now confines its four peer APIs to loopback candidates,
avoiding host Docker/veth topology and irrelevant ICE candidate pairs. The
actual bidirectional audio and text/binary DataChannel assertions are unchanged.
The focused test passed ten consecutive runs. Production relay networking,
private-address filtering and firewall rules are unchanged.

New regressions walk all 600 same-second request records without omissions or
duplicates, and verify analytics filters still find older matching rollups
behind more than 5,000 unrelated groups. The dashboard cursor/filter-reset
interaction is covered by a component test.

The native snapshot benchmark with 128 candidate accounts measured
306,418 ns/op, 21,353 B/op and 31 allocations/op on this host. This measures
candidate filtering, not end-to-end inference throughput or scored routing's
durable decision writes. SQLite dashboard bounds and backup/compaction
accounting preservation are regression-tested; this is not a production load
certification.

Live provider inference/billing, OAuth re-login and upstream reset phasing
remain unverified. Operational and accounting limitations are documented in
`control-plane.md`.

## Continuation slices

- Pool-specific health policies and persisted cooldowns are covered by threshold,
  rate-limit, independent-pool, scope and restart tests.
- Control-plane usage settlement now runs synchronously at publication; immediate
  store close/reopen verifies committed usage without waiting for the async queue.
  Ordinary usage plugins remain asynchronous and are tested for single delivery.
- Complete analytics totals and later breakdown pages are tested beyond 5,000
  groups; quota/decision/warm/audit cursor walks check exact counts and duplicates.
- Provider disable policy covers dispatch, background scheduling and manual quota
  probes. Model-specific price settlement and the storage-failure admission gate
  are regression-tested.
- Transient pool cooldown/pressure leaves warm schedules intact; claim enforcement
  prevents probes without erasing persisted phase/last-probe evidence. Durable
  accounting panic recovery closes admission instead of silently failing open.
- Full Go suite and the control-plane/API/usage/auth race suite passed after the
  continuation backend changes. Required Go build and control-plane ESLint passed.
- The isolated Docker smoke additionally verified runtime security inspection,
  paginated analytics/history API availability and persistence of provider policy,
  retention and pricing settings. No production resources were changed.
- The final Docker image was rebuilt after the monitoring/timeline additions;
  health, auth isolation, maintenance and restart smoke were rerun against it.

## Final local-management slice

- Explicit identity-less re-auth continuity has regression coverage for exact
  confirmation, independent admin authentication, audit persistence, paused
  replacement eligibility, provider/principal conflicts, key-scope rejection,
  logical metadata preservation and restart. It is an operator assertion, not
  proof of upstream principal identity.
- Full Go suite, control-plane/API/auth/usage race suite and required Go build
  passed after the reassociation backend changes. Frontend build and focused
  ESLint passed after account details and segmented settings were integrated.
- Browser render smoke exercised the actual account drawer/re-auth tab,
  security metadata, provider settings tabs, sign-out and the 390px layout.
  It reproduced a drawer-close interruption caused by an unstable callback
  during polling. The callback is now stable, its refresh identity has a
  regression assertion, and desktop/mobile drawer closure passed the rerun.
  API transport still used request routing as described above; native-fetch
  certification remains out of scope. The fixture finished with HTTP 204.
- Logs are retained under `/tmp/opencode/control-plane-finish-*`; no live
  provider or production deployment was used.

See [the acceptance audit](control-plane-acceptance.md) for the adapted frontend
scope and operational boundaries. Passing local tests does not verify live
OAuth, inference, provider billing or real quota reset phasing.

## Acceptance closure continuation

- Accounts now reuse the actual `AccountMetricsGrid`, `AccountProviderTabs`,
  `Input`, `Select`, `CopyableText`, `PaginationControls` and reference page styles.
  Search/status/plan/pool filters, deterministic quota/reset/recent/request sorts,
  page reset and refreshed inventory shrink are tested. Stale, amount-only,
  model-specific and expired-reset evidence are not promoted to made-up
  account-wide percentages or available capacity.
- Original off-canvas mobile sidebar/backdrop behavior replaces the earlier
  static mobile button grid. Direct page hashes and Back/Forward are tested.
  Overview uses original monitoring summary cards rather than generic counters.
- HTTP inference rejection observation persists sanitized route-pattern/status
  evidence and rollups without touching settlements, account billing, key limits,
  affinity or token/cost counters. Published executor usage suppresses the outer
  HTTP failure record, avoiding duplicate request accounting. Tests verify
  credential/body/query/resource-ID redaction and restart persistence.
- Full `go test ./...`, relevant four-package race suite and required Go build
  passed. Full frontend suite: **262 files / 4,204 tests passed**. Control-plane
  ESLint and TypeScript/Vite build passed. Source-wide reference lint debt was
  rerun and remains visible; no lint rules were relaxed.
- The final CloakBrowser run used **native frontend fetch**, without API route
  interception or response fulfillment. Sign-in, search, pause/resume, account
  drawer/re-auth view, security metadata, direct links and Back/Forward passed.
  On 390px mobile, off-canvas opening/selection/closing, drawer closing and no
  horizontal document overflow passed. A native invalid-key POST returned 401
  and appeared as a local rejection in Monitoring. Sign-out cleared the session.
- Browser verification found the original shell header's high stacking order
  intercepted the desktop drawer close button. The control-plane shell header
  now stays below the original drawer portal layer; real pointer closure passed
  the rerun. Final CSS was rebuilt after this fix.
- The fixture finished through native `POST /test/finish` with 204; browser and
  the new bridge closed cleanly. Logs: `/tmp/opencode/control-plane-acceptance-*`.
  No production resources or live OAuth accounts were used.
- Final multi-stage Docker image was rebuilt after the shell-layer fix. Isolated
  runtime smoke passed health/dashboard/admin isolation/schema v3/WAL/0600,
  settings restart persistence, online backup and compaction. Its container was
   stopped and removed; `git diff --check` passed and the Go build artifact was removed.

## Requested dashboard source integration (2026-10-04)

This section verifies the `itsmylife44/cliproxyapi-dashboard` integration.
Earlier CPA-Manager-Plus frontend checks above describe the retained Operations
bundle, not this dashboard.

- Imported 388 source files at `b062653e34d94644b73e3b7bac85c87a92607c0b`.
  Verified all recorded SHA-256 hashes against the pinned Git objects, including
  the repository-root AGENTS and license path overrides. Verified preserved
  upstream package manifests and the original server home. Integration details
  and build boundaries are in `dashboard/README.native.md`.
- `dashboard`: `npm run typecheck`, `npm run lint`, `npm test` and `npm run build`
  passed. The native suite contains **2 files / 13 tests**. Tests cover admin
  separation, explicit scopes, credential-import translation, session-body races,
  late OAuth cancellation, unsafe/malformed links, device flow, disabled logging,
  quota freshness and bounded usage totals without invented token tiers.
  Native lint checks `native/`, not the entire imported Next.js source. Vitest
  emits an import-extension warning for its config; the run exits successfully.
- `/usr/local/go/bin/go test ./...`, the required server build, and
  `go test -race ./internal/api ./internal/api/handlers/management
  ./internal/controlplane -count=1` passed after the native bridge changes.
  New regressions cover redaction, duplicate/ambiguous keys, concurrent additions,
  failed-save rollback, logging/key configuration ownership, separate admin auth,
  native provider discovery, targeted re-auth validation and dashboard deep links.
- Local Chromium used native browser fetch against the Go fixture, without
  API response fabrication. Desktop and 390px mobile routes passed. The UI
  required an explicit account scope before key creation; an authorized **fake
  executor** produced settled usage through the actual UsageReporter. Reload
  cleared login; the final run produced no page or console errors. The original
  tooltip needed viewport clamping to avoid invisible mobile overflow.
- Separate browser **simulations** delayed OAuth launch and polling responses.
  Closing a modal cancelled a late launch; an old successful poll could not
  complete a replacement modal. Those checks intercepted only fixture OAuth
  endpoints and contacted no provider. Live OAuth completion remains unverified.
- Multi-stage Docker build passed. The image includes the requested single-file
  dashboard, Operations and attribution. Only `cpa-dashboard-review` received the
  new image `cli-proxy-api-control-plane:dashboard-native`, with localhost port
  `18347` and the existing private Tailscale HTTPS route on `8443`.
- Local and Tailscale health, dashboard/deep-link bundle hashes, Operations,
  unauthenticated rejection and authenticated read-only APIs passed. Native
  Antigravity appears; absent Gemini CLI plugins do not. The review instance
  retained its empty account/pool/key/quota inventory. HTTPS browser checks passed
  on desktop and 390px mobile, including enabled Antigravity and disabled Gemini
  CLI buttons, reload logout and zero page errors. No live provider OAuth,
  inference, quota refresh or credential import ran during review verification.
- Retained rollback: stopped container
  `cpa-dashboard-review-before-native-20261004`, original image and private offline
  config/data backup at `/tmp/opencode/cpa-dashboard-review-before-native-20261004`.
  The new review container reports healthy. No commit, push or production deploy.
  Fixtures, browser contexts and the temporary browser bridge closed cleanly.

Evidence: `/tmp/opencode/cpa-dashboard-native-go-{full,race}-final.log`,
`cpa-dashboard-native-docker-build-final.log`,
`cpa-dashboard-native-browser-smoke.log`, `cpa-dashboard-native-oauth-race.log`,
`cpa-dashboard-native-fixture-final.log`, `cpa-dashboard-native-review-check.log`
and `cpa-dashboard-native-review-browser.log`. Screenshots reside under
`/tmp/opencode/cpa-dashboard-native-review-screenshots/`.

Remaining boundaries: no live OAuth completion/token rotation, loaded-plugin
login acceptance, invoice reconciliation, real provider reset transition or
multi-account live staggering. Usage key/model breakdowns remain a bounded first
page; raw request evidence stays in Operations. The external PostgreSQL users,
Docker administration, sharing, Telegram and sidecar features do not run here.
