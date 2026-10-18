# CI security pin remediation implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this bounded task with independent review.

**Goal:** Restore PR #2 CI without weakening its HIGH/CRITICAL security gate.

**Architecture:** Update only the Go toolchain and the vulnerable transitive network module; preserve application behavior, runtime image, private registry migration and Azure deployment gates.

**Tech Stack:** Go 1.27.2, golang.org/x/net v0.60.0, Docker, existing Node contract tests and GitHub Actions.

## Global constraints

- Go must be exactly `1.27.2` in go.mod and the Docker builder stage.
- Builder image must be `golang:1.27.2-bookworm@sha256:5cf287a799e6b94384bad13d16b14904c531f51ba65792237e122ce42b392f61`, resolved from the official registry.
- golang.org/x/net must resolve to `v0.60.0`; regenerate module sums with Go tooling and include only necessary dependency changes.
- Do not disable scanning, add vulnerability ignores, change severity thresholds, or modify application behavior, credentials, infrastructure or unrelated files.
- Preserve `.worklk` and ignored operational/secret files. No additional worktrees.

### Task 1: Upgrade security pins and prove regression

**Scope/files:** go.mod, go.sum, Dockerfile, README.md, docs/development-and-first-release.md, scripts/foundation-config.test.mjs and scripts/development-release-docs.test.mjs. This plan is the only new documentation. Historical website implementation plan remains unchanged.

**Inputs:** CI run 39123045263 fails only at Scan local image: four HIGH findings in Go 1.27.1 and x/net v0.58.0; reported fixed versions are Go 1.27.2 and x/net v0.60.0. Prior commit is 835151dc8411d18886ac88f0ca6fb8f3185f604d.

- [ ] Update existing contract tests to require the exact new builder image, Go directive and x/net dependency. Run targeted Node tests first and record expected failure against the old pins.
- [ ] Update production pins with apply_patch. Run `GOTOOLCHAIN=go1.27.2 go get golang.org/x/net@v0.60.0` and `GOTOOLCHAIN=go1.27.2 go mod tidy` for generated dependency metadata; inspect changes and do not broaden upgrades.
- [ ] Update current README/release prerequisites to Go 1.27.2; leave historical plans intact.
- [ ] Run formatter on changed contract tests and README; run `node --test scripts/foundation-config.test.mjs scripts/development-release-docs.test.mjs` with existing isolated Bicep test config.
- [ ] Run `GOTOOLCHAIN=go1.27.2 go test ./...`, `go vet ./...` and `go tool staticcheck ./...` with a writable temporary GOCACHE.
- [ ] Commit only scoped files after validation; report changed files, red/green evidence, commands, results and residual risks. Parent will independently review, push and require green hosted CI (including image scan) on this revision.

**Acceptance criteria:** Targeted contracts and Go validations pass. CI scanner no longer detects the four reported HIGH findings. Build remains immutable and nonroot; no security gate weakened. Independent review approved before push. Hosted CI success, rather than local tests alone, closes the task.

**Operational follow-up:** Do not enable ordinary Azure deployment or retention before the existing custom-domain and six-variable gates. Check live health/login after applicable authorized changes. Domain order remains blocked pending Azure subscription eligibility support; no alternative registrar purchase is authorized.
