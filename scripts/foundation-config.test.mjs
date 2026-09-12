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
  assert.match(rolloutScript, /--revision-suffix "\$revision_suffix"/)
  assert.match(rolloutScript, /containerapp revision list[\s\S]*--all/)
  assert.match(rolloutScript, /--query properties\.latestRevisionName/)
  assert.doesNotMatch(rolloutScript, /latest_revision_name\(\)/)
  assert.match(rolloutScript, /test "\$revision_active" = true/)
  assert.match(runbook, /ROLLOUT_ID=/)
  assert.match(rolloutScript, /properties\.configuration\.activeRevisionsMode/)
  assert.match(rolloutScript, /test "\$active_revisions_mode" = Multiple/)
})

const rolloutScriptPath = new URL("article-storage-rollout.sh", import.meta.url)
const validImageDigest = `ghcr.io/example/legal@sha256:${"a".repeat(64)}`
const rolloutID = "rollout01"
const repairSuffix = `repair-${rolloutID}`
const migrateSuffix = `migrate-${rolloutID}`
const repairRevision = `app-test-${repairSuffix}`
const migrateRevision = `app-test-${migrateSuffix}`
const appArguments = ["--resource-group", "rg-test", "--name", "app-test"]

async function runRolloutCommand(args, overrides = {}) {
  const directory = await mkdtemp(join(tmpdir(), "legal-callegarin-rollout-"))
  const fakeBin = join(directory, "bin")
  const log = join(directory, "calls.log")
  await writeFile(log, "")
  await mkdir(fakeBin)

  const fakeAz = `#!/usr/bin/env node
const fs = require("node:fs")
const args = process.argv.slice(2)
const option = (name) => {
  const index = args.indexOf(name)
  return index === -1 ? undefined : args[index + 1]
}
const startsWith = (...prefix) => prefix.every((value, index) => args[index] === value)
const expectedRepair = process.env.FAKE_EXPECTED_REPAIR_REVISION
const expectedMigrate = process.env.FAKE_EXPECTED_MIGRATE_REVISION
let op
if (startsWith("storage", "entity", "query")) {
  op = "legacy-query"
} else if (startsWith("containerapp", "revision", "list")) {
  const query = option("--query") || ""
  if (query === "[?properties.active].name") op = "active-list"
  else if (query === "length([?properties.active])") op = "residual-count"
  else if (query.includes(".name)")) {
    op = query.includes("repair-") ? "repair-suffix-check" : "migrate-suffix-check"
  } else if (query.startsWith("length([?name ==")) op = "repair-exists"
  else op = "observe"
} else if (startsWith("containerapp", "revision", "deactivate")) {
  const revision = option("--revision")
  op = revision === expectedRepair ? "repair-deactivate" : "deactivate:" + revision
} else if (startsWith("containerapp", "revision", "set-mode")) {
  op = "set-mode"
} else if (startsWith("containerapp", "revision", "show")) {
  op = option("--revision") === expectedRepair ? "repair-show" : "migrate-show"
} else if (startsWith("containerapp", "show")) {
  op = option("--query").includes("configuration.ingress.traffic") ? "traffic-show" : "mode-show"
} else if (startsWith("containerapp", "update")) {
  op = option("--set-env-vars") === "ARTICLE_STORAGE_SCHEMA_MODE=repair" ? "repair-update" : "migrate-update"
} else if (startsWith("containerapp", "ingress", "traffic", "set")) {
  op = "traffic-set"
} else {
  process.stderr.write("unexpected fake az argv: " + JSON.stringify(args) + "\\n")
  process.exit(97)
}
const existing = fs.readFileSync(process.env.FAKE_CALL_LOG, "utf8")
  .split("\\n")
  .filter(Boolean)
  .map((line) => JSON.parse(line))
const occurrence = existing.filter((call) => call.op === op).length + 1
fs.appendFileSync(process.env.FAKE_CALL_LOG, JSON.stringify({ tool: "az", op, args }) + "\\n")
const failAt = process.env.FAKE_FAIL_AT || ""
if (failAt === op || failAt === op + "#" + occurrence) process.exit(42)
const sequenceValue = (name, fallback) => {
  const values = (process.env[name] || "").split(",").filter(Boolean)
  return values[occurrence - 1] || fallback
}
if (op === "legacy-query") {
  process.stdout.write((process.env.FAKE_LEGACY_COUNT || "0") + "\\n")
} else if (op === "active-list") {
  process.stdout.write((process.env.FAKE_ACTIVE_NAMES || "") + "\\n")
} else if (op === "residual-count") {
  process.stdout.write((process.env.FAKE_RESIDUAL_COUNT || "0") + "\\n")
} else if (op.endsWith("suffix-check")) {
  process.stdout.write(sequenceValue("FAKE_SUFFIX_COUNTS", "0") + "\\n")
} else if (op === "repair-exists") {
  process.stdout.write((process.env.FAKE_REPAIR_EXISTS_COUNT || "1") + "\\n")
} else if (op === "mode-show") {
  process.stdout.write(sequenceValue("FAKE_ACTIVE_MODES", "Multiple") + "\\n")
} else if (op === "traffic-show") {
  const state = sequenceValue("FAKE_TRAFFIC_STATES", "1:0")
  process.stdout.write(state.replace(":", "\\t") + "\\n")
} else if (op === "repair-update") {
  process.stdout.write((process.env.FAKE_REPAIR_UPDATE_REVISION || expectedRepair) + "\\n")
} else if (op === "migrate-update") {
  process.stdout.write((process.env.FAKE_MIGRATE_UPDATE_REVISION || expectedMigrate) + "\\n")
} else if (op === "repair-show" || op === "migrate-show") {
  const phase = op === "repair-show" ? "REPAIR" : "MIGRATE"
  const active = sequenceValue("FAKE_" + phase + "_ACTIVES", process.env["FAKE_" + phase + "_ACTIVE"] || "true")
  const health = sequenceValue("FAKE_" + phase + "_HEALTHS", process.env["FAKE_" + phase + "_HEALTH"] || "Healthy")
  const image = sequenceValue("FAKE_" + phase + "_IMAGES", process.env["FAKE_" + phase + "_IMAGE"] || process.env.IMAGE_DIGEST)
  const mode = sequenceValue("FAKE_" + phase + "_MODES", process.env["FAKE_" + phase + "_MODE"] || phase.toLowerCase())
  process.stdout.write([active, health, image, mode].join("\\t") + "\\n")
}
`
  const fakeSleep = `#!/usr/bin/env node
const fs = require("node:fs")
const args = process.argv.slice(2)
fs.appendFileSync(process.env.FAKE_CALL_LOG, JSON.stringify({ tool: "sleep", op: "sleep", args }) + "\\n")
if ((process.env.FAKE_FAIL_AT || "") === "sleep") process.exit(42)
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
      IMAGE_DIGEST: validImageDigest,
      ROLLOUT_ID: rolloutID,
      FAKE_EXPECTED_REPAIR_REVISION: repairRevision,
      FAKE_EXPECTED_MIGRATE_REVISION: migrateRevision,
      ...overrides,
    },
  })
  const calls = await readFile(log, "utf8")
  await rm(directory, { recursive: true, force: true })
  return {
    ...result,
    calls: calls
      .trim()
      .split("\n")
      .filter(Boolean)
      .map((line) => JSON.parse(line)),
  }
}

function operations(result) {
  return result.calls.map((call) => call.op)
}

function callFor(result, operation, occurrence = 1) {
  const matches = result.calls.filter((call) => call.op === operation)
  assert.ok(matches.length >= occurrence, `missing ${operation} #${occurrence}`)
  return matches[occurrence - 1]
}

