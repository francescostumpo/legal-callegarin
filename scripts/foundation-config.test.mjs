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

test("the container build uses three immutable, exact toolchain stages", async () => {
  const dockerfile = await readRepositoryFile("Dockerfile")
  const fromLines = dockerfile.match(/^FROM .+$/gm) ?? []

  assert.deepEqual(fromLines, [
    "FROM node:24.21.0-bookworm-slim@sha256:2fe369e969550cde8e867afc3fe370b260140cab4a23d467074295b42163d553 AS frontend",
    "FROM golang:1.27.1-bookworm@sha256:648f440f42a0958804efb24df176f806f9d353b41f1c0627f666428e40310f6b AS builder",
    "FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab AS runtime",
  ])
  assert.match(dockerfile, /^COPY package\.json package-lock\.json \.\/$/m)
  assert.match(dockerfile, /^RUN npm ci$/m)
  assert.match(dockerfile, /^RUN npm run build$/m)
  assert.match(dockerfile, /^COPY go\.mod go\.sum \.\/$/m)
  assert.match(
    dockerfile,
    /COPY --from=frontend \/app\/internal\/webassets\/admin\/dist \.\/internal\/webassets\/admin\/dist/,
  )
  assert.ok(
    dockerfile.indexOf("COPY --from=frontend") <
      dockerfile.indexOf("RUN CGO_ENABLED=0 GOOS=linux"),
  )
  assert.match(
    dockerfile,
    /^RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w -X main\.buildVersion=\$\{VERSION\} -X main\.buildCommit=\$\{COMMIT\}" -o \/out\/legal-callegarin \.\/cmd\/web$/m,
  )
  assert.deepEqual(dockerfile.match(/^ARG .+$/gm), [
    "ARG VERSION",
    "ARG COMMIT",
  ])
  assert.doesNotMatch(
    dockerfile,
    /ARG .*?(?:SECRET|PASSWORD|TOKEN|KEY)|ENV .*?(?:SECRET|PASSWORD|TOKEN|KEY)/i,
  )
  assert.match(
    dockerfile,
    /^COPY --from=builder \/out\/legal-callegarin \/legal-callegarin$/m,
  )
  assert.match(dockerfile, /^USER 65532:65532$/m)
  assert.match(dockerfile, /^EXPOSE 8080$/m)
  assert.match(dockerfile, /^ENTRYPOINT \["\/legal-callegarin"\]$/m)
  assert.doesNotMatch(dockerfile, /^HEALTHCHECK/m)

  const runtimeStage = dockerfile.slice(dockerfile.lastIndexOf("FROM "))
  assert.equal((runtimeStage.match(/^COPY /gm) ?? []).length, 1)
})

test("the Docker context excludes local, generated, secret, and unrelated files", async () => {
  const dockerignore = await readRepositoryFile(".dockerignore")
  for (const pattern of [
    ".git",
    ".worktrees",
    ".superpowers",
    ".env",
    ".env.*",
    "node_modules",
    "internal/webassets/admin/dist",
    "bin",
    "coverage",
    "docs",
    "**/*_test.go",
    "**/*.test.*",
  ]) {
    assert.match(
      dockerignore,
      new RegExp(
        `^${pattern.replaceAll(".", "\\.").replaceAll("*", ".*")}$`,
        "m",
      ),
    )
  }
  assert.doesNotMatch(dockerignore, /^package(?:-lock)?\.json$/m)
  assert.doesNotMatch(dockerignore, /^go\.(?:mod|sum)$/m)
})

