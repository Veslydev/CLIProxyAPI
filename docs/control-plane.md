# Built-in control plane

The fork runs the existing CLIProxyAPI runtime and an optional local SQLite
control plane in one process. The default dashboard uses the actual
`itsmylife44/cliproxyapi-dashboard` source, pinned in `dashboard/UPSTREAM.json`.
The fork retains the native Operations UI at `/control-plane`. It runs no
PostgreSQL dashboard service and contains no Headroom integration. See
`dashboard/README.native.md` for the integration boundary and attribution.

## Enable

Build the dashboard with Node 22.12+ and the server with Go 1.26+:

```sh
cd dashboard
npm ci --no-audit --no-fund
npm run build
cd ..
cd web
npm ci --legacy-peer-deps --no-audit --no-fund
npm run build
cd ..
go build -o cli-proxy-api ./cmd/server
```

Generate a separate random admin credential in your secret manager and expose
it through `CONTROL_PLANE_ADMIN_SECRET` (at least 32 bytes). Never use an
inference key for this credential. Enable the canonical v8 configuration:

```yaml
management:
  control-plane:
    enabled: true
    database: data/controlplane.sqlite
    admin-secret-env: CONTROL_PLANE_ADMIN_SECRET
    dashboard-file: dashboard/dist/index.html
    operations-dashboard-file: web/dist/index.html
    retention-days: 30
```

The legacy top-level `control-plane` spelling is migrated to
`management.control-plane` by the existing YAML boundary. Relative database and
dashboard paths resolve against the configuration file's directory.
Use HTTPS through the deployment's reverse proxy. `/dashboard` serves the
public application shell; `/api/control-plane/*` requires the separate bearer
admin credential. The frontend keeps it only in memory. Reloading signs out.
Invalid initialization fails closed, including embedded HTTP handlers.
Changing activation, database path, dashboard path or admin-secret source
requires a process restart; pool/key/quota/settings changes are live.

Existing YAML inference keys still use the original access manager; they are
not silently replaced. Newly created `cp_` keys require explicit per-provider
account or pool bindings. Empty scope is invalid. Only a cryptographic digest
and display prefix persist; the secret is returned once at creation. Request
limits are admitted transactionally once per runtime invocation, before
retries. Token/cost limits stop **new** requests after settled usage reaches
the limit; already-running work can overshoot before final usage is known.
Standalone Codex Alpha Search also enforces admission and scoped selection,
tracks in-flight cleanup and records status/latency/quota headers. Its upstream
does not report token usage; those records do not estimate missing tokens.

Local control-plane routing and CLIProxyAPIHome dispatch are mutually
exclusive. Existing executors, translation paths and OAuth implementations
remain in place. The new admin API reuses existing Codex/Claude OAuth handlers,
not deprecated `/v0/management` routes.

## Accounts and scope

Startup and periodic synchronization discover runtime auth records without
moving them. Stable identity uses provider subject/account IDs and workspace
or organization IDs, never email alone. Claude account/organization UUIDs are
supported. If a provider exposes no stable subject, identity falls back to
the credential ID; changing that ID on re-login requires the explicit audited
re-auth continuity flow below rather than unsafe automatic matching.
Refresh/access tokens are not copied into SQLite.

Implicit `default-<provider>` pools preserve existing provider routing. The
dashboard can create provider-specific pools, change membership, restrict
models, reserve quota, toggle soft sticky and configure warm-up. Pool-bound
keys follow live membership. Candidate filtering applies before selection,
including retries, plugin delegation and credits fallback. It may reduce,
never expand, runtime eligibility. Unsupported models and native health
cooldowns are filtered by the SDK before control-plane scoring.

## Strategies and affinity

Available strategies:

