# Final Remediation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make clean-checkout validation deterministic and correct public HTML revalidation/CSP, saturated username limiting, and the Azure attempt timeout without changing product content or unrelated behavior.

**Architecture:** Keep the existing monolith and stable `make check` entrypoint. Fix each independent boundary at its source: build ordering in Make, HTML-only validators/CSP in the public response writer, fail-closed saturation in the username bucket set, and the Azure executor constant.

**Tech Stack:** GNU Make, Node contract tests, Go `net/http`, `html/template`, SHA-256 CSP hashes, in-memory rate-limit buckets, Azure storage executor tests.

## Global Constraints

- Do not change approved public copy, privacy/consent version, storage schema, infrastructure/deploy code, dependencies, or admin UI.
- Preserve conditional public caching, cached JSON-LD, and per-request CSP nonces.
- Preserve production login capacity, address limiting, password verification, and normal failure messages.
- Keep one deterministic `make check` entrypoint using `npm ci`.
- Do not push, merge, rebuild Docker, or clean host disk.

---

### Task 1: Make clean-checkout validation frontend-first

**Files:**
- Modify: `Makefile`
- Modify: `scripts/foundation-config.test.mjs`
- Modify: `scripts/development-release-docs.test.mjs`
- Modify: `README.md`
- Modify: `docs/development-and-first-release.md`

**Interfaces:**
- Consumes: root npm lockfile and existing frontend `build` script.
- Produces: `make check` order `workflow-policy`, `npm ci`, formatting/type/frontend checks and build, then every Go consumer of `webassets.Files`.

- [ ] Add a Node contract assertion that `npm ci` and `npm run build` occur before `go vet`, Staticcheck, Go tests, and `check-clean-build.sh`.
- [ ] Run the focused Node tests and observe failure against the current Go-first order.
- [ ] Reorder only `make check`; retain the same commands and one stable target.
- [ ] Update README and development guide order plus their exact contract assertions.
- [ ] Run focused Node tests and observe success.
- [ ] Move `node_modules` and `internal/webassets/admin/dist` to a temporary directory, run `make check`, and restore both directories with a trap or explicit verified moves.

### Task 2: Make cached HTML validators and CSP semantically correct

**Files:**
- Modify: `internal/web/public/articles.go`
- Modify: `internal/app/app_test.go`
- Modify as needed for focused helpers: `internal/web/public/http_cache_test.go`

**Interfaces:**
- Consumes: cached HTML containing `cspNoncePlaceholder` and middleware-provided CSP nonce policy.
- Produces: weak HTML ETags, RFC weak `If-None-Match` comparison, and a stable SHA-256 source expression for exact inline JSON-LD bytes on 200 and 304.

- [ ] Add full-app table tests for `/`, `/sentenze-e-riflessioni`, and one published detail: two 200 responses have distinct body nonces and bodies but equal weak ETags; conditional GET returns 304/no body; 200/304 CSP contains the exact JSON-LD SHA-256 hash and no `unsafe-inline`.
- [ ] Add focused table cases proving `etagMatches` weakly compares strong/weak forms while rejecting different opaque tags.
- [ ] Run focused tests and observe strong ETag/hash failures.
- [ ] Add an HTML-only weak validator path without changing sitemap, robots, or assets.
- [ ] Extract exact inline JSON-LD content bytes, compute SHA-256/Base64, and append that source to `script-src` before either 200 or 304 is written.
- [ ] Run public/app focused tests and observe success.

### Task 3: Remove the saturated username oracle

**Files:**
- Modify: `internal/web/adminapi/rate_limit.go`
- Modify: `internal/web/adminapi/handler_test.go`

**Interfaces:**
- Consumes: `bucketSet.failClosedFull`, pinned configured username, bounded map.
- Produces: uniform denial for every username whenever the fail-closed username set remains full after pruning.

- [ ] Add limiter tests for a full two-entry set and `MaxBuckets=1` pinned-only edge case.
- [ ] Add handler regression with `MaxBuckets=2` and distinct client prefixes comparing configured and never-seen username status/body.
- [ ] Run the requested admin test pattern and observe the configured username remains distinguishable.
- [ ] After idle pruning, deny all keys when `failClosedFull` and `len(buckets) >= maxEntries`; retain eviction behavior for address sets.
- [ ] Run focused admin tests and observe uniform 429 behavior and bounded pinned protection.

### Task 4: Align the Azure executor timeout

**Files:**
- Modify: `internal/storage/azure/retry.go`
- Modify: `internal/storage/azure/retry_test.go`

**Interfaces:**
- Consumes: `defaultExecutor()` and zero-value executor fallback.
- Produces: exact `defaultTimeout == 5 * time.Second` with no retry tuning changes.

- [ ] Add a focused test asserting both the normative constant and default executor timeout are five seconds.
- [ ] Run the Executor test pattern and observe failure at three seconds.
- [ ] Change only `defaultTimeout` to five seconds.
- [ ] Run the Executor test pattern and observe success.

### Task 5: Aggregate verification and handoff

**Files:**
- Create: `.superpowers/sdd/final-remediation-report.md`

**Interfaces:**
- Consumes: all four approved fixes.
- Produces: evidence-backed report and committed branch state.

- [ ] Run focused Go/Node commands, `make check`, race tests, and full `npm run e2e`.
- [ ] Verify aggregate diff, prohibited-scope absence, clean generated-artifact state, and exact consent version.
- [ ] Record verified facts, assumptions, files, commands/results, and remaining risks in the report.
- [ ] Commit scoped files without push/merge.
