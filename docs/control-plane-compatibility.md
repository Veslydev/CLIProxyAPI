# Provider, model and protocol coverage

Current completion evidence: 2026-10-03. The full Go suite passed after the
follow-up changes. The files below locate deterministic source tests in that
suite; they do not certify real provider behavior. Opt-in live tests may skip
without credentials. This follow-up used synthetic accounts and no provider
requests; the live column cites only the historical authorized Codex record in
[verification](control-plane-verification.md).

Executor paths below are relative to `internal/runtime/executor/`.

| Provider / model scope | Preserved entry/wire protocols and source | Local fixture evidence, including tools/multimodal/streaming | Live acceptance |
| --- | --- | --- | --- |
| OpenAI-compatible endpoints / configured aliases | Chat Completions and Responses, `openai_compat_executor.go`, `openai_responses_signature.go`; existing translator registry | `openai_compat_executor_retry_test.go`, `openai_compat_executor_tool_results_test.go`, image/video/reasoning/max-token/compact tests and translator suite | No new live OpenAI API-key test; model-by-model availability unverified |
| Codex / registered GPT aliases | Responses HTTP/SSE and native WS/duplex, translated Chat; `codex_executor*.go`, `codex_websockets*.go` | `codex_native_fidelity_test.go`, tool schema/parallel tools/spawn-agent/imagegen/image extraction, bootstrap/retry/chunk ownership/duplex tests; control-plane original ownership, disconnect and WS reconnect fixtures | Historical authorized `gpt-6-luna`: short Responses non-stream and Chat SSE, WHAM quotas, manual warm and exact usage counters. Tools, images/audio/video, WS/duplex and other models not live verified |
| Anthropic/Claude / configured Claude models | Messages, translated Chat/Responses; `claude_executor*.go`, existing Claude OAuth | `claude_executor_ratelimit_test.go`, `claude_cloaked_cache_repro_test.go` including tool continuation/streaming; synchronized shared-auth race regression; control-plane native quota/header/OAuth JSON parser fixtures | Blocked: no authorized Claude account. OAuth completion, tools/multimodal/stream and native window behavior lack live evidence |
| Gemini API / Gemini CLI / AI Studio / Vertex / Antigravity | GenerateContent and Interactions, translated Chat/Responses/Claude; Gemini/Vertex/Antigravity executors | `gemini_executor_test.go` Interactions entry conversions, tool signatures and stream usage; signature/Vertex/Interactions translation tests; Antigravity compaction/replay and auth-manager credits fallback fixtures | No authorized live test; model/plan/channel availability and multi-modal combinations unverified |
| Grok/xAI / configured Grok aliases | Responses HTTP/SSE and WS; `xai_executor*.go`, `xai_websockets_executor*.go` | `xai_executor_test.go` namespace/custom tools, native image-generation normalization, composer isolation and stream filtering; `xai_websockets_executor_test.go`; Codex-compatible XAI keepalive fixtures | No authorized live test; fixture image-tool shaping is not generated-image verification |
| Kimi / configured Kimi aliases | Responses passthrough, Chat/Claude-compatible paths; `kimi_executor*.go` | `kimi_executor_test.go` Responses/stream, tool-history linkage, schema normalization, thinking, refresh, apply-patch; unsupported Responses compact returns explicit not-implemented | No authorized live test; multimodal and all scoped fallback combinations unverified |
| Muse/Meta / configured Muse aliases | Existing Meta executor and auth integration; `meta_executor*.go`, `internal/auth/meta`, `sdk/auth/meta.go` | `meta_executor_test.go`, `helps/meta_tools_test.go`, Meta auth/config/refresh tests; no new control-plane-specific Meta transport regression | No authorized live test; exhaustive tools/multimodal/stream/WS and scoped recovery coverage partial |
| Devin / account-advertised SWE/GPT/Claude aliases | Existing Interactions/frame execution and translated Chat/Claude/Responses; `devin_executor*.go` | `devin_executor_test.go` image supplementation, tool result translation, sequential/interleaved stream frames, late signatures, refresh and tool-call limits | No authorized live test; registry aliases do not prove account entitlement |
| Plugin providers | Existing delegated scheduler/executor path plus candidate-policy reduction | `internal/controlplane/runtime_test.go:TestRuntimeRetryAndPluginDelegateCannotEscapeScope`, plugin usage helpers | Arbitrary third-party plugins and live protocol-specific scope recovery not certified |