test("repair success uses exact argv and safe operation order", async () => {
  const result = await runRolloutCommand(["quiesce-and-create-repair"], {
    FAKE_ACTIVE_NAMES: "revision-a\nrevision-b",
    FAKE_RESIDUAL_COUNT: "0",
  })

  assert.equal(result.status, 0, result.stderr)
  assert.equal(result.stdout.trim(), repairRevision)
  assert.deepEqual(operations(result), [
    "traffic-show",
    "active-list",
    "deactivate:revision-a",
    "deactivate:revision-b",
    "residual-count",
    "sleep",
    "set-mode",
    "mode-show",
    "repair-suffix-check",
    "traffic-show",
    "repair-update",
    "observe",
    "repair-show",
  ])
  assert.deepEqual(callFor(result, "sleep").args, ["30"])
  assert.deepEqual(callFor(result, "set-mode").args, [
    "containerapp",
    "revision",
    "set-mode",
    ...appArguments,
    "--mode",
    "multiple",
  ])
  assert.deepEqual(callFor(result, "repair-update").args, [
    "containerapp",
    "update",
    ...appArguments,
    "--image",
    validImageDigest,
    "--revision-suffix",
    repairSuffix,
    "--set-env-vars",
    "ARTICLE_STORAGE_SCHEMA_MODE=repair",
    "--query",
    "properties.latestRevisionName",
    "--output",
    "tsv",
  ])
  assert.deepEqual(callFor(result, "mode-show").args, [
    "containerapp",
    "show",
    ...appArguments,
    "--query",
    "properties.configuration.activeRevisionsMode",
    "--output",
    "tsv",
  ])
  assert.deepEqual(callFor(result, "repair-suffix-check").args, [
    "containerapp",
    "revision",
    "list",
    ...appArguments,
    "--all",
    "--query",
    `length([?name == '${repairRevision}'].name)`,
    "--output",
    "tsv",
  ])
  assert.deepEqual(callFor(result, "traffic-show").args, [
    "containerapp",
    "show",
    ...appArguments,
    "--query",
    "[[length(properties.configuration.ingress.traffic),length(properties.configuration.ingress.traffic[?latestRevision == `true` || revisionName == `null` || revisionName == ''])]]",
    "--output",
    "tsv",
  ])
  assert.deepEqual(callFor(result, "repair-show").args, [
    "containerapp",
    "revision",
    "show",
    ...appArguments,
    "--revision",
    repairRevision,
    "--query",
    "[[to_string(properties.active),properties.healthState,properties.template.containers[0].image,(properties.template.containers[0].env[?name==`ARTICLE_STORAGE_SCHEMA_MODE`].value|[0])]]",
    "--output",
    "tsv",
  ])
  assert.deepEqual(callFor(result, "deactivate:revision-a").args, [
    "containerapp",
    "revision",
    "deactivate",
    ...appArguments,
    "--revision",
    "revision-a",
  ])
  assert.deepEqual(callFor(result, "deactivate:revision-b").args, [
    "containerapp",
    "revision",
    "deactivate",
    ...appArguments,
    "--revision",
    "revision-b",
  ])
})

