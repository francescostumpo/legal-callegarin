import assert from "node:assert/strict"
import { spawnSync } from "node:child_process"
import { chmod, mkdtemp, readFile, rm, writeFile } from "node:fs/promises"
import { tmpdir } from "node:os"
import { join, resolve } from "node:path"
import { fileURLToPath } from "node:url"
import test from "node:test"

const repositoryRoot = resolve(fileURLToPath(new URL("../", import.meta.url)))
const workflowPath = join(repositoryRoot, ".github", "workflows", "ci.yml")
const makefilePath = join(repositoryRoot, "Makefile")
const composePath = join(repositoryRoot, "compose.test.yaml")
const actionlintImage =
  "rhysd/actionlint:1.7.12@sha256:b1934ee5f1c509618f2508e6eb47ee0d3520686341fec936f3b79331f9315667"
const checkoutAction =
  "actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1"
const setupGoAction =
  "actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e"
const setupNodeAction =
  "actions/setup-node@820762786026740c76f36085b0efc47a31fe5020"
const uploadArtifactAction =
  "actions/upload-artifact@043fb46d1a93c77aae656e7c1c64a875d1fc6a0a"
const azuriteImage =
  "mcr.microsoft.com/azure-storage/azurite:3.37.0@sha256:830430c1da1a2d537e08f3e6764dd1f5ae00cf0346bcaf625b968ec3f0971fd5"
const integrationCommand =
  "go test -race -count=1 -tags=integration ./internal/storage/azure ./internal/storage/contracttest"
const azuriteConnectionString =
  "DefaultEndpointsProtocol=http;AccountName=devstoreaccount1;AccountKey=Eby8vdM02xNOcqFlqUwJPLlmEtlCDXJ1OUzFT50uSRZ6IFsuFq2UVErCz4I6tq/K1SZFPTOtr/KBHBeksoGMGw==;BlobEndpoint=http://127.0.0.1:11000/devstoreaccount1;TableEndpoint=http://127.0.0.1:11002/devstoreaccount1;"

async function optionalText(path) {
  try {
    return await readFile(path, "utf8")
  } catch (error) {
    if (error.code === "ENOENT") return ""
    throw error
  }
}

const [workflow, makefile, compose] = await Promise.all([
  optionalText(workflowPath),
  readFile(makefilePath, "utf8"),
  readFile(composePath, "utf8"),
])

function positionOf(pattern, label) {
  const match = workflow.match(pattern)
  assert.ok(match, `missing ${label}`)
  return match.index
}

test("PR CI has the sole trigger, read-only permissions, and bounded execution", () => {
  assert.notEqual(workflow, "", "expected .github/workflows/ci.yml")
  assert.match(workflow, /^on:\n  pull_request:\s*$/m)
  assert.doesNotMatch(workflow, /pull_request_target/)
  assert.doesNotMatch(
    workflow,
    /^  (?:push|schedule|workflow_dispatch|workflow_call):/m,
  )
  assert.match(workflow, /^permissions:\n  contents: read\s*$/m)
  assert.match(workflow, /^    timeout-minutes: (?:[1-9]|[1-5][0-9]|60)$/m)
  assert.doesNotMatch(workflow, /^\s*environment\s*:/m)
  assert.doesNotMatch(workflow, /^\s+(?:packages|id-token):\s*write\s*$/m)
  assert.doesNotMatch(workflow, /^\s*permissions:\s*write-all\s*$/m)
})