Model names come from configured aliases and the runtime registry, not a new
control-plane hardcoded catalog. The only model with historical live inference
evidence here is `gpt-6-luna`. Synthetic `model` and `gpt-test` IDs prove local
dispatch/accounting contracts. A successful model listing does not prove tools,
multimodal support or entitlement for each listed model.

## Cross-cutting control-plane evidence

| Contract | Executed deterministic coverage | Remaining gap |
| --- | --- | --- |
| Retry/429/failover/plugin scope | `internal/controlplane/runtime_test.go` HTTP and stream retries with failed scoped account; `controlplane_test.go:TestKeyScopeFailoverPoolUpdateAndRevocation`; common candidate filtering before native/plugin selection | Partial dedicated matrix for model fallback and provider-specific 429/credits fallback under a control-plane scoped key. Native SDK fallback tests alone do not prove control-plane scope for each protocol |
| Soft sticky and unavailable owner | `controlplane_test.go:TestSoftStickyHardAffinityAndSensitiveHashes`; `routing_security_test.go` sticky threshold, cross-provider and single-account restart cases | Live cache locality/performance unverified |
| Original request ownership | `routing_security_test.go:TestOriginalContinuationObjectsRetainOwnerAcrossTransformedPayloadAndScope` checks response, conversation, file, resource and encrypted state; cross-scope rejection and hashed storage | Provider-owned turn state unverified. Runtime passthrough client `turn_id` is not proof of upstream ownership; no arbitrary hard-affinity promotion |
| HTTP streaming/cancellation | `runtime_test.go:TestRuntimeStreamDisconnectReleasesInFlightAndPersistsOwner`; `internal/api/server_controlplane_load_test.go` 64 SSE attempts / 16 concurrent, 16 intentional disconnects | Synthetic executor; no upstream disconnect billing guarantee or streaming SQLite wait histogram |
| WebSocket reconnect | `TestControlPlaneWebsocketReconnectKeepsScopeAndRejectsUnreplayableContinuation` uses real local WS transport and fake SSE-backed executor; two fresh calls retain scope; unavailable previous response returns explicit 409 `previous_response_not_found` | Does not prove cross-connection provider continuation replay or native upstream WS billing |
| Exactly-once published usage | `telemetry.go`/settlement-ID retention; `executions_test.go`; SDK synchronous usage tests; `helps/usage_execution_identity_test.go`; Codex base-before-image usage regression | Crash/release without publication remains unresolved. No invoice-grade recovery from guessed tokens |
| Codex quota windows | `controlplane_test.go:TestProviderQuotaAdapters`, `helps/codex_quota_test.go` native 5h/weekly/monthly/additional evidence and incomplete-window rejection | Historical live Plus 5h/weekly only; actual monthly/additional transitions not live tested |
| Claude quota windows | `routing_security_test.go:TestClaudeQuotaCanonicalKeysAndModelIsolation`, native rate-limit fixtures, `quota.go` OAuth usage parser | Live headers/usage/plan/model/reset evidence blocked |
| Identity and OAuth continuity | `reauth_guard_test.go` principal/workspace mismatch and changed-reference scope/history/owner preservation; management guard replay/concurrent/expiry/cancel/wrong-state tests; frontend targeted re-auth/cancel/sign-out regressions | Actual token exchange/re-login/rotation requires separate authorization; identity-less fallback uses audited operator assertion |
| Journal/restart | `executions_test.go` unresolved/late trustworthy settlement/retention; repeated HTTP stream race fixture checks settled/unresolved rows and zero leaked flights | Latest-100 unresolved API is not a full recovery export; journal observation can precede any actual upstream call |

## Blocked and unverified acceptance

- Live Claude needs secure account placement and explicit authorization; do not put
  secrets in chat. No unrelated credentials may substitute for it.
- Additional Codex requests require a stated target, request/token budget,
  duration, rotation risk and cleanup. Interactive OAuth/rotation needs explicit
  permission. The supplied account was not read or used during this follow-up.
- Real reset/stagger acceptance needs elapsed time and multiple separately
  authorized accounts. Controlled clocks prove schedule/arbitration only; the
  proxy cannot move fixed or already-active provider windows.
- Billing comparison needs authorized billing evidence. No verified generic
  per-attempt reconciliation API or safe upper token/cost reservation bound
  currently supports exact ceilings or automatic recovery.
- Dedicated protocol-specific scoped fallback coverage and native provider-owned
  turn ownership remain partial. Full lint remains failing; see
  [the residual inventory](control-plane-lint.md).
