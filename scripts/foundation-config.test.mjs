import assert from "node:assert/strict"
import {
  chmod,
  mkdir,
  mkdtemp,
  readFile,
  rm,
  stat,
  writeFile,
} from "node:fs/promises"
import { tmpdir } from "node:os"
import { join } from "node:path"
import { spawnSync } from "node:child_process"
import test from "node:test"

const repositoryRoot = new URL("../", import.meta.url)

async function readRepositoryFile(path) {
  return readFile(new URL(path, repositoryRoot), "utf8")
}

test("a clean checkout retains the binary output directory", async () => {
  const [gitignore, makefile, hasPlaceholder] = await Promise.all([
    readRepositoryFile(".gitignore"),
    readRepositoryFile("Makefile"),
    stat(new URL("bin/.keep", repositoryRoot)).then(
      () => true,
      () => false,
    ),
  ])

  assert.equal(hasPlaceholder, true)
  assert.match(gitignore, /^\/bin\/\*$/m)
  assert.match(gitignore, /^!\/bin\/\.keep$/m)
  assert.doesNotMatch(gitignore, /^\/bin\/$/m)
  assert.match(makefile, /^\t\.\/scripts\/check-clean-build\.sh$/m)
})

test("the frontend formatter is pinned and ordered after npm ci", async () => {
  const [packageJSON, makefile] = await Promise.all([
    readRepositoryFile("package.json"),
    readRepositoryFile("Makefile"),
  ])
  const manifest = JSON.parse(packageJSON)

  assert.match(manifest.devDependencies.prettier, /^\d+\.\d+\.\d+$/)
  assert.equal(
    manifest.scripts["format:check"],
    'prettier --check "*.{json,md}" "scripts/**/*.mjs" "web/admin/**/*.{ts,tsx,html,css,json,md}" "internal/webassets/**/*.{html,css}"',
  )

  const installIndex = makefile.indexOf("\tnpm ci")
  const formatIndex = makefile.indexOf("\tnpm run format:check")
  const typecheckIndex = makefile.indexOf("\tnpm run typecheck")

  assert.notEqual(installIndex, -1)
  assert.ok(formatIndex > installIndex)
  assert.ok(typecheckIndex > formatIndex)
})

test("the article migration runbook fails closed on revision mode and legacy rows", async () => {
  const [runbook, rolloutScript] = await Promise.all([
    readRepositoryFile("docs/article-storage-rollout.md"),
    readRepositoryFile("scripts/article-storage-rollout.sh"),
  ])

  const shellBlocks = [...runbook.matchAll(/```bash\n([\s\S]*?)```/g)]
  assert.ok(shellBlocks.length > 0)
  for (const block of shellBlocks) {
    assert.match(block[1], /^set -eu\n/)
  }
  assert.match(
    rolloutScript,
    /length\(items\[\?id == `null` \|\| RowKey == id\]\)/,
  )
  assert.match(rolloutScript, /test "\$legacy_count" = 0 \|\| fail/)
  assert.equal(
    [...runbook.matchAll(/^sh "\$ROLLOUT_SCRIPT" assert-no-legacy$/gm)].length,
    2,
  )

  assert.match(runbook, /scripts\/article-storage-rollout\.sh/)
  assert.doesNotMatch(
    runbook,
    /<each-active-revision-name>|<healthy-repair-revision>|<healthy-recovery-migrate-revision>/,
  )
  assert.match(rolloutScript, /--query '\[\?properties\.active\]\.name'/)
  assert.match(rolloutScript, /--query 'length\(\[\?properties\.active\]\)'/)
  assert.match(rolloutScript, /^  sleep 30$/m)
  assert.match(rolloutScript, /properties\.configuration\.activeRevisionsMode/)
  assert.match(rolloutScript, /test "\$active_revisions_mode" = Multiple/)
})

const rolloutScriptPath = new URL("article-storage-rollout.sh", import.meta.url)