| Strategy | Behavior |
| --- | --- |
| `native` | Original CLIProxyAPI selector (including weighted routing) |
| `round_robin` | Rotate scoped eligible accounts |
| `capacity_weighted` | Prefer remaining absolute capacity, otherwise relative headroom; penalize in-flight work and failures |
| `relative_availability` | Random weighted choice by relative headroom |
| `usage_weighted` | Prefer less-used accounts using durable settled request counts |
| `fill_first`, `sequential_drain` | Use the first stable eligible credential until unavailable |
| `reset_drain` | Prefer usable headroom with a nearer reset; account for in-flight pressure |
| `single_account` | Persist first owner per key/provider/model and fail rather than reassign |

Pool strategy overrides the global strategy except `native`, which inherits
the global setting. Multiple providers are selected before scoring; quota
scoring never mixes incompatible providers. A key bound to several pools uses
the first matching pool in binding order for each account; avoid conflicting
policies in overlapping pools.

Warm scheduling does not use that first-binding routing rule: every opted-in pool
retains its independent schedule, with account-wide duplicate suppression below.

Soft sticky hashes session headers or `prompt_cache_key`, scoped by key and
provider. Reassignment is permitted only inside the current eligible scope.
`sticky_min_remaining_percent` (0–100, default 0) stops preferring a soft owner
below its fresh quota headroom threshold; it never moves a hard owner.
`failure_penalty` and `in_flight_penalty` (nonnegative, default 1) configure
scoring pressure for capacity/usage/reset strategies. Zero disables that
penalty, not eligibility checks or native cooldowns.
`phase_preference` (0–0.05, default 0) is an optional scheduling-locality hint for
`capacity_weighted`, `usage_weighted` and `reset_drain`. It multiplies the eligible
score by at most 1.05, favoring a nearer future slot. It requires known quota and
a fresh, active, unclaimed, policy-matching schedule for the selected pool.
Missing/stale schedules and unknown quota produce no hint. Decisions retain the
phase pool/next/bonus separately from real quota/reset evidence. Native/random/
ordered strategies and hard owners are not reassigned by this preference.
Codex hard ownership tracks response, conversation, file/resource IDs and
encrypted continuation hashes from observed responses. It examines the
**original request**. Unknown, conflicting, expired or unavailable owners
fail explicitly; there is no unproven replay/migration. Objects created
outside this proxy need a fresh request without account-bound continuation
state. Ownership records currently live for 30 days; single-account pins live
for 100 years and intentionally do not reassign on pool membership changes.
Client `turn_id` values in passthrough metadata are preserved by the runtime but
are not proven provider-owned objects; arbitrary turn IDs are not automatically
promoted to hard affinity. Provider-native turn ownership remains unverified.

## Quota evidence

Codex adapters parse native response headers and the authenticated WHAM usage
endpoint: primary 5h, weekly/monthly secondary and additional feature windows.
Claude adapters parse native unified/rate-limit headers and OAuth usage JSON,
with canonical `5h`, `7d` and model-specific Sonnet/Opus windows. Headers and
body observations update the same canonical keys. Model-specific windows do
not block unrelated models. Additional features without an identifiable
model are displayed but not treated as account-wide exhaustion.

Quota records include raw/canonical keys, observation source/time, resets,
duration and capacity when exposed. Missing data stays unknown. Stale or
past-reset evidence no longer proves exhaustion; native runtime cooldowns
still apply. Automatic refresh runs approximately every five minutes;
manual refresh is available per supported account. Other providers retain
normal routing without fabricated quota. Live OAuth/provider inference is
not required by fixture tests and must be validated with your own accounts.

## Warm-up

