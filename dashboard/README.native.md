# Go/SQLite dashboard integration

This directory contains the actual UI from
https://github.com/itsmylife44/cliproxyapi-dashboard at commit
`b062653e34d94644b73e3b7bac85c87a92607c0b`. `UPSTREAM.json` records the
SHA-256 hashes of the imported source. `LICENSE.upstream` contains the MIT
copyright notice. The original manifests remain as `package.upstream.json`
and `package-lock.upstream.json`; `src/app/dashboard/page.upstream.tsx` retains
the original server-rendered home page.

## Build and checks

Use Node 22.12+ and npm:

```sh
npm ci --no-audit --no-fund
npm run typecheck
npm test
npm run lint
npm run build
```

The native build uses Vite and produces one `dist/index.html`, including fonts
and images. The parent Dockerfile builds both this dashboard and `../web`.
`npm run lint` checks the native adapter; it does not claim that the imported
Next.js server source passes lint. The original Next.js tests and Prisma
commands belong to the external stack, not this runtime.

## Runtime boundary

- Go serves this UI at `/dashboard` and its deep links. It serves the existing
  native Operations bundle at `/control-plane`.
- `native/main.tsx` imports upstream pages and the original client shell.
  `native/` supplies navigation, image, dynamic and i18n adapters.
- `native/api.ts` translates the original browser API contract into authenticated
  Go control-plane calls. The browser stores the admin credential only in memory.
  Reload or sign-out clears the session; SWR caches belong to that session.
- Providers use native v8 OAuth dispatch and enabled registered plugins. Missing
  plugins remain disabled. The UI cancels waiters when a modal closes, navigation
  abandons a launch or the session ends. Cancellation cannot undo a credential
  save that has already begun.
- Upstream-key reads return hashes and masks. Writes use a locked Go
  read/modify/save operation and restore the handler configuration on save failure.
- Inference-key creation requires explicit account or pool bindings. The backend
  validates scope. Only the creation response contains the plaintext inference key.
- Quota shows native evidence and freshness. Unobserved fractions remain unknown;
  stale snapshots do not contribute to current capacity summaries. Aggregated
  capacity retains the upstream UI's combination formula, not an additive token
  allowance or a provider guarantee.
- Usage totals and daily charts use SQLite rollups. Key/model breakdowns use a
  bounded first page. Missing input/output/cache breakdowns and billing evidence
  remain unavailable. Operations retains paginated request and lifecycle evidence.

The runtime excludes the imported Next.js API routes, PostgreSQL/Prisma users,
Docker administration, Telegram, sharing, Perplexity sidecar and update controls.
Keep their source for provenance, but do not run the imported Dockerfile,
development compose stack or migration scripts to manage this fork.

Settings, pools, routing, targeted re-auth, history and backups remain in
Operations. Both bundles share the Go backend and SQLite state. Operations has
its own memory-only admin login; the dashboard does not place secrets in iframe
URLs or browser storage.
