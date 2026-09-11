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
Staticcheck, all Go tests, a clean npm install, TypeScript checks, frontend
tests, and the production frontend build in that order.

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
liveness; other routes remain uninitialized in this foundation task.

`APP_ENV` must be `development`, `test`, or `production`. Test configuration
must provide `SESSION_KEY_BASE64`. Production additionally requires
`PUBLIC_BASE_URL`, Azure storage mode and account URL, `ADMIN_USERNAME`,
`ADMIN_PASSWORD_HASH`, and a base64 session key that decodes to at least 32
bytes. No literal session-key fallback is accepted outside development.