Both pool and account must opt in. Warm-up makes **real billable upstream
requests**, pinned to the selected account and pool, using the configured model/prompt
and a small output budget. Default phase spacing is primary window duration
divided by opted-in healthy accounts (five accounts / five hours = one hour).
`window_seconds: 0` (the dashboard default) derives that duration from fresh native
primary-window evidence for every eligible member. Unknown/incompatible durations
suspend automatic probing without deleting previous cooldown evidence; use an
explicit positive duration only as an operator override. `spacing_seconds: 0`
divides the duration by eligible membership. Manual spacing and deterministic jitter
are configurable; jitter is capped to a quarter of the assigned spacing. The
dashboard shows persisted window, spacing and phase rather than estimating from
all configured members. Persistent claims,
minimum cooldown, health checks, in-flight checks and recent real-traffic
suppression prevent duplicate or unnecessary probes. Manual warm bypasses
idle suppression, not opt-in, health or cooldown.

A reset is confirmed only by a new lower usage watermark observed after the
previous reset boundary **with an advanced native reset timestamp**. Unrelated
model-specific evidence does not trigger an account-wide probe. `warm.reset_mode`
is `phased` by default: simultaneous confirmations queue assigned phase slots.
Explicit `immediate` queues the first eligible dispatch without waiting for a phase.
It still obeys reserve/model/provider/health checks, the global account claim,
cooldown and recent real-traffic suppression. The worker runs every 30 seconds;
"immediate" is not a synchronous request from the quota ingestion handler.
Cooldown delays a confirmed reset instead of discarding it. Traffic deferrals keep
phase alignment in phased mode; immediate mode waits only for eligibility.
Automatic phased slots missed by at least a minute are rephased rather
than replayed as a catch-up burst. Active primary windows with positive usage and
fresh evidence defer idle staggering until a slot after their reset. A
crash-interrupted claim is recorded as `interrupted_no_replay`, never blindly
replayed. Claims reject stale pool/policy/slot snapshots and capture dispatch
model/prompt/provider before execution. Membership/policy changes recalculate
phases; obsolete schedules remain inactive, and evidence loss suspends them.
Historical probes remain visible, but inactive/suspended/claimed rows do not
appear as future timeline points.

Every pool/account pair has its own schedule. Earliest eligible slot wins;
simultaneous slots prefer manual, then confirmed reset, then stable pool ID.
`Warm now` selects that effective policy, not an arbitrary first membership.
Each account has one global claim and the longest enabled member-pool cooldown,
captured at claim time. A probe services the account and advances competing
policies as shared evidence; it does not open independent quota windows per pool.
Cooldown survives opt-out/re-enable, membership changes, policy edits, missing
evidence and restart. Interrupted claims retain `interrupted_no_replay` history.
The timeline and account drawer expose retained/competing/effective policies.
Scheduling attempts to phase requests; it cannot force
upstream fixed windows or guarantee resets move when a provider does not
start windows on first use. Warm-up usage is visible but does not count as
recent real traffic.

## Dashboard and telemetry

The default `/dashboard` routes use the imported cliproxyapi-dashboard shell,
navigation, styles and components: Quick Start, Providers, API Keys, Quota,
Usage, Monitoring and Logs. Settings and Operations link to the native
control-plane controls. The Go bridges use the native v8 OAuth dispatcher,
including Antigravity and enabled registered provider plugins. The UI disables
providers whose plugins are absent. Browser admin credentials stay in memory.

The native Operations UI at `/control-plane` retains Overview, Monitoring,
Usage Analytics, Accounts, API Keys, Pools, Routing Settings and Settings.
Its source comes from CPA-Manager-Plus with backend-bound pages. The following
telemetry and routing controls describe **Operations**, not full feature parity
with either external dashboard product.

