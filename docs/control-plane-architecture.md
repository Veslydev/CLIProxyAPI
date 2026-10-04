# Built-in control plane

The proxy remains the execution and translation engine. The optional `management.control-plane`
configuration enables a SQLite database and a CPA-Manager-Plus-derived dashboard at
`/dashboard`. New admin endpoints live at `/api/control-plane`; deprecated management
endpoints remain unchanged. Dashboard authentication uses a separate environment
secret, never a downstream inference key.

SQLite owns logical accounts, credential references, provider-specific pools, hashed
downstream keys, quotas/history, affinity, warm schedules, request telemetry, rollups,
settings and audit records. OAuth secrets remain in the existing auth store. Schema
upgrades run transactionally, with an online SQLite backup before upgrading an
existing schema. WAL, foreign keys, busy timeout and full synchronization apply.

A candidate policy runs before native/plugin scheduling. It reduces the candidate
set for every selection attempt, including retries, and reads original request state.
The policy is independent of selector replacement during configuration reload.
Account, pool, key and quota snapshots are published after durable mutations. Existing
health/model eligibility remains authoritative. Home dispatch and this local control
plane are mutually exclusive: neither may bypass the other's authorization.

Usage is consumed from the existing runtime usage publisher, with idempotent request
IDs and transactional rollups. Sensitive request bodies, session identifiers, upstream
headers and auth metadata are not exposed by control-plane APIs.

Warm-up is opt-in and uses existing executors with an account-pinned candidate set.
Schedules use deterministic phases, pool-size-derived spacing, persistent claims and
cooldowns. A claimed probe is not automatically replayed after a process crash.

## Follow-up boundaries (schema v4)

- Schedule entities are keyed by a hash of pool ID, NUL and account ID. Each pool
  retains its model, prompt, duration, spacing and phase. Separate `warm_account`
  entities own the account-wide claim, last probe, reset watermark and captured
  cooldown. There are not separate upstream windows for overlapping memberships.
- Planning reads the snapshot inside the serialized mutation. Claiming revalidates
  eligibility, policy fingerprint and deterministic arbitration, then captures the
  provider/model/prompt. A later policy edit cannot replace an already-claimed
  dispatch; a stale completion cannot release another claim.
- Earliest eligible slot wins; ties prefer manual, confirmed reset, then stable pool
  ID. The longest cooldown of enabled member pools is captured when claiming. A
  completed/interrupted probe updates competing schedules as shared evidence.
  Opt-out, missing quota duration and membership edits retain inactive/suspended
  rows and the global cooldown instead of deleting lifecycle history.
- Reset dispatch is `phased` by default; opt-in `immediate` runs on the first worker
  pass after confirmation and eligibility, not synchronously at quota ingestion.
  Both modes respect cooldown and recent real traffic. Native quota/reset evidence
  remains distinct from a planned slot.
- Optional `phase_preference` is validated in 0–0.05, defaults off and only applies
  to capacity/usage/reset scoring. A fresh, policy-matching selected-pool schedule
  and known quota are required. It never creates quota or bypasses filtering,
  health, scope or hard ownership.
- Target-bound Codex/Claude OAuth sessions validate fresh native principal/workspace
  before metadata merge, hooks and token-store persistence. The one-time completion
  guard expires after five minutes and linearizes saving against cancellation.
  Cancellation cannot promise rollback once saving begins. Identity-less providers
  still require separately imported credentials and audited operator reassociation.
- Per-attempt execution journaling is independent of an account lease: stream
  attempt IDs are created after acquisition. The SDK completion observer closes all
  attempt journals when the stream releases. Published usage atomically settles
  the journal; release/restart without publication remains unresolved, never guessed.
  A journal write failure gates later admission, not the attempt already starting.

The v4 transaction migrates schedule keys/guards, adds pool-tagged warm history and
an indexed execution journal. Existing databases retain a private online pre-upgrade
backup. Older warm runs have an unknown pool (`""`), not fabricated provenance.
Rollback requires an older compatible binary with the pre-upgrade backup; restoring
that snapshot loses post-upgrade mutations. Preserve current DB/WAL/SHM separately.

Token/cost ceilings remain post-settlement admission controls; in-flight thresholds
remain selection pressure, not semaphores. There is no proven generic upper bound
for provider reasoning, tool/image usage or invoice cost, and no verified per-attempt
provider billing reconciliation API. The journal reports the gap without inventing
reservations or recovered tokens. See the [acceptance audit](control-plane-acceptance.md),
[requirement/parity matrix](control-plane-followup-matrix.md) and
[compatibility matrix](control-plane-compatibility.md).

The frontend adapts the actual CPA-Manager-Plus source, UI primitives, branding and
styles. It has a same-origin API client rather than a second manager service. The
production single-file bundle is served by Go; Docker builds it in a Node stage.
