# CLIProxyAPI Vesly Fork - Implementation Task

You are working on the fork at:

- Target repository: /root/cpa/CLIProxyAPI
- Upstream: router-for-me/CLIProxyAPI
- Dashboard reference/source: /root/cpa/reference/CPA-Manager-Plus
- Routing/quota reference: /root/cpa/reference/codex-lb

Current working branch should be `vesly-control-plane`.

Do not merely write a plan. Inspect the local repositories, implement the work, run builds/tests, fix failures, and leave the target repository in a usable state.

## Core rule

CLIProxyAPI remains the proxy/runtime base.

Preserve its existing provider support and protocol translation. Do not rewrite working OpenAI/Responses, Codex, Anthropic/Claude, Gemini, Grok/xAI, Kimi, Muse, Devin, streaming, WebSocket, tools/function-calling, multimodal, OAuth, or request translation paths unless an extension is actually required for the new control-plane features.

The fork should feel like CLIProxyAPI with a serious built-in control plane, not a different proxy glued in front of it.

## Absolutely no Headroom

Do NOT integrate Headroom.
Do NOT add a Headroom sidecar.
Do NOT add Headroom configuration.
Do NOT add context compression/optimization features from Headroom.

This fork does not need Headroom.

## Dashboard requirement: use CPA-Manager-Plus

The dashboard should be based directly on the current CPA-Manager-Plus UI in:

`/root/cpa/reference/CPA-Manager-Plus`

I specifically want the CPA-Manager-Plus dashboard/UI, not a new dashboard inspired by codex-lb.

Reuse/adapt the actual CPA-Manager-Plus frontend structure, layout, styling, components, charts, pages, filters, account views, quota views, monitoring UX, and general visual design where practical.

Do not redesign it into a generic admin panel.

The final product should not require a separately installed CPA-Manager-Plus instance. Integrate the dashboard into this fork and wire it directly to the fork's own backend/control-plane APIs.

Preferred final shape:
- one CLIProxyAPI application
- built-in web dashboard
- one SQLite control-plane database
- no separate manager product required

It is acceptable to keep the web frontend as a normal React/Vite app during development and build/embed/serve the production bundle from the Go application.

Preserve required third-party license/attribution notices when adapting code.

## SQLite control plane

Use SQLite as the default persistent database.

Suggested path:
`data/controlplane.sqlite`

Use:
- WAL mode
- foreign keys
- busy timeout
- migrations/schema versioning
- automatic backup before risky migrations
- indexes for hot dashboard/routing queries
- retention/compaction for request logs
- batched/asynchronous telemetry writes where safe

Security/routing/accounting state must remain durable.

Persist at least:
- stable logical accounts/credentials
- provider metadata
- quota windows/history
- account health
- account pools
- pool membership
- API keys
- API-key account/pool bindings
- API-key restrictions/limits
- sticky sessions
- continuation ownership
- warm-up policies/schedules/runs
- request logs
- usage events/rollups
- settings
- audit events

Do not duplicate OAuth secrets into SQLite unless necessary. Prefer referencing CLIProxyAPI's existing auth records/files using stable logical account IDs.

## Stable account identity

Each provider credential/account needs a stable logical account ID that survives:
- OAuth refresh
- auth file rewrite
- re-login
- restart
- display name changes

Do not use email alone as identity.

Support same email with multiple provider workspaces/accounts.

## Account pools

Implement first-class provider-specific account pools.

Example:

Pool: personal-codex
Provider: codex
Accounts: A, B, C, D, E

Pools should control:
- membership
- routing strategy
- quota rules
- sticky behavior
- warm-up/stagger policy
- enabled models
- reserve thresholds
- cooldown/health behavior

A logical API key may have different pools for different providers.

Do not mix incompatible providers in the same routing candidate set.

## Strong API-key management

Build strong API-key management comparable to or better than codex-lb.

Dashboard must support create/edit/revoke and show the secret only once.

Support:
- expiration
- allowed models
- provider access
- request/token/cost limits where practical
- per-key usage
- last used
- account/pool routing restrictions

Most important feature:

API keys must be bindable to selected accounts or pools PER PROVIDER.

When editing a key, show separate selectors like:

Codex:
- Codex Pool A
- Codex Pool B
- individual Codex accounts

Claude:
- Claude Pool A
- Claude Pool B
- individual Claude accounts

and make the design extensible to Gemini/Grok/etc.

A key assigned to a subset MUST NEVER escape that subset during:
- retries
- failover
- 429 recovery
- sticky reassignment
- WebSocket reconnects
- model fallback
- quota exhaustion
- overload handling

Enforce this server-side by reducing the candidate set before routing.

Pool-bound keys should automatically follow pool membership changes.

## Per-account quota awareness

Add proper per-account quota tracking and display.

Use a provider-agnostic quota model rather than hardcoding the DB around Codex.