Monitoring polls persistent history every five seconds. Filters cover time,
provider, model, account, pool, key and status. Usage records contain requested
and actual models, selected account/key/pool, status, token breakdown, TTFT,
latency and explicit operator price estimates. When dispatch context is
available, records include retry ordinal, redacted error category and routing
evidence with quota headroom, reset, in-flight pressure and skipped reasons.
Raw upstream failure bodies, OAuth tokens and session IDs are excluded.
The original CPA-Manager-Plus monitoring tab component separates a latest-only
realtime feed from cursor-driven persistent history. Both use the same durable
request source, not a separate manager service.
Account provider tabs, health badges and quota window cards are reused directly
from the same frontend. Native quota evidence is adapted to their view model
without invented current/previous cycles, usage forecasts or quota percentages
for amount-only windows. Account/quota/analytics snapshots also poll every five
seconds; slow loads do not overlap.
Account search, provider/status/plan/pool filtering and quota/reset/recent/request
sorting use the actual reference input/select, account metrics and pagination
components. Filtering resets pagination; polling preserves active filters.
Model-specific windows never imply account-wide exhaustion in these metrics.
Direct links such as `/dashboard#/accounts` and browser Back/Forward work without
rewriting proxy routes. Credentials and OAuth state are never written to page
history. Mobile uses the original off-canvas sidebar and backdrop.
Account details use the original responsive drawer and segmented tabs. Settings
uses the same tab component for provider policy, pricing, security and database
maintenance, with a structured read-only deployment/security panel.

### Provider sign-in and remote callbacks

Accounts and the account drawer can launch Codex/Claude add/re-auth sessions.
For provider-identified accounts, drawer re-auth binds the launch to the selected
logical ID (`GET /api/control-plane/oauth/:provider?account=:id`). A server-side,
one-time five-minute guard compares the newly returned native principal/workspace
before existing metadata merge, hooks or credential persistence. Different or
missing principal evidence is rejected; neither email nor callback-state possession
proves identity. Replay, cancel/expiry and concurrent sessions are isolated.
Once saving begins cancellation returns failure instead of promising rollback.
This validates a local contract, not live OAuth completion or token rotation.
The adapted OAuth dialog polls automatically and cancels through the existing
provider-session handler before closing. Signing out requests cancellation and
clears the dialog; a late launch response is cancelled rather than installed in
the next admin session. Cancellation failure is reported, not assumed successful.

For remote installations, paste the complete localhost redirect into **Remote
callback URL**. The dialog requires the active state and sends only provider,
state, code/error to the separately authenticated, size-bounded
`POST /api/control-plane/oauth/callback`. The proxy does not fetch the pasted URL.
The existing OAuth handler validates provider/state and writes its normal callback
file. Callback data is not placed in navigation history or audit details.
Closing the browser without explicit cancellation can leave a session pending
until the existing backend expiry; browser unload is not a cancellation guarantee.

### Identity-less re-authentication

Normal same-principal provider-identified re-login retains logical identity.
When a provider does not supply a stable subject and a new login changes the
credential reference, the proxy cannot prove sameness from an email. The account
drawer provides a separate **Re-auth continuity** flow: verify the upstream account,
pause the unused replacement, disable its warmer and enter the original logical
account ID to attach it. The separate admin endpoint
`POST /api/control-plane/accounts/:id/reassociate` accepts `source` and
`confirmation` (the target logical ID). This is an explicit operator assertion,
not automatic principal verification.