test("repair failure matrix stops before every subsequent operation", async () => {
  const sequence = [
    "traffic-show",
    "active-list",
    "deactivate:revision-a",
    "deactivate:revision-b",
    "residual-count",
    "sleep",
    "set-mode",
    "mode-show",
    "repair-suffix-check",
    "traffic-show",
    "repair-update",
    "observe",
    "repair-show",
  ]
  const failures = [
    ["traffic-show#1", 0],
    ["active-list", 1],
    ["deactivate:revision-a", 2],
    ["deactivate:revision-b", 3],
    ["residual-count", 4],
    ["sleep", 5],
    ["set-mode", 6],
    ["mode-show", 7],
    ["repair-suffix-check", 8],
    ["traffic-show#2", 9],
    ["repair-update", 10],
    ["observe", 11],
    ["repair-show", 12],
  ]
  for (const [failedOperation, lastIndex] of failures) {
    const result = await runRolloutCommand(["quiesce-and-create-repair"], {
      FAKE_ACTIVE_NAMES: "revision-a\nrevision-b",
      FAKE_FAIL_AT: failedOperation,
    })
    assert.notEqual(result.status, 0, failedOperation)
    assert.deepEqual(
      operations(result),
      sequence.slice(0, lastIndex + 1),
      failedOperation,
    )
  }
})

