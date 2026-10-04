# Residual frontend lint diagnostics

Final run on 2026-10-03 from `web`:

```sh
npm run lint -- --format json --output-file /tmp/opencode/cpa-followup-lint-final.json
```

Exit status: **1**, with **189 errors / 1 warning across 44 files**. Full lint is
not green. `npx eslint src/controlplane`, typecheck, production build and the
full 263-file/4,215-test frontend suite passed. Rules and file coverage remain
unchanged. The earlier 196-error/4-warning inventory describes a historical run.

| Severity | Rule | Messages |
| --- | --- | ---: |
| Error | `react-hooks/set-state-in-effect` | 99 |
| Error | `react-hooks/refs` | 87 |
| Error | `react-hooks/preserve-manual-memoization` | 2 |
| Error | `react-hooks/purity` | 1 |
| Warning | `react-refresh/only-export-components` | 1 |

Byte comparison against `/root/cpa/reference/CPA-Manager-Plus/apps/web` confirms
42 diagnostic files match the reference. Two adapted diagnostic files retain
reference debt:

- `ServerCodexInspectionPage.tsx`: moved the timezone expression inside `useMemo`;
  original clean-reference lint reports 9 errors, including `react-hooks/use-memo`.
  The adapted file reports 8 remaining errors.
- `UsageMaintenancePage.tsx`: moved state declarations before their use;
  clean-reference lint reports 14 errors, including immutability/memoization
  diagnostics. The adapted file reports 9 remaining errors.

Clean-reference reproduction uses the same ESLint config and installed tools,
with untouched copies of these two reference files:
`/tmp/opencode/cpa-followup-reference-lint.json`. It reports 23 errors, establishing
the original debt without asserting that the adapted files are byte-identical.
Three test callbacks in `AccountsPage.test.tsx` now use inferred parameter types;
the focused 357-test suite and typecheck passed. Broad hook/state rewrites are
outside these safe lint fixes.

## Exact residual locations

Paths are relative to `web/src`. Each list preserves all message locations as
`line:column`, including repeated locations. Rule abbreviations below expand to
the exact names in the table above: **S** = set-state-in-effect, **R** = refs,
**M** = preserve-manual-memoization, **P** = purity, **W** = only-export-components.
S/R/M/P are errors; W is a warning. Counts include duplicates.