The backend rejects cross-provider or provider-identified replacements, used or
in-flight replacements, configured-pool membership, key scope and affinity owners.
It transactionally reassigns credential references and records an audit event.
The target keeps its pools, pause/warm flags, usage limits and hard ownership;
the imported source becomes read-only evidence and is removed from its implicit
pool. No source history, keys or ownership are merged. A later different native
principal receives a different logical identity even after manual association.
Overview shows fresh/stale quota evidence by provider. Accounts show assigned
pools; pool summaries distinguish active members from model-specific exhausted
windows. Analytics supports a start/end date and exact provider/model/account/
pool/key filters. The paginated analytics endpoint returns complete matching
totals and 100 breakdown groups per page (500 maximum), with daily chart
aggregates over up to 3,660 dates. The legacy endpoint remains bounded at
5,000 groups. Analytics filters are applied in SQL before pagination. Monitoring has
Older/Newer/Latest controls using a `(timestamp, request ID)` cursor so requests
sharing a timestamp are not lost between pages. Filter changes reset the page.
The API accepts `before` (Unix seconds) and `before_id`; timestamp-only legacy
cursors keep their original behavior. Concurrent insertion/retention can alter
history between page loads; pagination is not a frozen database snapshot.
Request rollups count executor attempts, not only successful client requests.
Control-plane usage settles synchronously at the existing usage publication
boundary; ordinary telemetry plugins retain asynchronous delivery. Each attempt
gets a distinct execution identity shared by its routing evidence and usage
reporter; retry/fallback attempts retain distinct identities, while additional
model usage retains a distinct settlement ID and its parent dispatch evidence.
Settlements and rollups are
idempotent by execution ID, including replay after raw retention. A durable
settlement-ID table survives telemetry cleanup. Publication returns only after
the SQLite settlement commits. Persistence failure stops subsequent admitted
requests until storage is repaired and the process restarted. A crash before
the runtime publishes final usage remains outside exact settlement durability.
Schema v4 records each observed attempt as `running`; release without publication
becomes `unresolved_no_usage`, and startup changes leftover running rows to
`unresolved_restart`. Real late usage with the same attempt ID can settle it;
no tokens or cost are inferred. Stream attempts have independent IDs/journals,
closed via the SDK completion observer rather than the earlier account lease.
Failure to write the journal gates later admissions but cannot prevent the
attempt already starting. The observer-to-upstream-call interval can also leave
an unresolved row without proof that upstream execution began.

Monitoring and authenticated `GET /api/control-plane/executions/unresolved` show
the latest 100 unresolved attempts, independently of request filters. Older
unresolved rows remain durable, but that API is not a paginated recovery export.
Raw retention removes old settled journal rows, not unresolved recovery evidence
or settlement IDs. There is no automatic provider reconciliation endpoint proven
to recover exact per-attempt usage. Provider billing comparison remains necessary
for invoice-grade accounting and requires separate authorized evidence.

Pools can require active health, exclude accounts at an in-flight threshold,
and configure independent rate-limit/error cooldowns and a consecutive-error
threshold. Cooldown state is persisted per account/pool; another pool does not
inherit it. An overlapping successful request does not erase a committed
cooldown. In-flight thresholds are selection-time pressure controls, not an
atomic concurrency reservation limit.

Settings persist provider dispatch/probe disable flags, quota refresh intervals
(zero uses 300 seconds), raw telemetry retention (zero uses deployment default),
and provider/model pricing overrides. Provider-specific prices take precedence
over provider-independent prices, then global prices. Explicit zero-price model
overrides produce known zero cost rather than unknown pricing. Runtime security inspection
reports deployment configuration without revealing credentials. Admin secret
rotation and TLS changes remain environment/configuration operations requiring
restart, not browser-side secret editors. Quota, decision, warm-up and audit
history use ID cursors with Older/Newer controls (100 default, 200 maximum).
History is shown in structured tables with expandable redacted evidence. Pool
warm-up timelines plot persisted last/next probe timestamps using the integrated
CPA-Manager-Plus chart component. Account rows expose logical IDs as well as
display names. Automatic refresh preserves unsaved settings until save or explicit
discard; late action/history responses from a signed-out session are ignored.

Routing snapshots are immutable and cached. Request admission and usage
settlement update only changed account/key snapshot maps. Routing decisions
and ownership still use synchronous SQLite writes for durability. SQLite
uses one connection and serialized transactions; no global lock is held for
the lifetime of streams. High-throughput multi-instance deployment is not
supported: do not share one SQLite database between proxy processes.

## Database and recovery

