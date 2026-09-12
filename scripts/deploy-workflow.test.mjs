import assert from "node:assert/strict"
import { readFile } from "node:fs/promises"
import { join, resolve } from "node:path"
import { fileURLToPath } from "node:url"
import test from "node:test"

const repositoryRoot = resolve(fileURLToPath(new URL("../", import.meta.url)))
const workflowPath = join(repositoryRoot, ".github", "workflows", "deploy.yml")
const imageRepository = "ghcr.io/francescostumpo/legal-callegarin"
const checkoutAction =
  "actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1"
const setupBuildxAction =
  "docker/setup-buildx-action@37fe631027851001ddb9b187196cc803df7f5f0e"
const loginAction =
  "docker/login-action@dbcb813823bdd20940b903addbd779551569679f"
const buildPushAction =
  "docker/build-push-action@53b7df96c91f9c12dcc8a07bcb9ccacbed38856a"
const azureLoginAction =
  "azure/login@532459ea530d8321f2fb9bb10d1e0bcf23869a43"

async function optionalText(path) {
  try {
    return await readFile(path, "utf8")
  } catch (error) {
    if (error.code === "ENOENT") return ""
    throw error
  }
}

const workflow = await optionalText(workflowPath)

function positionOf(source, pattern, label) {
  const match = source.match(pattern)
  assert.ok(match, `missing ${label}`)
  return match.index
}

function jobBlock(id, nextId, source = workflow) {
  const start = positionOf(source, new RegExp(`^  ${id}:$`, "m"), `${id} job`)
  const end = nextId
    ? positionOf(source, new RegExp(`^  ${nextId}:$`, "m"), `${nextId} job`)
    : source.length
  assert.ok(start < end, `${id} must precede ${nextId}`)
  return source.slice(start, end)
}

function indentation(line) {
  return line.match(/^ */)[0].length
}

function mappingBlock(source, key, indent, label) {
  const lines = source.replaceAll("\r\n", "\n").split("\n")
  const header = `${" ".repeat(indent)}${key}:`
  const starts = lines
    .map((line, index) => (line === header ? index : -1))
    .filter((index) => index >= 0)
  assert.equal(starts.length, 1, `expected exactly one ${label} mapping`)

  const start = starts[0]
  let end = lines.length
  for (let index = start + 1; index < lines.length; index++) {
    if (lines[index].trim() === "") continue
    if (indentation(lines[index]) <= indent) {
      end = index
      break
    }
  }
  return lines.slice(start, end).join("\n").trimEnd()
}

function assertPermissionMappings(source) {
  const publish = jobBlock("publish", "deploy-production", source)
  const deploy = jobBlock("deploy-production", undefined, source)

  assert.equal(
    mappingBlock(source, "permissions", 0, "workflow permissions"),
    "permissions:\n  contents: read",
  )
  assert.equal(
    mappingBlock(publish, "permissions", 4, "publish permissions"),
    "    permissions:\n      contents: read\n      packages: write",
  )
  assert.equal(
    mappingBlock(deploy, "permissions", 4, "deploy permissions"),
    "    permissions:\n      contents: read\n      id-token: write",
  )
}

function assertPublishOutputMapping(source) {
  const publish = jobBlock("publish", "deploy-production", source)
  assert.equal(
    mappingBlock(publish, "outputs", 4, "publish outputs"),
    "    outputs:\n      image_ref: ${{ steps.verify.outputs.image_ref }}",
  )
}

test("production publication has only the exact main push and manual triggers", () => {
  assert.notEqual(workflow, "", "expected .github/workflows/deploy.yml")
  assert.match(workflow, /^name: Publish and deploy production$/m)
  assert.match(
    workflow,
    /^on:\n  push:\n    branches:\n      - main\n  workflow_dispatch:\s*\n\npermissions:\n  contents: read\s*$/m,
  )
  const triggers = workflow.slice(
    positionOf(workflow, /^on:$/m, "trigger mapping"),
    positionOf(workflow, /^permissions:$/m, "workflow permissions"),
  )
  assert.doesNotMatch(
    triggers,
    /^ +(?:pull_request|pull_request_target|schedule|workflow_call|paths|paths-ignore|tags|tags-ignore):/m,
  )
})