Persist generic quota windows with fields such as:
- account
- provider
- canonical window key
- upstream/raw window key
- used/remaining
- percentage used/remaining
- capacity when known
- reset_at
- observed_at
- freshness/staleness
- evidence source

Never invent quota data when upstream does not expose it.

### Codex

Implement Codex/ChatGPT quota awareness comparable to codex-lb:
- 5-hour primary window
- weekly secondary window where available
- monthly secondary where the plan reports monthly instead
- reset timestamps
- additional/model-specific quota windows where exposed
- quota history

### Claude

Use provider-native Claude quota/rate-limit/usage information already available through CLIProxyAPI/Anthropic behavior and the parsing ideas in CPA-Manager-Plus.

Do not pretend Claude has identical Codex windows.

Show unknown when reliable quota data is unavailable.

## Quota-aware routing

Port/adapt the behavior of codex-lb's quota-aware routing into CLIProxyAPI in a provider-agnostic way.

At minimum support:
- capacity_weighted
- relative_availability
- usage_weighted
- round_robin
- existing CLIProxyAPI weighted routing where useful
- fill_first
- sequential_drain
- reset_drain
- single_account

Candidate filtering should approximately be:

1. provider
2. requested model capability
3. API-key account/pool restriction
4. account enabled/paused
5. auth/health validity
6. hard continuation owner if applicable
7. quota exhaustion/hard cooldown
8. pool eligibility
9. routing scoring/strategy

Routing should consider:
- 5h quota
- weekly/monthly quota
- remaining absolute capacity when known
- reset timing
- active/in-flight requests
- recent 429/errors
- cooldowns
- optionally relative TTFT/throughput as a soft weight

Persist enough routing evidence so the dashboard can explain why an account was selected or skipped.

## Sticky sessions and hard continuation affinity

Implement codex-lb-class sticky behavior.

Explicitly separate:

Soft sticky:
- preference for the same account
- preserves provider/cache/session locality
- may move when permitted

Hard affinity:
- account ownership required by provider-side continuation state
- must not move merely because another account has more quota

For Codex, inspect current continuation/account-bound state such as:
- previous_response_id
- conversation/turn state
- uploaded/file/resource IDs
- encrypted continuation state
- any other currently account-bound upstream object

Resolve affinity using the ORIGINAL request before any later transformation.

If hard owner account is unavailable and safe replay/migration cannot be proven, return an explicit error instead of silently using another account.

Store hashes of session keys where possible rather than raw sensitive values.

## Warm-up / token warmer

This feature is required.

Implement warm-up similar to codex-lb, including both reset-confirmed warm-up and staggered idle warm-up.

### Reset-confirmed warm-up

When an opted-in account quota window is confirmed to have reset, send one small real request using a configurable cheap model/prompt.

Purpose:
- verify the account works
- initialize the new window
- discover broken auth before real traffic arrives

Persist result/time/reason.

Do not spam probes.

### Pool-aware staggered warm-up

This is a major requirement.

For each opted-in account pool, deliberately phase account primary windows so they do not all reset together.

Default spacing should derive from:

`primary_window_duration / eligible_account_count`

Example:
5-hour window + 5 eligible Codex accounts = approximately 1 hour spacing.

So the pool can resemble:

00:00 A
01:00 B
02:00 C
03:00 D
04:00 E

This should create rolling usable capacity instead of every account becoming exhausted/reset on the same boundary.

Do NOT hardcode one hour. Calculate from window duration and pool size.

Support:
- pool opt-in
- account opt-in
- manual spacing override
- small jitter
- idle threshold
- minimum cooldown
- skip unhealthy accounts
- skip accounts with recent real traffic
- no duplicate concurrent probe
- persistent schedule across restart
- deterministic phase assignment
- safe recalculation when pool membership changes
- manual "warm now"
- next planned warm-up
- last result
- dashboard timeline/history

Reset-confirmed warm-up and staggered warm-up must not fight each other.

Routing should understand quota phase/reset state.

## Usage and request observability

Use CPA-Manager-Plus as the UX baseline.

Persist/show where available:
- request time
- provider
- model requested
- model actually used
- selected account
- selected pool
- API key
- status
- retries
- error category
- input tokens
- output tokens
- reasoning tokens
- cache tokens
- total tokens
- estimated cost
- TTFT
- total latency
- routing strategy
- routing decision/reason
- quota state at dispatch
- sticky/affinity source

Use rollups/background aggregation for long-term dashboards rather than scanning all raw logs on every page load.

## Dashboard pages

The integrated CPA-Manager-Plus-style dashboard should include at minimum:

Overview
- request counts
- success/error rate
- active/limited/exhausted accounts
- token usage
- cost
- provider/model breakdown
- latency
- quota summary

Monitoring
- realtime feed
- persistent request history
- strong filters
- routing/account/pool/API-key visibility
- redacted failure evidence

Usage Analytics
- provider/model/account/pool/API-key breakdown
- tokens/cost/failures/latency
- time-range filters