test("repair rejects residual writers and every invalid revision property", async () => {
  const residual = await runRolloutCommand(["quiesce-and-create-repair"], {
    FAKE_ACTIVE_NAMES: "revision-a\nrevision-b",
    FAKE_RESIDUAL_COUNT: "1",
  })
  assert.notEqual(residual.status, 0)
  assert.deepEqual(operations(residual), [
    "traffic-show",
    "active-list",
    "deactivate:revision-a",
    "deactivate:revision-b",
    "residual-count",
  ])

  const implicitTraffic = await runRolloutCommand(
    ["quiesce-and-create-repair"],
    {
      FAKE_ACTIVE_MODES: "Single",
      FAKE_TRAFFIC_STATES: "1:1",
    },
  )
  assert.notEqual(implicitTraffic.status, 0)
  assert.deepEqual(operations(implicitTraffic), ["traffic-show"])

  const emptyTraffic = await runRolloutCommand(["quiesce-and-create-repair"], {
    FAKE_TRAFFIC_STATES: "0:0",
  })
  assert.notEqual(emptyTraffic.status, 0)
  assert.deepEqual(operations(emptyTraffic), ["traffic-show"])

  const singleMode = await runRolloutCommand(["quiesce-and-create-repair"], {
    FAKE_ACTIVE_MODES: "Single",
  })
  assert.notEqual(singleMode.status, 0)
  assert.equal(operations(singleMode).includes("repair-update"), false)

  const existingSuffix = await runRolloutCommand(
    ["quiesce-and-create-repair"],
    { FAKE_SUFFIX_COUNTS: "1" },
  )
  assert.notEqual(existingSuffix.status, 0)
  assert.equal(operations(existingSuffix).includes("repair-update"), false)

  for (const overrides of [
    { FAKE_REPAIR_ACTIVE: "false" },
    { FAKE_REPAIR_HEALTH: "Unhealthy" },
    { FAKE_REPAIR_IMAGE: `ghcr.io/example/legal@sha256:${"b".repeat(64)}` },
    { FAKE_REPAIR_MODE: "migrate" },
  ]) {
    const result = await runRolloutCommand(
      ["quiesce-and-create-repair"],
      overrides,
    )
    assert.notEqual(result.status, 0)
    assert.equal(operations(result).at(-1), "repair-show")
    assert.equal(result.stdout, "")
  }
})

test("recovery success ties the exact update response to health and promotion", async () => {
  const result = await runRolloutCommand([
    "create-and-promote-recovery-migrate",
    repairRevision,
  ])

  assert.equal(result.status, 0, result.stderr)
  assert.equal(result.stdout.trim(), migrateRevision)
  assert.deepEqual(operations(result), [
    "repair-exists",
    "repair-show",
    "set-mode",
    "mode-show",
    "migrate-suffix-check",
    "traffic-show",
    "migrate-update",
    "observe",
    "migrate-show",
    "set-mode",
    "mode-show",
    "traffic-set",
    "repair-show",
    "repair-deactivate",
  ])
  assert.deepEqual(callFor(result, "migrate-update").args, [
    "containerapp",
    "update",
    ...appArguments,
    "--image",
    validImageDigest,
    "--revision-suffix",
    migrateSuffix,
    "--set-env-vars",
    "ARTICLE_STORAGE_SCHEMA_MODE=migrate",
    "--query",
    "properties.latestRevisionName",
    "--output",
    "tsv",
  ])
  assert.deepEqual(callFor(result, "traffic-set").args, [
    "containerapp",
    "ingress",
    "traffic",
    "set",
    ...appArguments,
    "--revision-weight",
    `${migrateRevision}=100`,
  ])
  assert.deepEqual(callFor(result, "migrate-suffix-check").args, [
    "containerapp",
    "revision",
    "list",
    ...appArguments,
    "--all",
    "--query",
    `length([?name == '${migrateRevision}'].name)`,
    "--output",
    "tsv",
  ])
  assert.deepEqual(callFor(result, "repair-exists").args, [
    "containerapp",
    "revision",
    "list",
    ...appArguments,
    "--all",
    "--query",
    `length([?name == '${repairRevision}'])`,
    "--output",
    "tsv",
  ])
  assert.deepEqual(callFor(result, "migrate-show").args, [
    "containerapp",
    "revision",
    "show",
    ...appArguments,
    "--revision",
    migrateRevision,
    "--query",
    "[[to_string(properties.active),properties.healthState,properties.template.containers[0].image,(properties.template.containers[0].env[?name==`ARTICLE_STORAGE_SCHEMA_MODE`].value|[0])]]",
    "--output",
    "tsv",
  ])
  assert.deepEqual(callFor(result, "repair-deactivate").args, [
    "containerapp",
    "revision",
    "deactivate",
    ...appArguments,
    "--revision",
    repairRevision,
  ])
})

