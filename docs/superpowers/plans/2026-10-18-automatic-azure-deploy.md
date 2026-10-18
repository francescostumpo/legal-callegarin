# Automatic Azure Deploy Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task.

**Goal:** Configure the explicitly authorized GitHub–Azure federation and verify a complete production deployment through GitHub Actions.

**Architecture:** Retain the reviewed immutable GitHub OIDC subject, passwordless Entra application and resource-group-only Container Apps Contributor role. Upstream the observed Azure response compatibility fixes before enabling the main-only production workflow. Keep private GHCR publishing on the ephemeral GitHub job token and retain existing Azure runtime secrets.

**Tech Stack:** Bash, Node test runner, GitHub Actions/API, Azure CLI, Microsoft Entra OIDC, Azure Container Apps, private GHCR.

## Global Constraints

- Repository: `francescostumpo/legal-callegarin`; owner ID `55147498`; repository ID `1365534753`.
- Tenant: `5b6f035f-b1c4-4c9e-8e1a-a2e92e9c2c32`; subscription: `79ca006a-b270-474a-8a3d-cf3f8cda866d`.
- Resource group: `rg-callegarin-prod`; app: `legal-prod-app`; public origin: `https://studiolegalecallegarin.it`.
- Exact OIDC subject: `repo:francescostumpo@55147498/legal-callegarin@1365534753:environment:production`; issuer `https://token.actions.githubusercontent.com`; audience `api://AzureADTokenExchange`.
- Role: Container Apps Contributor `358470bc-b998-42bd-ab17-a7e34c199c0f`, scoped only to the resource group. No subscription-wide roles, Owner, broad Contributor, client secrets, password credentials, or extra Reader assignment.
- Preserve business data, credentials, runtime secret references, storage schema `migrate`, named revision traffic, custom-domain certificates, scale/resources and probes. No Bicep redeployment.
- Never print tokens, passwords, hashes, session keys, contact bodies or signed log URLs. Operational evidence may contain identifiers and statuses only.
- Preserve `.worklk` and ignored artifacts. No dependency updates or retention-workflow activation in this task.
- Keep production deployment disabled until the reviewed tracked script and protected environment configuration are ready. Merge the bounded fix only after fresh hosted CI succeeds.

## Task 1: Track Azure response compatibility without increasing IAM scope

**Files:** Modify `scripts/deploy-container-app.sh`, `scripts/deploy-container-app.test.mjs`, and relevant deployment paragraphs in `docs/operations.md`. This plan is orchestrator-owned.

**Inputs:** Current script; existing full fake-Azure rollout/rollback harness; `/tmp/callegarin-deploy-reviewed.sh` and `/tmp/callegarin-deploy-running-state.test.mjs` as previously validated evidence, not files to copy wholesale.

**Produces:** Same six-argument deployment CLI, compatible with actual observed Azure projections and usable with only the declared role. No workflow permission or IAM bootstrap policy change.

- [ ] Add failing regression scenarios to the existing full deployment harness before script changes. Run `env -i PATH="$PATH" node --test scripts/deploy-container-app.test.mjs`; observe the new cases fail for the known reasons.
- [ ] Accept `RunningAtMaxScale` only alongside active, Healthy, Provisioned status. Retain Running and ScaledToZero; reject Failed, Degraded, Unknown, Stopped, Processing, inactive, unhealthy and unprovisioned revisions.
- [ ] Canonicalize only observed semantic defaults for comparison: empty literal `value` alongside an unchanged `secretRef` is equivalent to absent `value`; missing ephemeralStorage is `1Gi` only at CPU exactly `0.25`; null/absent scale cooldownPeriod and pollingInterval mean `300` and `30`. Keep non-empty literals, changed references, non-default storage/CPU/memory/scaling/rules as errors.
- [ ] Require the exact candidate resource name `<container-app>--<requested-suffix>`, derived from its validated ARM identity. Permit null/omitted revisionSuffix only under that exact identity; reject an explicit wrong suffix, wrong name/prefix or endsWith collision. Preserve immutable image, template, health, zero-traffic and rollback checks.
- [ ] Remove the standalone `az group show` dependency: the declared role lacks `Microsoft.Resources/subscriptions/resourceGroups/read`. Keep exact subscription/resource-group identity enforced by the existing full Container App ARM ID comparison. Change wrong-RG fixture to an actual app ARM ID in another RG, and test deployment works when group metadata reads are denied. Do not add another role or suppress an authorization failure on a required operation.
- [ ] Document these narrow response normalizations and the full-resource-ID scope check. No unrelated document rewrites.
- [ ] Run `env -i PATH="$PATH" node --test scripts/deploy-container-app.test.mjs scripts/deploy-workflow.test.mjs scripts/bootstrap-github-oidc.test.mjs`, `bash -n scripts/deploy-container-app.sh`, Prettier check on changed formatted files, `node scripts/check-workflows.mjs`, and `git diff --check`. Preserve all existing negative, pagination, rollback and non-disclosure coverage.
- [ ] Commit only task-owned files on `codex/automatic-azure-deploy`; report exact commands/results and remaining concerns to `/tmp/callegarin-auto-deploy-task1-report.md`. Do not push, merge, call Azure, access credentials, or spawn agents.
- [ ] Independent read-only task review must approve before publishing. Route every actionable finding back to the implementer.

