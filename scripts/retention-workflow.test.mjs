import assert from "node:assert/strict"
import { readFile } from "node:fs/promises"
import { join, resolve } from "node:path"
import { fileURLToPath } from "node:url"
import test from "node:test"

const repositoryRoot = resolve(fileURLToPath(new URL("../", import.meta.url)))
const workflowPath = join(
  repositoryRoot,
  ".github",
  "workflows",
  "retention.yml",
)
const checkoutAction =
  "actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1"
const azureLoginAction = "azure/login@532459ea530d8321f2fb9bb10d1e0bcf23869a43"
const expected = `name: Retain private GHCR images

on:
  schedule:
    - cron: '17 3 * * 0'
  workflow_dispatch:

permissions:
  contents: read

jobs:
  retain-private-ghcr:
    name: Prune private GHCR versions
    if: >-
      github.repository == 'francescostumpo/legal-callegarin' &&
      github.ref == 'refs/heads/main' &&
      (github.event_name == 'schedule' || github.event_name == 'workflow_dispatch')
    runs-on: ubuntu-24.04
    timeout-minutes: 15
    environment: production
    permissions:
      contents: read
      id-token: write
      packages: write
    concurrency:
      group: production-deploy
      cancel-in-progress: false
    env:
      AZURE_CLIENT_ID: \${{ vars.AZURE_CLIENT_ID }}
      AZURE_TENANT_ID: \${{ vars.AZURE_TENANT_ID }}
      AZURE_SUBSCRIPTION_ID: \${{ vars.AZURE_SUBSCRIPTION_ID }}
      AZURE_RESOURCE_GROUP: \${{ vars.AZURE_RESOURCE_GROUP }}
      AZURE_CONTAINER_APP_NAME: \${{ vars.AZURE_CONTAINER_APP_NAME }}
      AZURE_CORE_OUTPUT: none
    steps:
      - name: Check out exact source revision
        uses: ${checkoutAction} # v7.0.1
        with:
          ref: \${{ github.sha }}
          persist-credentials: false

      - name: Log in to Azure with OIDC
        uses: ${azureLoginAction} # v3.0.0
        with:
          client-id: \${{ env.AZURE_CLIENT_ID }}
          tenant-id: \${{ env.AZURE_TENANT_ID }}
          subscription-id: \${{ env.AZURE_SUBSCRIPTION_ID }}

      - name: Prune verified private GHCR versions
        shell: bash
        env:
          GH_TOKEN: \${{ github.token }}
        run: |
          set -euo pipefail
          ./scripts/prune-ghcr-versions.mjs \\
            --subscription-id "$AZURE_SUBSCRIPTION_ID" \\
            --resource-group "$AZURE_RESOURCE_GROUP" \\
            --container-app-name "$AZURE_CONTAINER_APP_NAME" \\
            --apply
`

async function optionalText(path) {
  try {
    return await readFile(path, "utf8")
  } catch (error) {
    if (error.code === "ENOENT") return ""
    throw error
  }
}

function mappingKeys(source, indent) {
  const prefix = " ".repeat(indent)
  return source.split("\n").flatMap((line) => {
    const match = line.match(new RegExp(`^${prefix}([A-Za-z0-9_-]+):(?:\\s|$)`))
    return match ? [match[1]] : []
  })
}