test("the local container smoke is bounded, hardened, and cleans only its own container", async () => {
  const [script, makefile] = await Promise.all([
    readRepositoryFile("scripts/container-smoke.sh"),
    readRepositoryFile("Makefile"),
  ])

  assert.match(script, /^set -eu$/m)
  assert.match(script, /SMOKE_CONTAINER=legal-callegarin-smoke-\$\$/)
  assert.match(script, /SMOKE_CONTAINER_ID=$/m)
  assert.match(script, /SMOKE_COMMIT=\$\(git rev-parse HEAD\)/)
  assert.match(
    script,
    /^SMOKE_STATUS=\$\(git status --porcelain --untracked-files=all\)$/m,
  )
  assert.doesNotMatch(script, /9084831/)
  assert.match(
    script,
    /docker build .*--build-arg VERSION=local-test .*--build-arg COMMIT="\$SMOKE_COMMIT"/,
  )
  assert.match(
    script,
    /docker create[\s\S]*--read-only[\s\S]*--cap-drop ALL[\s\S]*--security-opt no-new-privileges/,
  )
  assert.match(script, /SMOKE_CONTAINER_ID=\$\(docker create/)
  assert.match(script, /docker start "\$SMOKE_CONTAINER_ID"/)
  assert.ok(
    script.indexOf("SMOKE_CONTAINER_ID=$(docker create") <
      script.indexOf('docker start "$SMOKE_CONTAINER_ID"'),
  )
  assert.match(script, /-p 127\.0\.0\.1:18080:8080/)
  assert.match(script, /APP_ENV=development/)
  assert.match(script, /STORAGE_MODE=memory/)
  assert.match(script, /ADMIN_USERNAME=smoke-admin/)
  assert.match(script, /SMOKE_PASSWORD=smoke-password-non-production/)
  assert.match(script, /ADMIN_PASSWORD_HASH=/)
  assert.match(script, /SESSION_KEY_BASE64=/)
  assert.match(script, /PUBLIC_BASE_URL=\$SMOKE_ORIGIN/)
  assert.match(script, /trap cleanup EXIT INT TERM/)
  assert.match(script, /if \[ -n "\$SMOKE_CONTAINER_ID" \]; then/)
  assert.match(script, /docker rm -f "\$SMOKE_CONTAINER_ID"/)
  assert.doesNotMatch(script, /docker rm -f "\$SMOKE_CONTAINER"/)
  assert.doesNotMatch(script, /docker container inspect "\$SMOKE_CONTAINER"/)
  assert.doesNotMatch(
    script,
    /docker (?:system )?prune|docker rm -f \$\(|docker rm -f `|docker login|docker push/,
  )
  assert.match(script, /seq 1 60/)
  assert.doesNotMatch(script, /http [^\n]*\|[ \t]*grep/)
  assert.match(script, /http -fsS -o "\$SMOKE_DIR\/readiness"/)
  assert.match(script, /grep -qx 'ok' "\$SMOKE_DIR\/readiness"/)
  for (const route of [
    "/health/live",
    "/health/ready",
    "/",
    "/admin/login",
    "/api/admin/session",
  ]) {
    assert.ok(script.includes(route), `missing smoke route ${route}`)
  }
  assert.match(script, /grep -q '\"code\":\"authentication_required\"'/)
  assert.doesNotMatch(script, /\"code\":\"unauthenticated\"/)
  assert.match(script, /Content-Security-Policy/i)
  assert.match(script, /Strict-Transport-Security/i)
  assert.match(
    script,
    /^if ! grep -Eqi '\^Referrer-Policy:\[\[:space:\]\]\*same-origin\[\[:space:\]\]\*\$' "\$SMOKE_DIR\/public\.headers"; then$/m,
  )
  assert.match(
    script,
    /^  echo "public response Referrer-Policy is not exactly same-origin" >&2$/m,
  )
  assert.doesNotMatch(script, /Referrer-Policy:[^\n]*no-referrer/i)
  assert.match(script, /Permissions-Policy/i)
  assert.match(script, /X-Frame-Options/i)
  assert.match(script, /Set-Cookie/i)
  assert.match(script, /public_asset=.*site-\[0-9a-f\]/)
  const curlLines = script
    .split("\n")
    .filter((line) => /(^|\s)curl(?:\s|$)/.test(line))
  assert.deepEqual(curlLines, ['  curl --connect-timeout 2 --max-time 5 "$@"'])
  assert.match(script, /http -fsS "\$SMOKE_ORIGIN\$public_asset"/)
  assert.match(script, /started=.*name="started"/)
  assert.match(script, /test "\$login_status" = 303/)
  assert.match(script, /session_cookie=.*Set-Cookie/)
  assert.match(script, /admin_asset=.*\/admin\/assets\/index-/)
  assert.match(
    script,
    /http -fsS -H "Cookie: \$session_cookie" "\$SMOKE_ORIGIN\$admin_asset"/,
  )
  assert.match(script, /\"requestId\":\"\[\^\"\]\+\"/)
  assert.match(script, /\"fields\":\{\}/)
  assert.match(script, /docker stop .*"\$SMOKE_CONTAINER_ID"/)
  assert.match(script, /grep .*commit.*SMOKE_COMMIT/)
  assert.match(
    makefile,
    /^container-smoke:\n\t\.\/scripts\/container-smoke\.sh/m,
  )
})

test("the container smoke fails closed when Git status cannot prove cleanliness", async () => {
  const directory = await mkdtemp(join(tmpdir(), "legal-smoke-git-failure-"))
  const fakeBin = join(directory, "bin")
  const dockerLog = join(directory, "docker.log")
  await mkdir(fakeBin)
  await writeFile(
    join(fakeBin, "git"),
    `#!/bin/sh
if [ "$1 $2" = "rev-parse HEAD" ]; then
  echo 0123456789abcdef0123456789abcdef01234567
  exit 0
fi
if [ "$1" = "status" ]; then exit 42; fi
exit 43
`,
  )
  await writeFile(
    join(fakeBin, "docker"),
    `#!/bin/sh
echo called >> "$FAKE_DOCKER_LOG"
exit 99
`,
  )
  await Promise.all([
    chmod(join(fakeBin, "git"), 0o755),
    chmod(join(fakeBin, "docker"), 0o755),
  ])
  try {
    const result = spawnSync(
      "sh",
      [new URL("container-smoke.sh", import.meta.url).pathname],
      {
        encoding: "utf8",
        env: {
          ...process.env,
          PATH: `${fakeBin}:${process.env.PATH}`,
          FAKE_DOCKER_LOG: dockerLog,
        },
      },
    )
    assert.notEqual(result.status, 0)
    const dockerCalls = await readFile(dockerLog, "utf8").catch(() => "")
    assert.equal(dockerCalls, "")
  } finally {
    await rm(directory, { recursive: true, force: true })
  }
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
  assert.match(rolloutScript, /properties\.template\.revisionSuffix/)
  assert.doesNotMatch(rolloutScript, /CONTAINER_APP-\$repair_suffix/)
  assert.doesNotMatch(rolloutScript, /CONTAINER_APP-\$migrate_suffix/)
  assert.match(runbook, /Azure-returned revision name/i)
  assert.doesNotMatch(
    runbook,
    /\$CONTAINER_APP-(?:repair|migrate)-\$ROLLOUT_ID/,
  )
  assert.match(runbook, /ROLLOUT_ID=/)
  assert.match(rolloutScript, /properties\.configuration\.activeRevisionsMode/)
  assert.match(rolloutScript, /test "\$active_revisions_mode" = Multiple/)
})

const rolloutScriptPath = new URL("article-storage-rollout.sh", import.meta.url)
const validImageDigest = `ghcr.io/example/legal@sha256:${"a".repeat(64)}`
const rolloutID = "rollout01"
const repairSuffix = `repair-${rolloutID}`
const migrateSuffix = `migrate-${rolloutID}`
const repairRevision = `app-test--${repairSuffix}`
const migrateRevision = `app-test--${migrateSuffix}`
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
  else if (query.includes("properties.template.revisionSuffix")) {
    op = query.includes("repair-") ? "repair-suffix-check" : "migrate-suffix-check"
  } else if (query.startsWith("length([?name ==") && query.includes(".name)")) {
    op = query.includes(expectedRepair) ? "repair-created-exists" : "migrate-created-exists"
  } else if (query.startsWith("length([?name ==")) op = "repair-exists"
  else op = "observe"
} else if (startsWith("containerapp", "revision", "deactivate")) {
  const revision = option("--revision")
  op = revision === expectedRepair ? "repair-deactivate" : "deactivate:" + revision
} else if (startsWith("containerapp", "revision", "set-mode")) {
  op = "set-mode"
} else if (startsWith("containerapp", "revision", "show")) {
  const revision = option("--revision")
  op = revision === expectedMigrate || revision === process.env.FAKE_MIGRATE_UPDATE_REVISION
    ? "migrate-show"
    : "repair-show"
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
} else if (op === "repair-created-exists" || op === "migrate-created-exists") {
  const phase = op === "repair-created-exists" ? "REPAIR" : "MIGRATE"
  process.stdout.write((process.env["FAKE_" + phase + "_CREATED_EXISTS_COUNT"] || "1") + "\\n")
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
  const suffix = sequenceValue("FAKE_" + phase + "_SUFFIXES", process.env["FAKE_" + phase + "_SUFFIX"] || process.env["FAKE_EXPECTED_" + phase + "_SUFFIX"])
  process.stdout.write([active, health, image, mode, suffix].join("\\t") + "\\n")
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
      FAKE_EXPECTED_REPAIR_SUFFIX: repairSuffix,
      FAKE_EXPECTED_MIGRATE_SUFFIX: migrateSuffix,
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
    "repair-created-exists",
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
    `length([?properties.template.revisionSuffix == '${repairSuffix}'])`,
    "--output",
    "tsv",
  ])
  assert.deepEqual(callFor(result, "repair-created-exists").args, [
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
    "[[to_string(properties.active),properties.healthState,properties.template.containers[0].image,(properties.template.containers[0].env[?name==`ARTICLE_STORAGE_SCHEMA_MODE`].value|[0]),properties.template.revisionSuffix]]",
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
    "repair-created-exists",
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
    ["repair-created-exists", 11],
    ["observe", 12],
    ["repair-show", 13],
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
    { FAKE_REPAIR_SUFFIX: migrateSuffix },
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
    "migrate-created-exists",
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
    `length([?properties.template.revisionSuffix == '${migrateSuffix}'])`,
    "--output",
    "tsv",
  ])
  assert.deepEqual(callFor(result, "migrate-created-exists").args, [
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
    "[[to_string(properties.active),properties.healthState,properties.template.containers[0].image,(properties.template.containers[0].env[?name==`ARTICLE_STORAGE_SCHEMA_MODE`].value|[0]),properties.template.revisionSuffix]]",
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

test("Azure-returned revision names work with either documented separator shape", async () => {
  const singleRepairRevision = `app-test-${repairSuffix}`
  const singleMigrateRevision = `app-test-${migrateSuffix}`
  const repair = await runRolloutCommand(["quiesce-and-create-repair"], {
    FAKE_EXPECTED_REPAIR_REVISION: singleRepairRevision,
  })
  assert.equal(repair.status, 0, repair.stderr)
  assert.equal(repair.stdout.trim(), singleRepairRevision)
  assert.equal(
    callFor(repair, "repair-created-exists").args.includes(
      `length([?name == '${singleRepairRevision}'].name)`,
    ),
    true,
  )

  const migrate = await runRolloutCommand(
    ["create-and-promote-recovery-migrate", singleRepairRevision],
    {
      FAKE_EXPECTED_REPAIR_REVISION: singleRepairRevision,
      FAKE_EXPECTED_MIGRATE_REVISION: singleMigrateRevision,
    },
  )
  assert.equal(migrate.status, 0, migrate.stderr)
  assert.equal(migrate.stdout.trim(), singleMigrateRevision)
  assert.equal(
    callFor(migrate, "migrate-created-exists").args.includes(
      `length([?name == '${singleMigrateRevision}'].name)`,
    ),
    true,
  )
})

test("repair revision provenance and first property gate precede migrate update", async () => {
  const foreign = await runRolloutCommand(
    ["create-and-promote-recovery-migrate", "other-app--repair-rollout01"],
    { FAKE_REPAIR_EXISTS_COUNT: "0" },
  )
  assert.notEqual(foreign.status, 0)
  assert.deepEqual(operations(foreign), ["repair-exists"])

  const wrongSuffix = await runRolloutCommand(
    ["create-and-promote-recovery-migrate", "app-test--opaque-revision"],
    { FAKE_REPAIR_SUFFIX: migrateSuffix },
  )
  assert.notEqual(wrongSuffix.status, 0)
  assert.deepEqual(operations(wrongSuffix), ["repair-exists", "repair-show"])

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
    { FAKE_REPAIR_SUFFIX: migrateSuffix },
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
    { FAKE_REPAIR_SUFFIXES: `${repairSuffix},${migrateSuffix}` },
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
    "migrate-created-exists",
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
    ["migrate-created-exists", 7],
    ["observe", 8],
    ["migrate-show", 9],
    ["set-mode#2", 10],
    ["mode-show#2", 11],
    ["traffic-set", 12],
    ["repair-show#2", 13],
    ["repair-deactivate", 14],
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
    { FAKE_MIGRATE_SUFFIX: repairSuffix },
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

test("same suffix, unlisted response, suffix mismatch, and inactive retry fail closed", async () => {
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

  const unlisted = await runRolloutCommand(
    ["create-and-promote-recovery-migrate", repairRevision],
    { FAKE_MIGRATE_CREATED_EXISTS_COUNT: "0" },
  )
  assert.notEqual(unlisted.status, 0)
  assert.equal(operations(unlisted).at(-1), "migrate-created-exists")
  assert.equal(operations(unlisted).includes("traffic-set"), false)
  assert.equal(operations(unlisted).includes("migrate-show"), false)

  const wrongReturnedSuffix = await runRolloutCommand(
    ["create-and-promote-recovery-migrate", repairRevision],
    {
      FAKE_MIGRATE_UPDATE_REVISION: "app-test--migrate-stale",
      FAKE_MIGRATE_SUFFIX: "migrate-stale",
    },
  )
  assert.notEqual(wrongReturnedSuffix.status, 0)
  assert.equal(operations(wrongReturnedSuffix).at(-1), "migrate-show")
  assert.equal(operations(wrongReturnedSuffix).includes("traffic-set"), false)

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
      FAKE_EXPECTED_MIGRATE_REVISION: "app-test--migrate-rollout02",
    },
  )
  assert.notEqual(newRollout.status, 0)
  assert.deepEqual(operations(newRollout), ["repair-exists", "repair-show"])
  assert.equal(operations(newRollout).includes("migrate-update"), false)
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