test("the two bounded jobs share exact repository, ref, and event guards", () => {
  const jobs = workflow.slice(positionOf(workflow, /^jobs:$/m, "jobs mapping"))
  const jobIds = [...jobs.matchAll(/^  ([a-z0-9-]+):$/gm)].map(
    (match) => match[1],
  )
  assert.deepEqual(jobIds, ["publish", "deploy-production"])

  const publish = jobBlock("publish", "deploy-production")
  const deploy = jobBlock("deploy-production")
  const exactGuard = [
    "    if: >-",
    "      github.repository == 'francescostumpo/legal-callegarin' &&",
    "      github.ref == 'refs/heads/main' &&",
    "      (github.event_name == 'push' || github.event_name == 'workflow_dispatch')",
  ].join("\n")

  for (const [name, job] of [
    ["publish", publish],
    ["deploy", deploy],
  ]) {
    assert.equal(
      job.split(exactGuard).length - 1,
      1,
      `${name} must have the exact hard guard`,
    )
    assert.match(job, /^    runs-on: ubuntu-24\.04$/m)
    const timeout = job.match(/^    timeout-minutes: (\d+)$/m)
    assert.ok(timeout, `${name} needs a timeout`)
    assert.ok(Number(timeout[1]) <= 30, `${name} timeout must be <= 30`)
  }

  assert.doesNotMatch(workflow, /github\.event\./)
})

test("permissions, dependency, environment, and concurrency are least privilege", () => {
  const publish = jobBlock("publish", "deploy-production")
  const deploy = jobBlock("deploy-production")

  assertPermissionMappings(workflow)
  assert.doesNotMatch(publish, /^\s+id-token:|^\s+environment:/m)
  assert.match(deploy, /^    needs: publish$/m)
  assert.match(deploy, /^    environment: production$/m)
  assert.doesNotMatch(deploy, /^\s+packages:/m)
  assert.match(
    deploy,
    /^    concurrency:\n      group: production-deploy\n      cancel-in-progress: false$/m,
  )
})

test("permission mappings reject an additional capability at every level", () => {
  const mutations = [
    [
      "workflow",
      workflow.replace(
        "permissions:\n  contents: read",
        "permissions:\n  contents: read\n  issues: read",
      ),
    ],
    [
      "publish",
      workflow.replace(
        "      packages: write",
        "      packages: write\n      issues: read",
      ),
    ],
    [
      "deploy",
      workflow.replace(
        "      id-token: write",
        "      id-token: write\n      actions: read",
      ),
    ],
  ]

  for (const [label, mutation] of mutations) {
    assert.notEqual(mutation, workflow, `${label} fixture must mutate source`)
    assert.throws(
      () => assertPermissionMappings(mutation),
      { name: "AssertionError" },
      `${label} mapping accepted an additional permission`,
    )
  }
})