function validate(source) {
  assert.equal(
    source,
    expected,
    "workflow must match the bounded reviewed structure",
  )
  assert.deepEqual(mappingKeys(source, 0), [
    "name",
    "on",
    "permissions",
    "jobs",
  ])
  assert.equal((source.match(/^  retain-private-ghcr:$/gm) ?? []).length, 1)

  const triggers = source.match(/^on:\n([\s\S]*?)\npermissions:/m)?.[1] ?? ""
  assert.deepEqual(mappingKeys(triggers, 2), ["schedule", "workflow_dispatch"])
  const cron = source.match(/^    - cron: '([^']+)'$/m)?.[1]
  assert.equal(cron, "17 3 * * 0")
  const [minute, hour, day, month, weekday] = cron.split(" ")
  assert.ok(Number(minute) > 0 && Number(minute) < 60)
  assert.ok(Number(hour) >= 0 && Number(hour) <= 23)
  assert.deepEqual([day, month, weekday], ["*", "*", "0"])

  const workflowPermissions =
    source.match(/^permissions:\n([\s\S]*?)\njobs:/m)?.[1] ?? ""
  assert.deepEqual(mappingKeys(workflowPermissions, 2), ["contents"])
  assert.match(workflowPermissions, /^  contents: read$/m)
  const jobPermissions =
    source.match(/^    permissions:\n([\s\S]*?)^    concurrency:/m)?.[1] ?? ""
  assert.deepEqual(mappingKeys(jobPermissions, 6), [
    "contents",
    "id-token",
    "packages",
  ])
  assert.match(jobPermissions, /^      contents: read$/m)
  assert.match(jobPermissions, /^      id-token: write$/m)
  assert.match(jobPermissions, /^      packages: write$/m)

  assert.match(source, /^    runs-on: ubuntu-24\.04$/m)
  assert.match(source, /^    timeout-minutes: 15$/m)
  assert.match(source, /^    environment: production$/m)
  assert.match(source, /^      group: production-deploy$/m)
  assert.match(source, /^      cancel-in-progress: false$/m)
  assert.match(
    source,
    /github\.repository == 'francescostumpo\/legal-callegarin'/,
  )
  assert.match(source, /github\.ref == 'refs\/heads\/main'/)
  assert.match(
    source,
    /\(github\.event_name == 'schedule' \|\| github\.event_name == 'workflow_dispatch'\)/,
  )

  const uses = [...source.matchAll(/^\s+uses: (\S+) # (\S+)$/gm)].map(
    (match) => [match[1], match[2]],
  )
  assert.deepEqual(uses, [
    [checkoutAction, "v7.0.1"],
    [azureLoginAction, "v3.0.0"],
  ])
  assert.match(
    source,
    new RegExp(
      `uses: ${checkoutAction.replaceAll("/", "\\/")} # v7\\.0\\.1\\n        with:\\n          ref: \\$\\{\\{ github\\.sha \\}\\}\\n          persist-credentials: false`,
    ),
  )
  assert.match(
    source,
    new RegExp(
      `uses: ${azureLoginAction.replaceAll("/", "\\/")} # v3\\.0\\.0\\n        with:\\n          client-id: \\$\\{\\{ env\\.AZURE_CLIENT_ID \\}\\}\\n          tenant-id: \\$\\{\\{ env\\.AZURE_TENANT_ID \\}\\}\\n          subscription-id: \\$\\{\\{ env\\.AZURE_SUBSCRIPTION_ID \\}\\}`,
    ),
  )

  const jobEnv = source.match(/^    env:\n([\s\S]*?)^    steps:/m)?.[1] ?? ""
  assert.deepEqual(mappingKeys(jobEnv, 6), [
    "AZURE_CLIENT_ID",
    "AZURE_TENANT_ID",
    "AZURE_SUBSCRIPTION_ID",
    "AZURE_RESOURCE_GROUP",
    "AZURE_CONTAINER_APP_NAME",
    "AZURE_CORE_OUTPUT",
  ])
  assert.doesNotMatch(jobEnv, /GH_TOKEN|github\.token|secrets\./)
  assert.equal((source.match(/GH_TOKEN:/g) ?? []).length, 1)
  assert.match(source, /^          GH_TOKEN: \$\{\{ github\.token \}\}$/m)

  const run = source.match(/^        run: \|\n([\s\S]*)$/m)?.[1] ?? ""
  assert.equal(
    run,
    `          set -euo pipefail
          ./scripts/prune-ghcr-versions.mjs \\
            --subscription-id "$AZURE_SUBSCRIPTION_ID" \\
            --resource-group "$AZURE_RESOURCE_GROUP" \\
            --container-app-name "$AZURE_CONTAINER_APP_NAME" \\
            --apply
`,
  )
  assert.doesNotMatch(
    source,
    /pull_request_target|continue-on-error|retry|curl|wget|\bgh\s+api|\baz\s+|\/packages\/.*\/versions|\$\(|set\s+-[^\n]*x|@latest|:latest|secrets\.|password|personal.access.token/i,
  )
  assert.equal(
    (source.match(/\.\/scripts\/prune-ghcr-versions\.mjs/g) ?? []).length,
    1,
  )
}

const workflow = await optionalText(workflowPath)

test("retention workflow has the exact bounded protected structure", () =>
  validate(workflow))

test("comments, duplicate or extra mappings, mutable actions, and command changes cannot satisfy the contract", () => {
  const mutations = [
    expected.replace("  workflow_dispatch:", "  push:\n  workflow_dispatch:"),
    expected.replace(
      "  contents: read\n\njobs:",
      "  contents: read\n  actions: write\n\njobs:",
    ),
    expected.replace(
      "      packages: write",
      "      packages: write\n      packages: write",
    ),
    expected.replace(checkoutAction, "actions/checkout@v7"),
    expected.replace(
      "          set -euo pipefail",
      "          # set -euo pipefail",
    ),
    expected.replace("            --apply", "            --dry-run"),
    expected.replace(
      "            --apply",
      "            --apply\n          gh api /user/packages/container/legal-callegarin",
    ),
    expected.replace("    steps:", "    continue-on-error: true\n    steps:"),
  ]
  for (const mutation of mutations) {
    assert.throws(() => validate(mutation))
  }
})
