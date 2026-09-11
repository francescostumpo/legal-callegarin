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
`TRUSTED_PROXY` defaults to `false` and accepts only the exact values `true` or
`false`; enable it only when every direct request reaches the application
through a trusted reverse proxy that replaces forwarding headers.

Generate the administrator password hash offline with `go run ./cmd/adminhash`.
See [administrator password recovery](docs/password-recovery.md) for the safe
generation, Azure secret rotation, session invalidation, and verification
procedure.

Azure mode uses `AZURE_STORAGE_ACCOUNT_URL` with a canonical
`https://<account>.blob.core.windows.net` origin and `DefaultAzureCredential`.
`AZURE_STORAGE_CONNECTION_STRING` is limited to development/test (for example,
Azurite). The former `AZURE_ACCOUNT_URL` name is rejected explicitly. Storage
connection strings and credentials must never be logged.

Production startup is non-provisioning: it constructs clients and starts even
when Azure Storage is temporarily unavailable; readiness then reports `503`
while liveness remains `200`. The deployment Bicep is responsible for creating
the `articles`, `contacts`, and `sessions` tables and the private
`article-bodies` container before the application revision starts. The explicit
connection-string development path provisions those resources for Azurite and
local tests only.
