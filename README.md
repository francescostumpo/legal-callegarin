# legal-callegarin

Modular Go monolith for the Studio Legale Alessandro Callegarin website. Go is
the only runtime process; Node.js is used only to build and test the embedded
React administration application.

## Toolchains

- Go 1.27.1, declared in `go.mod`. With `GOTOOLCHAIN=auto` (the Go default), an
  older local Go command downloads and uses the declared toolchain.
- Node.js 24, declared in the root `package.json`.
- npm, using the single root `package-lock.json` for all workspaces.
- Staticcheck is pinned as a Go tool dependency and runs through `go tool`.

## Setup and validation

Install the frontend dependencies from the lockfile and run every repository
check:

```sh
npm ci
make check
```

`make check` runs the fail-on-difference Go formatting check, `go vet`,
Staticcheck, all Go tests, the foundation configuration and clean-build checks,
a clean npm install, the fail-on-difference frontend formatting check,
TypeScript checks, frontend tests, and the production frontend build in that
order.

Build the React bundle first and then the single Go executable:

```sh
make build
```

The executable is written to `bin/legal-callegarin`. Generated files under
`bin/` and `internal/webassets/admin/dist/` are intentionally ignored.

## Local process

Development uses explicit local-memory configuration:

```sh
APP_ENV=development go run ./cmd/web
```

The server listens on `:8080` by default. `GET /health/live` reports process
liveness and `GET /health/ready` checks the configured storage dependencies
with a short deadline.

`APP_ENV` must be `development`, `test`, or `production`. Test configuration
must provide `SESSION_KEY_BASE64`. Production additionally requires
`PUBLIC_BASE_URL`, Azure storage mode and account URL, `ADMIN_USERNAME`,
`ADMIN_PASSWORD_HASH`, and a base64 session key that decodes to at least 32
bytes. No literal session-key fallback is accepted outside development.
`TRUSTED_PROXY_HOPS` defaults to `0` and accepts only canonical integer values
from 0–3. In production behind Azure Container Apps, it must be exactly `1` so
only the single platform proxy hop is trusted. The obsolete `TRUSTED_PROXY`
variable is rejected.

Generate the administrator password hash offline with `go run ./cmd/adminhash`.
See [administrator password recovery](docs/password-recovery.md) for the safe
generation, Azure secret rotation, session invalidation, and verification
procedure.

Azure mode uses `AZURE_STORAGE_ACCOUNT_URL` with a canonical
`https://<account>.blob.core.windows.net` origin and `DefaultAzureCredential`.
`AZURE_STORAGE_CONNECTION_STRING` is limited to development/test (for example,
Azurite). The former `AZURE_ACCOUNT_URL` name is rejected explicitly. Storage
connection strings and credentials must never be logged.

Production startup is non-provisioning. Azure defaults
`ARTICLE_STORAGE_SCHEMA_MODE` to `compat`: dual-reader/current-writer serving
with no startup migration. The approved two-stage rollout deploys one immutable
GHCR digest to every active revision in `compat`, then switches that same
artifact to `migrate`; migration converges legacy rows and creates/checks the
durable marker before the HTTP listener starts. Normal post-migration startup
uses the marker fast path and a current-only O(page) repository. `repair`
forces an idempotent convergence scan after writers are quiesced and does not
delete the marker. See [the article storage rollout runbook](docs/article-storage-rollout.md).

The deployment Bicep remains responsible for creating the `articles`,
`contacts`, and `sessions` tables and the private `article-bodies` container
before the application revision starts. The connection-string development
path provisions those resources explicitly for Azurite/local tests. Setting
`ARTICLE_STORAGE_SCHEMA_MODE` with memory storage is rejected.

## Azure deployment orientation

Keep infrastructure bootstrap, the one-time custom-domain operation, and
routine CI application rollouts separate. Start with the
[Azure operations runbook](docs/operations.md), copy the reviewed
[main parameter example](infra/main.example.bicepparam) to an ignored local
file for bootstrap, and use the
[custom-domain parameter example](infra/custom-domain.example.bicepparam) only
for the DNS-verified domain phase. Routine CI rollouts are not full
infrastructure deployments.