Schema v1 creates typed JSON entities, credential references, key digests,
quota history, hashed affinity, requests, daily rollups, audit, routing
decisions and warm-up runs. Schema v2 adds provider/model/time, rollup/time,
decision/time and affinity expiration indexes. Schema v3 adds durable
settlement IDs (backfilled from retained requests) and warm-up account/time
indexes. Schema v4 adds indexed execution states and pool-tagged warm-up history,
migrates account-keyed schedules to hashed pool/account keys, and separates global
claim/cooldown guards. Old warm runs keep an unknown pool; no provenance is guessed.
Migration is transactional and pre-upgrade backups retain the old schema/rows.
Settlement IDs are deliberately not removed by raw retention.
WAL, foreign keys, a five
second busy timeout and FULL synchronization are enabled. Database and online
backup files are private (`0600`); parent directories are created `0700`.
Upgrades of existing databases create a `VACUUM INTO` snapshot before the
transactional migration. A binary refuses a newer schema.

Settings can create an online backup, retain raw telemetry or explicitly
compact SQLite. Compaction creates a backup first and temporarily serializes
database access. Automatic retention runs daily (default 30 days) and keeps
long-term rollups and audit. Backups contain account metadata and key hashes;
protect them even though OAuth secrets are absent. Backups are stored next
to the database and are not exposed for browser download.

Restore:

1. Stop this proxy process; preserve the current database **and** WAL/SHM as
   recovery evidence. Do not copy only the main file from a running instance.
2. Copy the chosen online backup to a new database path with mode `0600`.
3. Point `management.control-plane.database` at that path and start a binary
   compatible with its schema. Existing auth files must remain available.
4. Check dashboard accounts, key bindings, pools and an inference smoke test
   before retiring the old files. Downgrade uses the pre-migration backup,
   never an older binary against a newer database. Restoring the pre-upgrade
   snapshot discards mutations made after its creation; preserve the current
   state until restoration and required metadata/accounting checks pass.

## Docker

`Dockerfile` builds Node dashboard, Go server and Debian runtime, serves the
static bundle in-process, includes attribution, declares `/data` and probes
`/healthz`. Use `docker-compose.control-plane.yml` with your config and admin
secret. Set database to `/data/controlplane.sqlite`, dashboard to
`/CLIProxyAPI/web/dist/index.html`, and auth directory to `/root/.cli-proxy-api`
for its mounts. Existing auth files remain separate from SQLite. Do not use
the template inference keys in production. Back up both the online SQLite
snapshot and existing auth storage through your secret-safe backup system.

## Verification

See [the verification record](control-plane-verification.md) for actual test,
build, isolated runtime results and inherited failures.
The [current requirement/page parity matrix](control-plane-followup-matrix.md),
[provider/model/protocol coverage](control-plane-compatibility.md) and
[exact residual lint inventory](control-plane-lint.md) distinguish implemented
contracts from partial or blocked acceptance.

```sh
go test ./...
go test -race ./internal/controlplane ./sdk/cliproxy/auth ./internal/api
npm --prefix web test -- --maxWorkers=2
npm --prefix web run build
go build -o test-output ./cmd/server
docker build -t cli-proxy-api-control-plane:local .
```

Tests exercise migrations/backups/WAL, identity, strict scope, atomic limits,
quota parsing/isolation, sticky/hard ownership, single-account persistence,
retry/plugin fallback, streaming disconnect leases, usage idempotency,
deterministic phases, claim/restart/cooldown and actual HTTP authorization.
Live OAuth, actual provider billing and upstream window phasing require
operator verification. Dashboard requests, histories and analytics breakdowns
are paginated; analytics totals aggregate all matching groups. Offset-based
analytics pages can shift under concurrent rollup updates; they are not frozen
exports. Registered inference routes persist HTTP failures without published
executor usage as `http_rejection` evidence, including invalid keys and
pre-executor admission failures. Only route patterns, status, local key ID when
known and latency are stored, never body/query/credential/resource-ID values.
These failures contribute to request/error analytics but do not fabricate
upstream tokens/cost, change key limits, or create settlement/affinity ownership.
Published execution usage suppresses the outer HTTP rejection record.