| File | Rule: locations |
| --- | --- |
| `components/common/DatabaseMaintenanceContext.tsx` | S: 65:5, 78:10 |
| `components/providers/ProviderEditDrawer/ClaudeEditDrawer.tsx` | S: 210:5, 240:7, 454:5, 463:5 |
| `components/providers/ProviderEditDrawer/CodexEditDrawer.tsx` | S: 187:5, 212:5, 228:7, 757:5, 768:5 |
| `components/providers/ProviderEditDrawer/GeminiEditDrawer.tsx` | S: 155:5, 191:7, 424:5, 435:5 |
| `components/providers/ProviderEditDrawer/OpenAIEditDrawer.tsx` | S: 226:5, 266:7, 291:7, 364:5, 446:5; M: 651:34 |
| `components/providers/ProviderEditDrawer/VertexEditDrawer.tsx` | S: 100:5, 133:7 |
| `components/providers/ProviderHealthCheckDrawer/ProviderHealthCheckDrawer.tsx` | S: 94:7 |
| `components/providers/hooks/useProviderRecentRequests.ts` | S: 166:7 |
| `features/accounts/AccountsPage.tsx` | R: 1279:52, 1399:5, 1406:5, 1424:3, 1439:5, 1443:5, 1446:5, 1449:5, 1451:48, 1453:5, 1456:5, 1463:40, 1465:5, 1504:44, 1543:5, 1549:5, 1591:3, 1661:3, 1663:5, 1666:5, 3084:3, 3111:3, 3151:3, 3218:3, 3237:3, 3243:3, 3559:3, 10272:8; S: 1808:5, 1839:5, 2394:5, 3240:29, 5023:5, 5520:7, 5530:5, 5785:5 |
| `features/accounts/components/AccountHealthBadge.tsx` | W: 73:14 |
| `features/accounts/hooks/useCredentialInspectionSnapshot.ts` | R: 63:3; S: 173:5 |
| `features/aiProviders/AiProvidersClaudeEditLayout.tsx` | S: 257:7 |
| `features/aiProviders/AiProvidersClaudeModelsPage.tsx` | S: 113:5, 155:5 |
| `features/aiProviders/AiProvidersCodexEditPage.tsx` | S: 199:5, 234:7, 543:7, 587:5 |
| `features/aiProviders/AiProvidersGeminiEditPage.tsx` | S: 178:5, 216:7, 360:7, 407:5 |
| `features/aiProviders/AiProvidersOpenAIEditLayout.tsx` | S: 306:7; M: 514:34 |
| `features/aiProviders/AiProvidersOpenAIModelsPage.tsx` | S: 111:5, 121:5 |
| `features/aiProviders/AiProvidersPage.tsx` | S: 204:14, 211:32, 288:5, 317:5, 322:5 |
| `features/aiProviders/AiProvidersVertexEditPage.tsx` | S: 154:5, 196:7 |
| `features/authFiles/components/OAuthEditorModals.tsx` | S: 238:5, 262:5, 560:5, 583:5 |
| `features/authFiles/hooks/useAuthFileConfigurationEditor.ts` | R: 103:3, 106:3 |
| `features/config/ConfigPage.tsx` | S: 497:7, 526:5, 864:5, 874:10 |
| `features/config/components/AccountProcessingPolicySection.tsx` | S: 79:10 |
| `features/dashboard/DashboardPage.tsx` | S: 327:23, 331:10 |
| `features/dashboard/components/VersionCard.tsx` | S: 198:7 |
| `features/dashboard/hooks/useDashboardUsageSummary.ts` | S: 83:10 |
| `features/login/LoginPage.tsx` | S: 329:7 |
| `features/logs/LogsPage.tsx` | S: 319:5, 324:7, 341:10; R: 439:3, 771:22, 773:27, 775:18, 775:18 |
| `features/monitoring/AccountActionCandidatesPage.tsx` | S: 101:10 |
| `features/monitoring/CodexInspectionPage.tsx` | R: 124:26, 134:58, 135:54, 137:57, 140:77, 146:5, 149:5, 152:66, 161:32, 161:32, 161:32, 185:3; S: 220:5, 852:5, 857:5 |
| `features/monitoring/ModelPricesPage.tsx` | R: 93:40, 95:5; S: 139:7 |
| `features/monitoring/MonitoringCenterPage.tsx` | R: 245:5, 250:5, 253:5, 256:5, 259:5, 262:5, 265:5, 296:5, 299:5, 302:5, 305:5, 308:5, 311:5, 314:5, 317:5, 319:41, 321:5, 324:5, 327:5, 330:5, 347:5, 350:5, 353:5, 356:5, 358:62, 362:70, 368:5, 372:5; P: 892:70; S: 1007:7, 1027:5 |
| `features/monitoring/ServerCodexInspectionPage.tsx` | R: 800:3, 971:7, 974:25; S: 810:5, 937:7, 983:5, 1035:5, 1040:5 |
| `features/monitoring/components/MonitoringShared.tsx` | S: 229:5 |
| `features/monitoring/hooks/useHeaderSnapshotsLoader.ts` | R: 40:3, 41:3 |
| `features/monitoring/hooks/useMonitoringAnalytics.ts` | S: 248:10 |
| `features/monitoring/hooks/useUsageData.ts` | S: 245:10 |
| `features/oauth/OAuthPage.tsx` | R: 319:3 |
| `features/plugins/PluginResourcePage.tsx` | S: 86:10 |
| `features/plugins/PluginStorePage.tsx` | S: 565:10 |
| `features/plugins/PluginsPage.tsx` | S: 247:10 |
| `features/system/ManagerUpdates.tsx` | S: 73:7, 144:5 |
| `features/system/SystemPage.tsx` | S: 256:5, 262:10 |
| `features/usage-maintenance/UsageMaintenancePage.tsx` | S: 436:40, 646:5, 752:10, 766:5, 890:5, 951:5; R: 906:17, 906:17, 1663:40 |
