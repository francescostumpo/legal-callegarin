# Final remediation implementation report

Date: 2026-09-22

Branch: `codex/website-v1`

Base commit: `9717e0a test: isolate browser e2e runs`

## Implemented scope

1. `make check` now runs deterministic frontend installation, checks, tests, and production build before Node contracts and every Go command that consumes embedded frontend assets. README and the development guide describe the same order, and Node contract tests enforce it.
2. Cached public HTML now uses a stable weak ETag over the nonce-placeholder representation. Conditional matching uses weak entity-tag comparison. The exact cached JSON-LD inline bytes are SHA-256/Base64 authorized in `script-src` on both 200 and 304 responses while the per-response nonce remains in place. The shared sitemap/robots revalidation path remains strong.
3. A saturated fail-closed username bucket set now denies every key after idle pruning, including the pinned/configured key, until an idle non-pinned entry expires and frees a slot. Address-set eviction and production limiter capacities are unchanged.
4. The Azure operation executor default per-attempt timeout is five seconds. Retry attempts and delay tuning are unchanged.

## Changed files

- `Makefile`
- `README.md`
- `docs/development-and-first-release.md`
- `docs/superpowers/plans/2026-09-22-final-remediation.md`
- `scripts/foundation-config.test.mjs`
- `scripts/development-release-docs.test.mjs`
- `internal/web/public/articles.go`
- `internal/web/public/revalidation_internal_test.go`
- `internal/app/app_test.go`
- `internal/web/adminapi/rate_limit.go`
- `internal/web/adminapi/handler_test.go`
- `internal/storage/azure/retry.go`
- `internal/storage/azure/retry_test.go`
- `.superpowers/sdd/final-remediation-report.md`

No public copy, privacy/consent constant, storage schema, infrastructure/deploy source, dependency manifest, or admin UI source was changed. `internal/web/public/contact.go` still declares `contactConsentVersion = "privacy-v1-2026-09-11"`.

## TDD evidence

### Clean-check ordering

- RED: `node scripts/foundation-config.test.mjs` failed because Go validation appeared before the frontend build.
- GREEN: `node scripts/foundation-config.test.mjs` passed 19/19 and `node scripts/development-release-docs.test.mjs` passed 11/11 after the recipe/docs update.

### HTML ETag and CSP

- RED: `go test ./internal/web/public ./internal/app -run 'CachedPublicHTML|ETagMatches' -count=1` exposed strong HTML ETags and asymmetric weak matching.
- GREEN: the same focused command passed for public and app after implementation. A later final focused run also passed after adding malformed-wildcard rejection.
- Full-app coverage includes `/`, `/sentenze-e-riflessioni`, and a published detail route, with two nonce-varying 200 bodies, equal weak ETags, bodyless 304, and an exact JSON-LD hash in both 200/304 CSP.

### Username saturation

- RED: the requested pattern reproduced the oracle: configured username returned 401 while an unseen username returned 429 at map capacity; the pinned-only minimum-capacity case also remained open.
- GREEN: `go test ./internal/web/adminapi -run 'LoginLimiter|LoginPOSTUniform|Satur' -count=1` passed after the fail-closed admission check. The handler regression uses `MaxBuckets=2` and three distinct client prefixes.

### Azure timeout

- RED: `go test ./internal/storage/azure -run 'Executor' -count=1` reported `default timeout = 3s, want 5s`.
- GREEN: the same command passed after changing only `defaultTimeout`.

## Aggregate validation evidence

Passed:

- `go test ./internal/web/public ./internal/app -count=1`
- `go test ./internal/web/adminapi -run 'LoginLimiter|LoginPOSTUniform|Satur' -count=1`
- `go test ./internal/storage/azure -run 'Executor' -count=1`
- `node scripts/foundation-config.test.mjs && node scripts/development-release-docs.test.mjs`
- `go tool staticcheck ./internal/web/public` with task-local cache directories
- Clean-check simulation: existing `node_modules` and `internal/webassets/admin/dist` were moved to RAM-backed temporary storage, their absence was asserted, and `make check` returned status 0. The trap reported `restored-node-modules=yes restored-admin-dist=yes`. This run included 48 frontend tests, 219 Node contract tests, vet, Staticcheck, all default-tag Go tests, and `check-clean-build.sh`.
- `go test -race ./internal/web/public ./internal/web/adminapi ./internal/storage/azure -count=1` passed; Azure completed in 131.494s.
- Final focused public/app test and public Staticcheck passed after malformed-wildcard rejection.
- `npm run build` and direct `go build -tags=e2e -o ./bin/legal-callegarin-e2e ./cmd/web` passed during E2E diagnosis.

Not fully green / blockers:

- A final ordinary `make check` rerun after the malformed-wildcard hardening reached Node contracts but reported one failing top-level file, `scripts/infra-contract.test.mjs` (15/16 top-level test files passed). The compact parent runner did not expose a nested assertion. Per orchestrator direction, no further test execution or cleanup was performed. The prior clean-check simulation had passed this contract and all downstream gates.
- Full `npm run e2e` was attempted twice and exited 1 before any browser test because Playwright's configured web server exited 1. A direct E2E build succeeded, but the E2E binary logged `server_starting` followed immediately by the intentionally redacted `server_stopped` failure. No listener remained on port 4173.
- Filesystem evidence during the E2E attempts: the root filesystem was at 100% use with approximately 436 MB free, and the forced repository-local Playwright Go cache occupied approximately 288 MB. Disk pressure is a plausible environmental contributor, but the sanitized server error prevents proving the exact listen failure. No disk cleanup was performed, as prohibited.

## Assumptions and remaining risks

- The CSP hash extractor intentionally targets the single repository-owned JSON-LD script shape emitted by the cached templates. Tests hash the exact bytes between its tags.
- Weak HTML validation is based on the cached nonce-placeholder representation, so different nonce-expanded response bytes remain semantically equivalent for revalidation.
- Fail-closed username saturation can temporarily deny the configured username even with available tokens; that denial is intentional and ends only when pruning frees map capacity.
- Full browser behavior remains unverified in this constrained environment. Re-run `npm run e2e` on a host with adequate free space and working loopback HTTPS before integration.
- Re-run `make check` once more in a healthy environment to resolve the isolated `infra-contract.test.mjs` rerun failure, even though the clean-check simulation passed the complete target.

## Prohibited actions

No push, merge, Docker rebuild, dependency update, external mutation, or host disk cleanup was performed.