async function runRolloutCommand(args, overrides = {}) {
  const directory = await mkdtemp(join(tmpdir(), "legal-callegarin-rollout-"))
  const fakeBin = join(directory, "bin")
  const log = join(directory, "calls.log")
  await writeFile(log, "")
  await mkdir(fakeBin)

  const fakeAz = `#!/bin/sh
set -eu
printf 'az %s\\n' "$*" >> "$FAKE_CALL_LOG"
case "$*" in
  *"storage entity query"*) printf '%s\\n' "${"$"}{FAKE_LEGACY_COUNT:-0}" ;;
  *"revision list"*"[?properties.active].name"*) printf '%s\\n' "${"$"}{FAKE_ACTIVE_NAMES:-}" ;;
  *"revision list"*"length([?properties.active])"*) printf '%s\\n' "${"$"}{FAKE_RESIDUAL_COUNT:-0}" ;;
  *"containerapp show"*"properties.latestRevisionName"*) printf '%s\\n' "${"$"}{FAKE_LATEST_REVISION:-revision-new}" ;;
  *"containerapp show"*"properties.configuration.activeRevisionsMode"*) printf '%s\\n' "${"$"}{FAKE_ACTIVE_MODE:-Multiple}" ;;
  *"revision show"*) printf '%s\\t%s\\t%s\\n' "${"$"}{FAKE_HEALTH:-Healthy}" "${"$"}{FAKE_REVISION_IMAGE:-ghcr.io/example/legal@sha256:digest}" "${"$"}{FAKE_REVISION_SCHEMA_MODE:-repair}" ;;
  *) : ;;
esac
`
  const fakeSleep = `#!/bin/sh
set -eu
printf 'sleep %s\\n' "$*" >> "$FAKE_CALL_LOG"
`
  await Promise.all([
    writeFile(join(fakeBin, "az"), fakeAz),
    writeFile(join(fakeBin, "sleep"), fakeSleep),
  ])
  await Promise.all([
    chmod(join(fakeBin, "az"), 0o755),
    chmod(join(fakeBin, "sleep"), 0o755),
  ])

  const result = spawnSync("sh", [rolloutScriptPath.pathname, ...args], {
    encoding: "utf8",
    env: {
      ...process.env,
      PATH: `${fakeBin}:${process.env.PATH}`,
      FAKE_CALL_LOG: log,
      RESOURCE_GROUP: "rg-test",
      CONTAINER_APP: "app-test",
      STORAGE_ACCOUNT: "storage-test",
      IMAGE_DIGEST: "ghcr.io/example/legal@sha256:digest",
      ...overrides,
    },
  })
  const calls = await readFile(log, "utf8")
  await rm(directory, { recursive: true, force: true })
  return { ...result, calls: calls.trim().split("\n").filter(Boolean) }
}

function callIndex(calls, fragment) {
  return calls.findIndex((call) => call.includes(fragment))
}

test("repair quiescence deactivates every active revision before drain and update", async () => {
  const result = await runRolloutCommand(["quiesce-and-create-repair"], {
    FAKE_ACTIVE_NAMES: "revision-a\nrevision-b",
    FAKE_RESIDUAL_COUNT: "0",
    FAKE_LATEST_REVISION: "revision-repair",
    FAKE_REVISION_SCHEMA_MODE: "repair",
  })

  assert.equal(result.status, 0, result.stderr)
  assert.equal(result.stdout.trim(), "revision-repair")
  assert.equal(
    result.calls.filter((call) => call.includes("revision deactivate")).length,
    2,
  )
  assert.ok(result.calls.some((call) => call.endsWith("--revision revision-a")))
  assert.ok(result.calls.some((call) => call.endsWith("--revision revision-b")))
  const residual = callIndex(result.calls, "length([?properties.active])")
  const sleep = callIndex(result.calls, "sleep 30")
  const update = callIndex(result.calls, "containerapp update")
  assert.ok(residual > callIndex(result.calls, "--revision revision-b"))
  assert.ok(sleep > residual)
  assert.ok(update > sleep)
  const observation = callIndex(result.calls, "[].{name:name,active:")
  const generatedRevision = callIndex(
    result.calls,
    "properties.latestRevisionName",
  )
  const health = callIndex(result.calls, "revision show")
  assert.ok(observation > update)
  assert.ok(generatedRevision > observation)
  assert.ok(health > generatedRevision)
})