test("repair revision provenance and first property gate precede migrate update", async () => {
  for (const invalidRevision of [
    "other-app-repair-rollout01",
    migrateRevision,
  ]) {
    const result = await runRolloutCommand(
      ["create-and-promote-recovery-migrate", invalidRevision],
      { FAKE_EXPECTED_REPAIR_REVISION: invalidRevision },
    )
    assert.notEqual(result.status, 0)
    assert.deepEqual(result.calls, [])
  }

  const missing = await runRolloutCommand(
    ["create-and-promote-recovery-migrate", repairRevision],
    { FAKE_REPAIR_EXISTS_COUNT: "0" },
  )
  assert.notEqual(missing.status, 0)
  assert.deepEqual(operations(missing), ["repair-exists"])

  for (const overrides of [
    { FAKE_REPAIR_ACTIVE: "false" },
    { FAKE_REPAIR_HEALTH: "Unhealthy" },
    { FAKE_REPAIR_IMAGE: `ghcr.io/example/legal@sha256:${"b".repeat(64)}` },
    { FAKE_REPAIR_MODE: "migrate" },
  ]) {
    const result = await runRolloutCommand(
      ["create-and-promote-recovery-migrate", repairRevision],
      overrides,
    )
    assert.notEqual(result.status, 0)
    assert.deepEqual(operations(result), ["repair-exists", "repair-show"])
  }
})

test("repair revision is reverified immediately before final deactivation", async () => {
  for (const overrides of [
    { FAKE_REPAIR_ACTIVES: "true,false" },
    { FAKE_REPAIR_HEALTHS: "Healthy,Unhealthy" },
    {
      FAKE_REPAIR_IMAGES: `${validImageDigest},ghcr.io/example/legal@sha256:${"b".repeat(64)}`,
    },
    { FAKE_REPAIR_MODES: "repair,migrate" },
  ]) {
    const result = await runRolloutCommand(
      ["create-and-promote-recovery-migrate", repairRevision],
      overrides,
    )
    assert.notEqual(result.status, 0)
    assert.equal(operations(result).includes("traffic-set"), true)
    assert.equal(operations(result).at(-1), "repair-show")
    assert.equal(operations(result).includes("repair-deactivate"), false)
  }
})

test("recovery command failure matrix suppresses all later operations", async () => {
  const sequence = [
    "repair-exists",
    "repair-show",
    "set-mode",
    "mode-show",
    "migrate-suffix-check",
    "traffic-show",
    "migrate-update",
    "observe",
    "migrate-show",
    "set-mode",
    "mode-show",
    "traffic-set",
    "repair-show",
    "repair-deactivate",
  ]
  const failures = [
    ["repair-exists", 0],
    ["repair-show#1", 1],
    ["set-mode#1", 2],
    ["mode-show#1", 3],
    ["migrate-suffix-check", 4],
    ["traffic-show", 5],
    ["migrate-update", 6],
    ["observe", 7],
    ["migrate-show", 8],
    ["set-mode#2", 9],
    ["mode-show#2", 10],
    ["traffic-set", 11],
    ["repair-show#2", 12],
    ["repair-deactivate", 13],
  ]
  for (const [failure, lastIndex] of failures) {
    const result = await runRolloutCommand(
      ["create-and-promote-recovery-migrate", repairRevision],
      { FAKE_FAIL_AT: failure },
    )
    assert.notEqual(result.status, 0, failure)
    assert.deepEqual(
      operations(result),
      sequence.slice(0, lastIndex + 1),
      failure,
    )
  }
})