## Task 2: Configure the authorized identity and protected environment

**Scope:** Existing reviewed bootstrap script and GitHub/Azure configuration, orchestrator execution only after source/policy review. No product writes in this task.

- [ ] Verify current tenant/subscription, exact repository OIDC configuration, production environment main-only branch policy, and existing identity/RBAC without exposing secrets.
- [ ] Run bootstrap dry-run with exact global identifiers, application display name `legal-callegarin-prod-github` and FIC name `github-production`; inspect intended trust and RG scope.
- [ ] Run the reviewed idempotent bootstrap with the existing operator credential held only in process memory/environment. Confirm exact Entra app/SP identities, no password credentials, one exact federation and one exact RG-scoped role assignment. Re-run to prove no unintended changes.
- [ ] Set and verify exactly six nonsecret production environment variables: AZURE_CLIENT_ID from bootstrap output; AZURE_TENANT_ID; AZURE_SUBSCRIPTION_ID; AZURE_RESOURCE_GROUP; AZURE_CONTAINER_APP_NAME; PUBLIC_BASE_URL using global values. No environment secrets.
- [ ] Verify protected production environment allows main only; verify main required CI protection. Make only necessary scoped configuration changes, preserving existing protection.
- [ ] Independently review operational configuration helpers. Enforce trusted CI app ID `15368` explicitly; never print environment-variable values; verify returned and freshly fetched check bindings, strict/admin enforcement and false force-push/deletion flags. Offline regressions must reject untrusted apps and unexpected protections and prove secret-looking variable values remain redacted. Reviewer approval is required before workflow activation.

## Task 3: Publish, verify and activate automatic deployment

**Inputs:** Approved Task 1 commit, audited federation/environment, existing disabled workflow.

- [ ] Push the bounded branch and create a PR. Wait for complete hosted CI, including image scan, at the exact PR head. Never merge a failing or superseded head.
- [ ] With reviewed code and green CI, merge the bounded PR into main, update local main fast-forward only, then enable `deploy.yml` and dispatch its main revision once. Leave retention disabled.
- [ ] Verify both hosted jobs: private immutable GHCR publish, Azure OIDC login, candidate health checks and production digest deployment. If a job fails, inspect sanitized evidence and diagnose before any change; preserve or restore prior healthy traffic.
- [ ] Verify live ready revision, immutable image digest, named 100% traffic, preserved domain bindings, HTTPS/canonical redirects, liveness/readiness, actual customer login, dashboard/contact/article GET APIs, and logout of the verification session.
- [ ] Save sanitized operational evidence in ignored `artifacts/` and provide workflow/PR/site links. Clearly distinguish completed automatic deployment from any remaining blockers; do not claim success from workflow enablement alone.

## Progress

- Baseline: targeted deploy/bootstrap/workflow contract files passed before changes; main e6d41ff has successful manual deployment.
- Task 1: implementation and independent review approved; targeted deploy/bootstrap/workflow/documentation tests passed. Publish with this plan, then require hosted CI.
- Task 2: complete; exact bootstrap audit passed twice idempotently, six production variables and zero environment secrets verified, main-only production and trusted CI main protection verified. Operational helper fixes independently approved and read-only live audit passed.
- Task 3: pending hosted CI, merge and end-to-end workflow validation. Retention remains disabled.