test("repair creation fails closed when its generated revision is unhealthy", async () => {
  const result = await runRolloutCommand(["quiesce-and-create-repair"], {
    FAKE_ACTIVE_NAMES: "revision-a",
    FAKE_RESIDUAL_COUNT: "0",
    FAKE_LATEST_REVISION: "revision-repair",
    FAKE_HEALTH: "Unhealthy",
    FAKE_REVISION_SCHEMA_MODE: "repair",
  })

  assert.notEqual(result.status, 0)
  assert.equal(result.stdout, "")
  assert.equal(callIndex(result.calls, "ingress traffic set"), -1)
})

test("repair quiescence aborts before drain and update when a revision remains active", async () => {
  const result = await runRolloutCommand(["quiesce-and-create-repair"], {
    FAKE_ACTIVE_NAMES: "revision-a\nrevision-b",
    FAKE_RESIDUAL_COUNT: "1",
  })

  assert.notEqual(result.status, 0)
  assert.equal(
    result.calls.filter((call) => call.includes("revision deactivate")).length,
    2,
  )
  assert.equal(callIndex(result.calls, "sleep 30"), -1)
  assert.equal(callIndex(result.calls, "containerapp update"), -1)
})

test("recovery promotion orders update health mode traffic and repair deactivation", async () => {
  const result = await runRolloutCommand(
    ["create-and-promote-recovery-migrate", "revision-repair"],
    {
      FAKE_LATEST_REVISION: "revision-migrate",
      FAKE_REVISION_SCHEMA_MODE: "migrate",
      FAKE_ACTIVE_MODE: "Multiple",
    },
  )

  assert.equal(result.status, 0, result.stderr)
  assert.equal(result.stdout.trim(), "revision-migrate")
  const update = callIndex(result.calls, "containerapp update")
  const observation = callIndex(result.calls, "[].{name:name,active:")
  const generatedRevision = callIndex(
    result.calls,
    "properties.latestRevisionName",
  )
  const health = callIndex(result.calls, "revision show")
  const setMode = callIndex(result.calls, "revision set-mode")
  const modeCheck = callIndex(
    result.calls,
    "properties.configuration.activeRevisionsMode",
  )
  const traffic = callIndex(result.calls, "ingress traffic set")
  const deactivate = callIndex(result.calls, "--revision revision-repair")
  assert.ok(update >= 0 && observation > update)
  assert.ok(generatedRevision > observation && health > generatedRevision)
  assert.ok(setMode > health && modeCheck > setMode)
  assert.ok(traffic > modeCheck && deactivate > traffic)
})

test("recovery promotion never shifts traffic when revision checks fail", async () => {
  for (const overrides of [
    { FAKE_HEALTH: "Unhealthy", FAKE_REVISION_SCHEMA_MODE: "migrate" },
    {
      FAKE_REVISION_IMAGE: "ghcr.io/example/legal@sha256:wrong",
      FAKE_REVISION_SCHEMA_MODE: "migrate",
    },
    { FAKE_REVISION_SCHEMA_MODE: "repair" },
    { FAKE_REVISION_SCHEMA_MODE: "migrate", FAKE_ACTIVE_MODE: "Single" },
  ]) {
    const result = await runRolloutCommand(
      ["create-and-promote-recovery-migrate", "revision-repair"],
      { FAKE_LATEST_REVISION: "revision-migrate", ...overrides },
    )
    assert.notEqual(result.status, 0)
    assert.equal(callIndex(result.calls, "ingress traffic set"), -1)
    assert.equal(callIndex(result.calls, "revision deactivate"), -1)
  }
})

test("legacy guard uses the exact count as a fail-closed result", async () => {
  const clean = await runRolloutCommand(["assert-no-legacy"], {
    FAKE_LEGACY_COUNT: "0",
  })
  const legacy = await runRolloutCommand(["assert-no-legacy"], {
    FAKE_LEGACY_COUNT: "2",
  })

  assert.equal(clean.status, 0, clean.stderr)
  assert.notEqual(legacy.status, 0)
  assert.match(legacy.stderr, /legacy article rows remain: 2/)
})