test("recovery property and mode failures never mutate traffic", async () => {
  for (const overrides of [
    { FAKE_MIGRATE_ACTIVE: "false" },
    { FAKE_MIGRATE_HEALTH: "Unhealthy" },
    { FAKE_MIGRATE_IMAGE: `ghcr.io/example/legal@sha256:${"b".repeat(64)}` },
    { FAKE_MIGRATE_MODE: "repair" },
    { FAKE_ACTIVE_MODES: "Single" },
    { FAKE_ACTIVE_MODES: "Multiple,Single" },
  ]) {
    const result = await runRolloutCommand(
      ["create-and-promote-recovery-migrate", repairRevision],
      overrides,
    )
    assert.notEqual(result.status, 0)
    assert.equal(operations(result).includes("traffic-set"), false)
    assert.equal(operations(result).includes("repair-deactivate"), false)
  }

  const singleBeforeUpdate = await runRolloutCommand(
    ["create-and-promote-recovery-migrate", repairRevision],
    { FAKE_ACTIVE_MODES: "Single" },
  )
  assert.equal(operations(singleBeforeUpdate).includes("migrate-update"), false)

  const implicitTraffic = await runRolloutCommand(
    ["create-and-promote-recovery-migrate", repairRevision],
    { FAKE_TRAFFIC_STATES: "1:1" },
  )
  assert.notEqual(implicitTraffic.status, 0)
  assert.equal(operations(implicitTraffic).includes("migrate-update"), false)
})

test("same suffix, stale response, and inactive no-change retry fail closed", async () => {
  const sameSuffix = await runRolloutCommand(
    ["create-and-promote-recovery-migrate", repairRevision],
    { FAKE_SUFFIX_COUNTS: "1" },
  )
  assert.notEqual(sameSuffix.status, 0)
  assert.deepEqual(operations(sameSuffix), [
    "repair-exists",
    "repair-show",
    "set-mode",
    "mode-show",
    "migrate-suffix-check",
  ])

  const stale = await runRolloutCommand(
    ["create-and-promote-recovery-migrate", repairRevision],
    { FAKE_MIGRATE_UPDATE_REVISION: "app-test-migrate-stale" },
  )
  assert.notEqual(stale.status, 0)
  assert.equal(operations(stale).includes("traffic-set"), false)
  assert.equal(operations(stale).includes("migrate-show"), false)

  const inactive = await runRolloutCommand(
    ["create-and-promote-recovery-migrate", repairRevision],
    { FAKE_MIGRATE_ACTIVE: "false" },
  )
  assert.notEqual(inactive.status, 0)
  assert.equal(operations(inactive).at(-1), "migrate-show")
  assert.equal(operations(inactive).includes("traffic-set"), false)

  const newRollout = await runRolloutCommand(
    ["create-and-promote-recovery-migrate", repairRevision],
    {
      ROLLOUT_ID: "rollout02",
      FAKE_EXPECTED_MIGRATE_REVISION: "app-test-migrate-rollout02",
    },
  )
  assert.equal(newRollout.status, 0, newRollout.stderr)
  assert.equal(newRollout.stdout.trim(), "app-test-migrate-rollout02")
})

test("image digest and rollout ID validation reject unsafe inputs before Azure", async () => {
  for (const image of [
    "ghcr.io/example/legal:latest",
    "ghcr.io/example/legal@sha256:abcd",
    `ghcr.io/example/legal@sha256:${"g".repeat(64)}`,
    `example.azurecr.io/legal@sha256:${"a".repeat(64)}`,
    `ghcr.io/Example/legal@sha256:${"a".repeat(64)}`,
    `ghcr.io/example/legal@sha256:${"A".repeat(64)}`,
  ]) {
    const result = await runRolloutCommand(["quiesce-and-create-repair"], {
      IMAGE_DIGEST: image,
    })
    assert.notEqual(result.status, 0, image)
    assert.deepEqual(result.calls, [], image)
  }
  for (const id of [
    "",
    "UPPER",
    "1leading",
    "-leading",
    "trailing-",
    "a--b",
    "bad_id",
    "bad/id",
    "too-long-rollout-id",
  ]) {
    const result = await runRolloutCommand(["quiesce-and-create-repair"], {
      ROLLOUT_ID: id,
    })
    assert.notEqual(result.status, 0, id)
    assert.deepEqual(result.calls, [], id)
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
  assert.deepEqual(operations(clean), ["legacy-query"])
  assert.deepEqual(operations(legacy), ["legacy-query"])

  const queryFailure = await runRolloutCommand(["assert-no-legacy"], {
    FAKE_FAIL_AT: "legacy-query",
  })
  assert.notEqual(queryFailure.status, 0)
  assert.deepEqual(operations(queryFailure), ["legacy-query"])
})