test("remote actions are exact, ordered, immutable, and checkouts do not persist credentials", () => {
  const remoteUses = [
    ...workflow.matchAll(/^\s+uses: (\S+) # (\S+)\s*$/gm),
  ].map((match) => [match[1], match[2]])
  assert.deepEqual(remoteUses, [
    [checkoutAction, "v7.0.1"],
    [setupBuildxAction, "v4.3.0"],
    [loginAction, "v4.6.0"],
    [buildPushAction, "v7.3.0"],
    [checkoutAction, "v7.0.1"],
    [azureLoginAction, "v3.0.0"],
  ])

  const checkoutPattern = new RegExp(
    `uses: ${checkoutAction.replaceAll("/", "\\/")} # v7\\.0\\.1\\n        with:\\n          ref: \\$\\{\\{ github\\.sha \\}\\}\\n          persist-credentials: false`,
    "g",
  )
  assert.equal([...workflow.matchAll(checkoutPattern)].length, 2)
  assert.doesNotMatch(workflow, /persist-credentials:\s*true/)
})

test("publish validates identity before ephemeral GHCR login and pushes one amd64 SHA tag", () => {
  const publish = jobBlock("publish", "deploy-production")
  assert.match(
    publish,
    new RegExp(`^      IMAGE_REPOSITORY: ${imageRepository}$`, "m"),
  )
  assert.match(
    publish,
    new RegExp(
      `^      IMAGE_TAG: ${imageRepository.replaceAll("/", "\\/")}:sha-\\$\\{\\{ github\\.sha \\}\\}$`,
      "m",
    ),
  )
  assert.match(publish, /^      IMAGE_VERSION: sha-\$\{\{ github\.sha \}\}$/m)
  assert.match(publish, /\^\[0-9a-f\]\{40\}\$/)
  assert.match(
    publish,
    /expected_tag="\$IMAGE_REPOSITORY:sha-\$GITHUB_SHA"/,
  )
  assert.match(
    publish,
    /\^ghcr\\\.io\/francescostumpo\/legal-callegarin:sha-\[0-9a-f\]\{40\}\$/,
  )
  assert.ok(
    positionOf(publish, /- name: Validate image identity/, "identity validation") <
      positionOf(publish, /docker\/login-action@/, "GHCR login"),
  )
  assert.ok(
    positionOf(publish, /docker\/login-action@/, "GHCR login") <
      positionOf(publish, /docker\/build-push-action@/, "image build"),
  )
  assert.match(
    publish,
    /docker\/login-action@[0-9a-f]{40} # v4\.6\.0\n        with:\n          registry: ghcr\.io\n          username: \$\{\{ github\.actor \}\}\n          password: \$\{\{ github\.token \}\}/,
  )
  assert.match(
    publish,
    /docker\/build-push-action@[0-9a-f]{40} # v7\.3\.0\n        with:\n          context: \.\n          file: \.\/Dockerfile\n          pull: true\n          platforms: linux\/amd64\n          push: true\n          tags: \$\{\{ env\.IMAGE_TAG \}\}/,
  )
  assert.match(
    publish,
    /build-args: \|\n            VERSION=\$\{\{ env\.IMAGE_VERSION \}\}\n            COMMIT=\$\{\{ github\.sha \}\}/,
  )
  assert.match(
    publish,
    /labels: \|\n            org\.opencontainers\.image\.source=https:\/\/github\.com\/francescostumpo\/legal-callegarin\n            org\.opencontainers\.image\.revision=\$\{\{ github\.sha \}\}\n            org\.opencontainers\.image\.version=\$\{\{ env\.IMAGE_VERSION \}\}/,
  )
  assert.match(publish, /^          provenance: false$/m)
  assert.match(publish, /^          sbom: false$/m)
  assert.equal([...publish.matchAll(/^          tags:/gm)].length, 1)
})

test("publish independently verifies the pushed digest and exports only the immutable reference", () => {
  const publish = jobBlock("publish", "deploy-production")
  assertPublishOutputMapping(workflow)
  assert.match(publish, /^          BUILD_DIGEST: \$\{\{ steps\.build\.outputs\.digest \}\}$/m)
  assert.match(publish, /\^sha256:\[0-9a-f\]\{64\}\$/)
  assert.match(
    publish,
    /resolved_digest_json="\$\(docker buildx imagetools inspect --format '\{\{json \.Manifest\.Digest\}\}' "\$IMAGE_TAG"\)"/,
  )
  assert.match(
    publish,
    /if \[\[ ! "\$resolved_digest_json" =~ \^\\"sha256:\[0-9a-f\]\{64\}\\"\$ \]\]; then/,
  )
  assert.match(publish, /resolved_digest="\$\{resolved_digest_json#\\"\}"/)
  assert.match(publish, /resolved_digest="\$\{resolved_digest%\\"\}"/)
  assert.match(publish, /if \[\[ "\$resolved_digest" != "\$BUILD_DIGEST" \]\]; then/)
  assert.match(
    publish,
    /printf 'image_ref=%s@%s\\n' "\$IMAGE_REPOSITORY" "\$BUILD_DIGEST" >> "\$GITHUB_OUTPUT"/,
  )
  const digestSequence = [
    positionOf(publish, /imagetools inspect/, "independent digest inspection"),
    positionOf(publish, /resolved_digest_json" =~/, "quoted digest validation"),
    positionOf(publish, /resolved_digest_json#/, "opening quote removal"),
    positionOf(publish, /resolved_digest%/, "closing quote removal"),
    positionOf(
      publish,
      /resolved_digest" =~ \^sha256:/,
      "raw digest validation",
    ),
    positionOf(
      publish,
      /resolved_digest" != "\$BUILD_DIGEST"/,
      "digest equality",
    ),
    positionOf(publish, /image_ref=%s@%s/, "immutable job output"),
  ]
  assert.deepEqual(digestSequence, [...digestSequence].sort((left, right) => left - right))
})

test("publish outputs reject an additional non-step value", () => {
  const mutation = workflow.replace(
    "      image_ref: ${{ steps.verify.outputs.image_ref }}",
    "      image_ref: ${{ steps.verify.outputs.image_ref }}\n      mutable_ref: literal",
  )

  assert.notEqual(mutation, workflow, "output fixture must mutate source")
  assert.throws(
    () => assertPublishOutputMapping(mutation),
    { name: "AssertionError" },
    "publish mapping accepted an additional output",
  )
})

test("deploy uses environment-backed OIDC and the verified cross-job digest", () => {
  const deploy = jobBlock("deploy-production")
  const expectedEnvironment = [
    "    env:",
    "      AZURE_CLIENT_ID: ${{ vars.AZURE_CLIENT_ID }}",
    "      AZURE_TENANT_ID: ${{ vars.AZURE_TENANT_ID }}",
    "      AZURE_SUBSCRIPTION_ID: ${{ vars.AZURE_SUBSCRIPTION_ID }}",
    "      AZURE_RESOURCE_GROUP: ${{ vars.AZURE_RESOURCE_GROUP }}",
    "      AZURE_CONTAINER_APP_NAME: ${{ vars.AZURE_CONTAINER_APP_NAME }}",
    "      PUBLIC_BASE_URL: ${{ vars.PUBLIC_BASE_URL }}",
    "      AZURE_CORE_OUTPUT: none",
    "      IMAGE_REF: ${{ needs.publish.outputs.image_ref }}",
  ].join("\n")
  assert.equal(deploy.split(expectedEnvironment).length - 1, 1)
  assert.match(
    deploy,
    /azure\/login@[0-9a-f]{40} # v3\.0\.0\n        with:\n          client-id: \$\{\{ env\.AZURE_CLIENT_ID \}\}\n          tenant-id: \$\{\{ env\.AZURE_TENANT_ID \}\}\n          subscription-id: \$\{\{ env\.AZURE_SUBSCRIPTION_ID \}\}/,
  )
  assert.doesNotMatch(deploy, /\bcreds\s*:|AZURE_CLIENT_SECRET|client-secret|\bsecrets\./i)
  assert.match(
    deploy,
    /\^ghcr\\\.io\/francescostumpo\/legal-callegarin@sha256:\[0-9a-f\]\{64\}\$/,
  )
  assert.match(deploy, /\^\[0-9a-f\]\{40\}\$/)
  assert.match(deploy, /\^\[0-9\]\+\$/)
  assert.match(
    deploy,
    /revision_suffix="deploy-\$\{GITHUB_SHA:0:12\}-\$\{GITHUB_RUN_ID\}-\$\{GITHUB_RUN_ATTEMPT\}"/,
  )
})

test("deploy delegates exactly one ordered six-argument mutation to the approved script", () => {
  const deploy = jobBlock("deploy-production")
  assert.match(
    deploy,
    /\.\/scripts\/deploy-container-app\.sh \\\n            --subscription-id "\$AZURE_SUBSCRIPTION_ID" \\\n            --resource-group "\$AZURE_RESOURCE_GROUP" \\\n            --container-app-name "\$AZURE_CONTAINER_APP_NAME" \\\n            --image-digest "\$IMAGE_REF" \\\n            --revision-suffix "\$revision_suffix" \\\n            --public-base-url "\$PUBLIC_BASE_URL"/,
  )
  assert.equal(
    [...deploy.matchAll(/\.\/scripts\/deploy-container-app\.sh/g)].length,
    1,
  )
  assert.ok(
    positionOf(deploy, /azure\/login@/, "Azure OIDC login") <
      positionOf(deploy, /deploy-container-app\.sh/, "deployment script"),
  )
})

test("workflow contains no mutable publication, direct cloud mutation, retention, or credentials", () => {
  assert.doesNotMatch(workflow, /(?:@|:)latest\b/i)
  assert.doesNotMatch(workflow, /github\.event\./)
  assert.doesNotMatch(workflow, /\b(?:docker push|az containerapp|az rest|curl)\b/i)
  assert.doesNotMatch(
    workflow,
    /(?:delete|deactivate|traffic|retention|bicep|custom[- ]?domain|client[-_ ]?secret|password:\s*\$\{\{\s*secrets\.)/i,
  )
  assert.doesNotMatch(workflow, /\b(?:pull_request|pull_request_target|schedule|workflow_call)\b/)
  assert.doesNotMatch(workflow, /set -x|ACTIONS_STEP_DEBUG|RUNNER_DEBUG/)
})