Accounts
- provider
- account identity
- plan/workspace metadata
- health
- quota windows and resets
- assigned pools
- warm-up enabled
- next/last warm-up
- pause/resume
- re-auth
- quota inspection

API Keys
- create/edit/revoke
- provider restrictions
- Codex account/pool selection
- Claude account/pool selection
- model/usage limits
- usage

Pools
- membership
- provider
- routing strategy
- quota state
- aggregate pool health
- warm-up/stagger configuration and timeline

Routing Settings
- strategy
- sticky thresholds
- quota thresholds
- cooldowns
- overload/error weighting
- reset preference

Settings
- auth/security
- retention
- database maintenance
- pricing
- provider settings
- backups

## Codex and Claude auth UX

Keep CLIProxyAPI's working provider auth implementation.

Do not replace working OAuth logic unnecessarily.

Adapt useful codex-lb behavior for:
- stable identity
- re-auth without losing metadata/pool assignment
- quota refresh
- health
- simple dashboard add/re-auth flow

Do the equivalent for Claude using CLIProxyAPI's existing Claude OAuth support.

## Security

Treat the management panel as Internet-exposed software.

Require separate dashboard/admin authentication.

Do not use inference API keys as admin credentials.

Do not expose OAuth refresh/access tokens via dashboard APIs.

Redact secrets in logs.

Show downstream API keys once at creation.

Enforce account/pool bindings server-side.

Add audit events for important management mutations.

## Backward compatibility

Existing CLIProxyAPI deployments should remain usable.

Keep existing YAML/config behavior where possible.

Keep existing auth files.

On first startup:
- discover existing auth files
- assign/import stable logical account identities
- do not destructively move credentials
- create sensible implicit/default provider pools if necessary

Do not silently invalidate existing clients.

## Docker/build

Provide a clean deployment.

Preferred:
- multi-stage dashboard build
- Go build
- final runtime
- embedded/static dashboard served by CLIProxyAPI
- persistent /data-style volume for SQLite/state/auth as appropriate
- healthcheck
- backup instructions

No Headroom service.

## Testing

Before major routing modifications, add regression coverage around existing CLIProxyAPI translation/executor behavior.

Then test at minimum:

API-key binding:
- key bound to A cannot use B
- retry/failover cannot escape scope
- sticky reassignment cannot escape scope
- pool membership update changes eligible set

Quota:
- exhausted account excluded
- stale evidence handled
- 5h + weekly/monthly handling
- capacity-aware selection
- reset timing
- in-flight pressure

Sticky:
- same session prefers same account
- soft sticky may reallocate
- hard continuation stays on owner
- unavailable owner fails explicitly
- raw sensitive session IDs not persisted unnecessarily

Warm-up:
- deterministic stagger
- 5 accounts / 5h ≈ 1h phases
- restart does not duplicate probes
- cooldown respected
- recent traffic suppresses idle probe
- reset and stagger modes do not conflict
- pool membership changes reschedule safely

SQLite:
- migrations
- WAL/concurrency behavior
- restart persistence
- backup before migration
- dashboard query performance

Streaming/WebSocket:
- disconnect cleanup
- retries
- no leaked in-flight reservations
- no duplicate usage settlement

## Performance

Do not make the request hot path perform excessive SQLite queries.

Maintain efficient in-memory snapshots/caches for:
- account metadata
- API-key scope
- pool membership
- latest quota state
- health
- routing settings

Use explicit invalidation/update after management mutations.

Telemetry may be batched.

Avoid global locks around streaming requests.

## Implementation process

First inspect the local target and both local reference repositories.

Write a short architecture note inside the target repo:
`docs/control-plane-architecture.md`

Then implement in vertical slices, keeping the project compiling/testable:

1. SQLite + stable account identity + dashboard foundation
2. persistent request/usage observability
3. account pools + API-key bindings
4. provider quota adapters/history
5. quota-aware routing
6. sticky + hard affinity
7. warm-up/stagger scheduler
8. dashboard completion/polish
9. security/backward compatibility/performance hardening

Do not add fake UI controls without backend enforcement.

Do not stop after scaffolding.

## Definition of done

The finished fork should combine:

- CLIProxyAPI's existing provider/protocol compatibility
- the actual CPA-Manager-Plus dashboard experience integrated into the fork
- SQLite persistence
- per-account quota visibility
- Codex 5h + weekly/monthly awareness
- provider-native Claude quota awareness where available
- codex-lb-class quota-aware routing
- account pools
- API-key-to-account/pool binding per provider
- advanced sticky sessions
- hard continuation ownership
- reset-confirmed warm-up
- pool-aware staggered token warmer
- persistent request/usage analytics
- Docker deployment
- backward compatibility
- tests and documentation

Again: NO Headroom integration.

When finished, report exactly what was implemented, which files/modules changed, migrations/schema added, routing strategies available, quota providers supported, warm-up behavior, dashboard pages, tests run/results, and any remaining limitations.