test("toolchain actions are exact, immutable, cache-aware, and do not persist credentials", () => {
  const remoteUses = [
    ...workflow.matchAll(/^\s+uses: (\S+) # (\S+)\s*$/gm),
  ].map((match) => [match[1], match[2]])
  assert.deepEqual(remoteUses, [
    [checkoutAction, "v7.0.1"],
    [setupGoAction, "v7.0.0"],
    [setupNodeAction, "v7.0.0"],
    [uploadArtifactAction, "v7.0.1"],
  ])
  assert.match(
    workflow,
    new RegExp(
      `uses: ${checkoutAction.replaceAll("/", "\\/")} # v7\\.0\\.1\\n        with:\\n          persist-credentials: false`,
    ),
  )
  assert.match(
    workflow,
    new RegExp(
      `uses: ${setupGoAction.replaceAll("/", "\\/")} # v7\\.0\\.0\\n        with:\\n          go-version-file: go\\.mod\\n          cache: true`,
    ),
  )
  assert.match(
    workflow,
    new RegExp(
      `uses: ${setupNodeAction.replaceAll("/", "\\/")} # v7\\.0\\.0\\n        with:\\n          node-version: 24\\.21\\.0\\n          cache: npm\\n          cache-dependency-path: package-lock\\.json`,
    ),
  )
  assert.doesNotMatch(workflow, /persist-credentials:\s*true/)
})

test("the pinned Bicep environment is inherited by every verification step", () => {
  assert.match(
    workflow,
    /jobs:\n  verify:\n    name: Verify pull request\n    runs-on: ubuntu-24\.04\n    timeout-minutes: 30\n    env:\n      AZURE_BICEP_CHECK_VERSION: "false"\n    steps:/,
  )
  assert.doesNotMatch(workflow, /\$\{\{ runner\.temp \}\}/)
  assert.match(
    workflow,
    /- name: Install exact Bicep CLI\n        shell: bash\n        run: \|\n          set -euo pipefail\n          export AZURE_CONFIG_DIR="\$RUNNER_TEMP\/azure-cli"\n          export DOTNET_BUNDLE_EXTRACT_BASE_DIR="\$RUNNER_TEMP\/dotnet"\n          printf 'AZURE_CONFIG_DIR=%s\\n' "\$AZURE_CONFIG_DIR" >> "\$GITHUB_ENV"\n          printf 'DOTNET_BUNDLE_EXTRACT_BASE_DIR=%s\\n' "\$DOTNET_BUNDLE_EXTRACT_BASE_DIR" >> "\$GITHUB_ENV"\n          mkdir -p "\$AZURE_CONFIG_DIR" "\$DOTNET_BUNDLE_EXTRACT_BASE_DIR"/,
  )
  assert.equal(workflow.match(/^      AZURE_BICEP_CHECK_VERSION:/gm)?.length, 1)
})

test("exact Bicep installation precedes all repository verification commands", () => {
  assert.match(
    workflow,
    /- name: Install exact Bicep CLI\n        shell: bash\n        run: \|\n          set -euo pipefail/,
  )
  const install = positionOf(
    /az bicep install --version v0\.45\.15/,
    "exact Bicep installation",
  )
  const assertion = positionOf(
    /Bicep CLI version 0\.45\.15/,
    "exact Bicep version assertion",
  )
  const actionlint = positionOf(
    /^          make actionlint$/m,
    "make actionlint",
  )
  const check = positionOf(/^          make check$/m, "make check")
  const race = positionOf(
    /^          go test -race -count=1 \.\/\.\.\.$/m,
    "Go race suite",
  )
  assert.ok(install < assertion)
  assert.ok(assertion < actionlint)
  assert.ok(actionlint < check)
  assert.ok(check < race)
})

test("Azurite readiness is transport-only, two-endpoint, bounded, and always torn down", () => {
  assert.match(compose, new RegExp(`^    image: ${azuriteImage}$`, "m"))
  assert.match(
    workflow,
    /docker compose -f compose\.test\.yaml up --detach azurite/,
  )
  assert.match(workflow, /timeout 60s bash -c/)
  assert.match(
    workflow,
    /http:\/\/127\.0\.0\.1:11000\/devstoreaccount1\?comp=list/,
  )
  assert.match(
    workflow,
    /http:\/\/127\.0\.0\.1:11002\/devstoreaccount1\/Tables/,
  )
  assert.doesNotMatch(workflow, /curl[^\n]*(?:--fail|\s-f(?:\s|$))/)
  assert.match(workflow, /docker compose -f compose\.test\.yaml ps/)
  assert.match(
    workflow,
    /docker compose -f compose\.test\.yaml logs --no-color azurite/,
  )
  assert.match(
    workflow,
    new RegExp(
      `env:\\n          AZURITE_CONNECTION_STRING: ${azuriteConnectionString.replaceAll("?", "\\?")}\\n        run: ${integrationCommand.replaceAll(".", "\\.")}`,
    ),
  )
  assert.doesNotMatch(workflow, /QueueEndpoint=/)
  assert.match(
    workflow,
    /- name: Stop Azurite\n        if: always\(\)\n        run: docker compose -f compose\.test\.yaml down --volumes --remove-orphans/,
  )
})

test("the local image identity is validated, built once, and gated without publication", () => {
  assert.match(
    workflow,
    /- name: Build local verification image\n        shell: bash\n        run: \|\n          set -euo pipefail/,
  )
  assert.match(workflow, /\^\[0-9a-f\]\{40\}\$/)
  assert.match(workflow, /ci_image="legal-callegarin:ci-\$GITHUB_SHA"/)
  assert.match(
    workflow,
    /docker build --pull --build-arg "VERSION=ci-\$GITHUB_SHA" --build-arg "COMMIT=\$GITHUB_SHA" --tag "\$ci_image" \./,
  )
  assert.match(workflow, /^          make sbom IMAGE="\$CI_IMAGE"$/m)
  assert.match(
    workflow,
    /^          make scan IMAGE="\$CI_IMAGE" SEVERITY=HIGH,CRITICAL$/m,
  )
  assert.ok(
    positionOf(/docker build --pull/, "container build") <
      positionOf(/make sbom IMAGE=/, "SBOM gate"),
  )
  assert.ok(
    positionOf(/make sbom IMAGE=/, "SBOM gate") <
      positionOf(/make scan IMAGE=/, "vulnerability gate"),
  )
  assert.doesNotMatch(
    workflow,
    /azure\/login|docker\/login-action|ghcr\.io|docker\s+(?:login|push)|download-artifact|\bdeploy\b|\bsecrets\.|GITHUB_TOKEN|github\.token/i,
  )
})

test("browser gates install local engines and run before Azurite", () => {
  const race = positionOf(
    /^          go test -race -count=1 \.\/\.\.\.$/m,
    "unit race suite",
  )
  const install = positionOf(
    /^          \.\/node_modules\/\.bin\/playwright install --with-deps chromium firefox webkit$/m,
    "local Playwright browser installation",
  )
  const chromium = positionOf(
    /^          npm run e2e -- --project=chromium$/m,
    "full Chromium browser gate",
  )
  const smoke = positionOf(
    /^          npm run e2e:smoke$/m,
    "three-engine smoke gate",
  )
  const composeUp = positionOf(
    /compose\.test\.yaml up --detach/,
    "Azurite start",
  )
  assert.ok(race < install)
  assert.ok(install < chromium)
  assert.ok(chromium < smoke)
  assert.ok(smoke < composeUp)
})

test("failure diagnostics upload only Playwright traces and screenshots for three days", () => {
  const uploadBlock = workflow.slice(
    positionOf(
      /- name: Upload Playwright failure diagnostics/,
      "Playwright diagnostic upload",
    ),
  )
  assert.match(
    workflow,
    new RegExp(
      `- name: Upload Playwright failure diagnostics\\n        if: failure\\(\\)\\n        uses: ${uploadArtifactAction.replaceAll("/", "\\/")} # v7\\.0\\.1\\n        with:\\n          name: playwright-failure-diagnostics\\n          path: \\|\\n            artifacts/playwright/test-results/\\*\\*/trace\\.zip\\n            artifacts/playwright/test-results/\\*\\*/\\*\\.png\\n          if-no-files-found: ignore\\n          retention-days: 3`,
    ),
  )
  assert.equal(workflow.match(/actions\/upload-artifact@/g)?.length, 1)
  assert.doesNotMatch(workflow, /^\s+path:\s+artifacts\/?\s*$/m)
  assert.doesNotMatch(
    uploadBlock,
    /html-report|\.webm|video|storageState|storage-state|credentials|request bod|request header/i,
  )
})

test("workflow commands retain the exact required integration and verification sequence", () => {
  const race = positionOf(
    /^          go test -race -count=1 \.\/\.\.\.$/m,
    "unit race suite",
  )
  const composeUp = positionOf(
    /compose\.test\.yaml up --detach/,
    "Azurite start",
  )
  const browserSmoke = positionOf(/^          npm run e2e:smoke$/m, "smoke")
  const integration = positionOf(
    new RegExp(integrationCommand.replaceAll(".", "\\.")),
    "tagged integration suite",
  )
  const teardown = positionOf(
    /compose\.test\.yaml down --volumes/,
    "Azurite teardown",
  )
  const build = positionOf(/docker build --pull/, "local container build")
  assert.ok(race < composeUp)
  assert.ok(browserSmoke < composeUp)
  assert.ok(composeUp < integration)
  assert.ok(integration < teardown)
  assert.ok(teardown < build)
})

test("make actionlint uses only the exact pinned, hardened local interface", async (t) => {
  assert.match(
    makefile,
    new RegExp(`^ACTIONLINT_IMAGE := ${actionlintImage}$`, "m"),
  )
  assert.match(makefile, /^\.PHONY:.*\bactionlint\b/m)
  assert.doesNotMatch(makefile, /^check:.*\bactionlint\b/m)

  const temporary = await mkdtemp(join(tmpdir(), "actionlint-target-test-"))
  t.after(() => rm(temporary, { recursive: true, force: true }))
  const engine = join(temporary, "fake-engine.mjs")
  const log = join(temporary, "args.json")
  await writeFile(
    engine,
    `#!/usr/bin/env node\nimport { writeFileSync } from "node:fs"\nwriteFileSync(process.env.ACTIONLINT_ENGINE_LOG, JSON.stringify(process.argv.slice(2)))\n`,
  )
  await chmod(engine, 0o700)

  const result = spawnSync(
    "make",
    ["--no-print-directory", "actionlint", `CONTAINER_ENGINE=${engine}`],
    {
      cwd: repositoryRoot,
      encoding: "utf8",
      env: { ...process.env, ACTIONLINT_ENGINE_LOG: log },
    },
  )
  assert.equal(result.status, 0, `${result.stdout}\n${result.stderr}`)
  assert.deepEqual(JSON.parse(await readFile(log, "utf8")), [
    "run",
    "--rm",
    "--network",
    "none",
    "--read-only",
    "--cap-drop",
    "ALL",
    "--security-opt",
    "no-new-privileges",
    "--tmpfs",
    "/tmp:rw,nosuid,nodev,noexec,size=16m",
    "--mount",
    `type=bind,src=${repositoryRoot},dst=/repo,readonly`,
    "--workdir",
    "/repo",
    actionlintImage,
    "-no-color",
  ])
})
