import assert from "node:assert/strict"
import { readFileSync } from "node:fs"
import { resolve } from "node:path"
import test from "node:test"
import { main, selectRetentionCandidates } from "./prune-ghcr-versions.mjs"

const script = resolve("scripts/prune-ghcr-versions.mjs")
const subscription = "00000000-0000-0000-0000-000000000000"
const resourceGroup = "rg-callegarin"
const appName = "callegarin"
const revisionName = "callegarin--production"
const token = "TOP_SECRET_GH_TOKEN"
const baseArgs = [
  "--subscription-id",
  subscription,
  "--resource-group",
  resourceGroup,
  "--container-app-name",
  appName,
  "--dry-run",
]
const acceptHeader = "Accept: application/vnd.github+json"
const versionHeader = "X-GitHub-Api-Version: 2026-03-10"
const imagePrefix = "ghcr.io/francescostumpo/legal-callegarin@"
const digest = (value) => `sha256:${value.toString(16).padStart(64, "0")}`
const appQuery =
  "{id:id,name:name,location:location,provisioningState:properties.provisioningState,latestRevisionName:properties.latestRevisionName,latestReadyRevisionName:properties.latestReadyRevisionName,activeRevisionsMode:properties.configuration.activeRevisionsMode,external:properties.configuration.ingress.external,allowInsecure:properties.configuration.ingress.allowInsecure,fqdn:properties.configuration.ingress.fqdn,traffic:properties.configuration.ingress.traffic,template:properties.template}"
const revisionQuery =
  "{id:id,name:name,active:properties.active,healthState:properties.healthState,provisioningState:properties.provisioningState,runningState:properties.runningState,fqdn:properties.fqdn,trafficWeight:properties.trafficWeight,template:properties.template}"

function createStatefulRunner(scenario, state, calls) {
  const appPath = `/subscriptions/${subscription}/resourceGroups/${resourceGroup}/providers/Microsoft.App/containerApps/${appName}`
  const revisionPath = `${appPath}/revisions/${revisionName}`
  const stamp = (index) =>
    new Date(Date.UTC(2020, 0, 1) - index * 1000).toISOString()
  const makeVersion = (index) => ({
    id: index,
    name: digest(index),
    created_at: stamp(index),
    updated_at: stamp(index),
    metadata: {
      package_type: "container",
      container: {
        tags:
          index % 2 === 0
            ? [`sha-${index.toString(16).padStart(40, "0")}`]
            : [],
      },
    },
  })
  const makeVersions = () => {
    const count =
      scenario === "multi-page"
        ? 101
        : scenario === "delete-cap"
          ? 112
          : scenario === "no-candidates"
            ? 10
            : 13
    let values = Array.from({ length: count }, (_, index) =>
      makeVersion(index + 1),
    )
    if (scenario === "version-cap")
      values = Array.from({ length: 101 }, (_, index) => makeVersion(index + 1))
    if (scenario === "item-id") values[0].id = 0
    if (scenario === "item-digest") values[0].name = "legacy"
    if (scenario === "item-time") values[0].created_at = "bad"
    if (scenario === "future-time")
      values[0].updated_at = "2999-01-01T00:00:00.000Z"
    if (scenario === "update-before-create")
      values[0].updated_at = "2019-01-01T00:00:00.000Z"
    if (scenario === "duplicate-id") values[1].id = values[0].id
    if (scenario === "duplicate-digest") values[1].name = values[0].name
    if (scenario === "duplicate-tag")
      values[0].metadata.container.tags = [
        `sha-${"a".repeat(40)}`,
        `sha-${"a".repeat(40)}`,
      ]
    if (scenario === "empty-tag") values[0].metadata.container.tags = [""]
    if (scenario === "unknown-tag")
      values[0].metadata.container.tags = ["latest"]
    if (scenario === "metadata-type") values[0].metadata.package_type = "npm"
    if (scenario === "tags-shape") values[0].metadata.container.tags = "bad"
    if (scenario === "missing-current")
      values = values.filter((item) => item.name !== digest(13))
    if (scenario === "pre-gh-drift" && state.snapshotReads >= 2)
      values[0].updated_at = "2020-01-02T00:00:00.000Z"
    if (scenario === "final-gh-drift" && state.snapshotReads >= 3)
      values.push(makeVersion(500))
    if (scenario === "final-current-missing" && state.snapshotReads >= 3)
      values = values.filter((item) => item.name !== digest(13))
    return values.filter((item) => !state.deleted.includes(item.id))
  }
  return (command, args, label) => {
    calls.push([command, ...args])
    if (args[0] === "--version") return ""
    if (command === "az") {
      if (scenario === "az-command-error" && args[0] === "account")
        throw new Error(`${label} failed`)
      if (args[0] === "account" && args[1] === "show")
        return JSON.stringify({
          id:
            scenario === "wrong-account"
              ? "11111111-1111-1111-1111-111111111111"
              : subscription,
          state: "Enabled",
        })
      if (args[0] === "group" && args[1] === "show")
        return scenario === "wrong-rg"
          ? appPath
          : `/subscriptions/${subscription}/resourceGroups/${resourceGroup}`
      if (args[0] !== "rest") throw new Error(`${label} failed`)
      const option = (name) => args[args.indexOf(name) + 1]
      const url = new URL(option("--url"))
      if (
        option("--method") !== "get" ||
        option("--output") !== "json" ||
        !option("--query") ||
        url.searchParams.get("api-version") !== "2026-01-01"
      )
        throw new Error(`${label} failed`)
      if (url.pathname.toLowerCase() === appPath.toLowerCase()) {
        state.azureReads += 1
        if (scenario === "az-malformed-json") return "{LEAKME"
        let traffic = [{ revisionName, weight: 100 }]
        if (scenario === "traffic-sum") traffic = [{ revisionName, weight: 90 }]
        if (scenario === "traffic-latest")
          traffic = [{ revisionName, weight: 100, latestRevision: true }]
        if (scenario === "traffic-label")
          traffic = [{ revisionName, weight: 100, label: "stable" }]
        if (scenario === "traffic-multiple")
          traffic = [
            { revisionName, weight: 50 },
            { revisionName: "callegarin--other", weight: 50 },
          ]
        if (scenario === "traffic-zero-coexists")
          traffic.push({ revisionName: "callegarin--prior", weight: 0 })
        if (scenario === "unsafe-revision")
          traffic = [{ revisionName: "../bad", weight: 100 }]
        return JSON.stringify({
          id: scenario === "app-id" ? `${appPath}-wrong` : appPath,
          name: appName,
          location: "italynorth",
          provisioningState:
            scenario === "app-provisioning" ? "Failed" : "Succeeded",
          latestRevisionName: revisionName,
          latestReadyRevisionName: revisionName,
          activeRevisionsMode: scenario === "app-mode" ? "Single" : "Multiple",
          external: scenario === "app-external" ? false : true,
          allowInsecure: scenario === "app-insecure" ? true : false,
          fqdn: "callegarin.example.azurecontainerapps.io",
          traffic,
          template: {
            containers: [
              { name: appName, image: `${imagePrefix}${digest(13)}` },
            ],
          },
        })
      }
      if (url.pathname.toLowerCase() === revisionPath.toLowerCase()) {
        state.revisionReads += 1
        const routedNumber =
          scenario === "no-candidates"
            ? 10
            : scenario === "pre-azure-drift" && state.revisionReads >= 2
              ? 999
              : scenario === "final-azure-drift" && state.revisionReads >= 3
                ? 998
                : 13
        let environment = [
          { name: "APP_ENV", value: "production" },
          { name: "ARTICLE_STORAGE_SCHEMA_MODE", value: "migrate" },
        ]
        if (scenario === "schema") environment[1].value = "compat"
        if (scenario === "schema-duplicate")
          environment.push({
            name: "ARTICLE_STORAGE_SCHEMA_MODE",
            value: "migrate",
          })
        if (scenario === "schema-secret")
          environment[1] = {
            name: "ARTICLE_STORAGE_SCHEMA_MODE",
            secretRef: "secret",
          }
        const containers = [
          {
            name: appName,
            image: `${imagePrefix}${digest(routedNumber)}`,
            env: environment,
          },
        ]
        if (scenario === "containers")
          containers.push({ ...containers[0], name: "sidecar" })
        if (scenario === "container-name") containers[0].name = "other"
        if (scenario === "image")
          containers[0].image =
            "ghcr.io/francescostumpo/legal-callegarin:latest"
        const revisionDocument = {
          id:
            scenario === "revision-id" ? `${revisionPath}-wrong` : revisionPath,
          name: revisionName,
          active: scenario === "revision-active" ? false : true,
          healthState: scenario === "revision-health" ? "Unhealthy" : "Healthy",
          provisioningState:
            scenario === "revision-provisioning" ? "Failed" : "Provisioned",
          runningState: scenario === "revision-running" ? "Stopped" : "Running",
          fqdn: "callegarin--production.example.azurecontainerapps.io",
          trafficWeight: scenario === "revision-weight-null" ? null : 100,
          template: { containers },
        }
        if (scenario === "revision-weight-omitted")
          delete revisionDocument.trafficWeight
        return JSON.stringify(revisionDocument)
      }
      throw new Error(`${label} failed`)
    }
    if (command !== "gh") throw new Error(`${label} failed`)
    if (!args.includes(acceptHeader) || !args.includes(versionHeader))
      throw new Error(`${label} failed`)
    if (scenario === "gh-command-error") throw new Error(`${label} failed`)
    const endpoint = args[1]
    if (endpoint === "/user/packages/container/legal-callegarin") {
      state.packageReads += 1
      if (scenario === "package-malformed") return "{LEAKME"
      return JSON.stringify({
        owner: {
          login: scenario === "package-owner" ? "other" : "francescostumpo",
        },
        name: scenario === "package-name" ? "other" : "legal-callegarin",
        package_type: scenario === "package-type" ? "npm" : "container",
        visibility: scenario === "package-visibility" ? "public" : "private",
      })
    }
    if (
      endpoint?.startsWith(
        "/user/packages/container/legal-callegarin/versions?",
      )
    ) {
      const params = new URL(`https://api.github.invalid${endpoint}`)
        .searchParams
      const page = Number(params.get("page"))
      if (
        params.get("state") !== "active" ||
        params.get("per_page") !== "100" ||
        !Number.isInteger(page) ||
        page < 1
      )
        throw new Error(`${label} failed`)
      if (scenario === "page-malformed") return '{"items":[]}'
      if (page === 1) state.snapshotReads += 1
      if (scenario === "page-cap")
        return JSON.stringify(
          Array.from({ length: 100 }, (_, index) =>
            makeVersion((page - 1) * 100 + index + 1),
          ),
        )
      const values = makeVersions()
      if (scenario === "version-cap")
        return JSON.stringify(page === 1 ? values : [])
      return JSON.stringify(values.slice((page - 1) * 100, page * 100))
    }
    if (
      endpoint?.startsWith(
        "/users/francescostumpo/packages/container/legal-callegarin/versions/",
      )
    ) {
      const id = Number(endpoint.split("/").at(-1))
      state.deleteAttempts.push(id)
      if (scenario === "delete-failure" && state.deleted.length === 1)
        throw new Error(`${label} failed`)
      state.deleted.push(id)
      return ""
    }
    throw new Error(`${label} failed`)
  }
}

function runCase(scenario = "success", args = baseArgs, envOverrides = {}) {
  const env = {
    ...process.env,
    GH_TOKEN: token,
    ...envOverrides,
  }
  const calls = []
  const state = {
    azureReads: 0,
    revisionReads: 0,
    packageReads: 0,
    snapshotReads: 0,
    deleted: [],
    deleteAttempts: [],
  }
  const runner = createStatefulRunner(scenario, state, calls)
  let status = 0
  let stdout = ""
  let stderr = ""
  let error
  try {
    main(args, env, runner, (value) => {
      stdout += value
    })
  } catch (caught) {
    status = 1
    error = caught
    stderr = `retention failed: ${caught.message}\n`
  }
  return { status, signal: null, stdout, stderr, error, calls, state }
}

const deletes = (result) =>
  result.calls.filter((call) => call[0] === "gh" && call.includes("DELETE"))
const ghGets = (result) =>
  result.calls.filter((call) => call[0] === "gh" && call.includes("GET"))

test("retention selector keeps routed, newest ten, and exact age boundary with deterministic ties", () => {
  const now = new Date("2026-09-12T12:00:00.000Z")
  const old = new Date(now.getTime() - 31 * 86_400_000).toISOString()
  const boundary = new Date(now.getTime() - 30 * 86_400_000).toISOString()
  const versions = Array.from({ length: 13 }, (_, index) => ({
    id: index + 1,
    name: digest(index + 1),
    created_at:
      index < 10 ? new Date(now.getTime() - index * 1000).toISOString() : old,
    updated_at:
      index < 10
        ? new Date(now.getTime() - index * 1000).toISOString()
        : new Date(Date.parse(old) + index).toISOString(),
    metadata: { package_type: "container", container: { tags: [] } },
  }))
  versions[10].name = digest(99)
  versions[11].created_at = boundary
  versions[11].updated_at = boundary

  assert.deepEqual(
    selectRetentionCandidates(versions, digest(99), now).map((item) => item.id),
    [13],
  )

  const tied = versions.slice(0, 12).map((item, index) => ({
    ...item,
    created_at: old,
    updated_at: index < 11 ? old : new Date(Date.parse(old) + 1).toISOString(),
  }))
  assert.deepEqual(
    selectRetentionCandidates(tied, digest(1), now).map((item) => item.id),
    [2],
  )

  const nanosecondBase = "2026-08-01T00:00:00."
  const nanoVersions = Array.from({ length: 9 }, (_, index) => ({
    id: index + 3,
    name: digest(index + 3),
    created_at: `2026-08-02T00:00:0${index}.000000000Z`,
    updated_at: `2026-08-02T00:00:0${index}.000000000Z`,
  }))
  nanoVersions.push(
    {
      id: 1,
      name: digest(1),
      created_at: `${nanosecondBase}000000002Z`,
      updated_at: `${nanosecondBase}000000002Z`,
    },
    {
      id: 2,
      name: digest(2),
      created_at: `${nanosecondBase}000000001Z`,
      updated_at: `${nanosecondBase}000000001Z`,
    },
  )
  assert.deepEqual(
    selectRetentionCandidates(nanoVersions, digest(3), now).map(
      (item) => item.id,
    ),
    [2],
  )
})

test("production subprocess adapter is direct, bounded, and sanitizes child failures", () => {
  const source = readFileSync(script, "utf8")
  const adapter = source.match(
    /function defaultRun\([\s\S]*?\n}\n\nlet executeCommand/,
  )?.[0]
  assert.ok(
    adapter,
    "default subprocess adapter must remain directly inspectable",
  )
  assert.match(adapter, /spawnSync\(command, args,/)
  assert.match(adapter, /shell: false/)
  assert.match(adapter, /maxBuffer: MAX_OUTPUT_BYTES/)
  assert.match(adapter, /delete childEnvironment\.GH_TOKEN/)
  assert.doesNotMatch(adapter, /result\.(?:stderr|output)/)
  assert.doesNotMatch(adapter, /exec(?:File|Sync)?\(/)
  assert.match(
    source,
    /if \(result\.error \|\| result\.signal \|\| result\.status !== 0\)\s+fail\(`\$\{label} failed`\)/,
  )
})

test("invalid inputs and missing token make zero subprocess calls without leakage", () => {
  const cases = [
    [baseArgs.slice(0, -1), {}],
    [[...baseArgs, "--apply"], {}],
    [[...baseArgs, "--dry-run"], {}],
    [[...baseArgs, "positional"], {}],
    [[...baseArgs, "--unknown", token], {}],
    [
      baseArgs.map((value) =>
        value === subscription ? "ABCDEF00-0000-0000-0000-000000000000" : value,
      ),
      {},
    ],
    [
      baseArgs.map((value) => (value === resourceGroup ? "../unsafe" : value)),
      {},
    ],
    [baseArgs.map((value) => (value === appName ? "A" : value)), {}],
    [baseArgs, { GH_TOKEN: "" }],
  ]
  for (const [args, environment] of cases) {
    const result = runCase("success", args, environment)
    assert.notEqual(result.status, 0, JSON.stringify(args))
    assert.deepEqual(result.calls, [], JSON.stringify(args))
    assert.ok(!`${result.stdout}${result.stderr}`.includes(token))
  }
})

test("multi-page dry-run is deterministic, accepts untagged versions, and never deletes", () => {
  const result = runCase("multi-page")
  assert.equal(
    result.status,
    0,
    JSON.stringify({
      stderr: result.stderr,
      stdout: result.stdout,
      error: result.error?.message,
      calls: result.calls,
    }),
  )
  assert.equal(deletes(result).length, 0)
  assert.match(result.stdout, /mode=dry-run/)
  assert.match(result.stdout, /versions=101/)
  assert.match(result.stdout, /candidates=90/)
  assert.match(result.stdout, /candidate id=11 digest=sha256:/)
  const pageGets = ghGets(result).filter((call) =>
    String(call[2]).includes("/versions?"),
  )
  assert.equal(pageGets.length, 2)
  assert.ok(pageGets[0][2].endsWith("state=active&per_page=100&page=1"))
  assert.ok(pageGets[1][2].endsWith("state=active&per_page=100&page=2"))
  assert.ok(result.calls.every((call) => !call.includes(token)))
  assert.deepEqual(
    new Set(
      result.calls
        .filter((call) => call[0] === "az" && call[1] === "rest")
        .map((call) => call[call.indexOf("--query") + 1]),
    ),
    new Set([appQuery, revisionQuery]),
  )
})

test("app traffic remains authoritative when revision trafficWeight is null or omitted", () => {
  for (const scenario of ["revision-weight-null", "revision-weight-omitted"]) {
    const result = runCase(scenario)
    assert.equal(result.status, 0, `${scenario}: ${result.stderr}`)
    assert.match(result.stdout, /versions=13/)
    assert.match(result.stdout, /candidates=2/)
    assert.equal(deletes(result).length, 0)
  }
})

test("apply revalidates Azure and snapshot before exact sequential deletes, then verifies final state", () => {
  const result = runCase(
    "traffic-zero-coexists",
    baseArgs.slice(0, -1).concat("--apply"),
  )
  assert.equal(
    result.status,
    0,
    JSON.stringify({
      stderr: result.stderr,
      stdout: result.stdout,
      error: result.error?.message,
      calls: result.calls,
    }),
  )
  assert.deepEqual(result.state.deleted, [11, 12])
  assert.equal(result.state.azureReads, 3)
  assert.equal(result.state.revisionReads, 3)
  assert.equal(result.state.snapshotReads, 3)
  assert.match(result.stdout, /deleted=2/)
  const deleteCalls = deletes(result)
  assert.deepEqual(
    deleteCalls.map((call) => call[2]),
    [
      "/users/francescostumpo/packages/container/legal-callegarin/versions/11",
      "/users/francescostumpo/packages/container/legal-callegarin/versions/12",
    ],
  )
  for (const call of deleteCalls) {
    assert.deepEqual(call, [
      "gh",
      "api",
      call[2],
      "--method",
      "DELETE",
      "-H",
      acceptHeader,
      "-H",
      versionHeader,
      "--silent",
    ])
  }
  const firstDelete = result.calls.indexOf(deleteCalls[0])
  assert.ok(
    result.calls
      .slice(0, firstDelete)
      .filter((call) => call[0] === "az" && call[1] === "rest").length >= 4,
  )
  assert.ok(
    result.calls
      .slice(0, firstDelete)
      .filter((call) => call[0] === "gh" && call.includes("GET")).length >= 4,
  )
})

test("unsafe Azure account, app, traffic, revision, image, and schema states cause zero deletes", () => {
  const scenarios = [
    "wrong-account",
    "wrong-rg",
    "app-id",
    "app-provisioning",
    "app-mode",
    "app-external",
    "app-insecure",
    "traffic-sum",
    "traffic-latest",
    "traffic-label",
    "traffic-multiple",
    "unsafe-revision",
    "revision-id",
    "revision-active",
    "revision-health",
    "revision-provisioning",
    "revision-running",
    "image",
    "containers",
    "container-name",
    "schema",
    "schema-duplicate",
    "schema-secret",
  ]
  for (const scenario of scenarios) {
    const result = runCase(scenario, baseArgs.slice(0, -1).concat("--apply"))
    assert.notEqual(result.status, 0, scenario)
    assert.equal(deletes(result).length, 0, scenario)
    assert.ok(!`${result.stdout}${result.stderr}`.includes(token), scenario)
  }
})

test("unsafe package or version data and every cap cause zero deletes", () => {
  const scenarios = [
    "package-owner",
    "package-name",
    "package-type",
    "package-visibility",
    "package-malformed",
    "page-malformed",
    "item-id",
    "item-digest",
    "item-time",
    "future-time",
    "update-before-create",
    "duplicate-id",
    "duplicate-digest",
    "duplicate-tag",
    "empty-tag",
    "unknown-tag",
    "metadata-type",
    "tags-shape",
    "missing-current",
    "page-cap",
    "version-cap",
    "delete-cap",
  ]
  for (const scenario of scenarios) {
    const result = runCase(scenario, baseArgs.slice(0, -1).concat("--apply"))
    assert.notEqual(result.status, 0, scenario)
    assert.equal(deletes(result).length, 0, scenario)
    assert.ok(!`${result.stdout}${result.stderr}`.includes(token), scenario)
  }
})

test("pre-delete Azure or GitHub drift causes zero deletes", () => {
  for (const scenario of ["pre-azure-drift", "pre-gh-drift"]) {
    const result = runCase(scenario, baseArgs.slice(0, -1).concat("--apply"))
    assert.notEqual(result.status, 0, scenario)
    assert.equal(deletes(result).length, 0, scenario)
  }
})

test("a delete failure stops immediately and reports only failed ID and partial count", () => {
  const result = runCase(
    "delete-failure",
    baseArgs.slice(0, -1).concat("--apply"),
  )
  assert.notEqual(result.status, 0)
  assert.deepEqual(result.state.deleted, [11])
  assert.deepEqual(result.state.deleteAttempts, [11, 12])
  assert.match(result.stderr, /failed ID 12/)
  assert.match(result.stderr, /1 already deleted/)
  assert.ok(!`${result.stdout}${result.stderr}`.includes("LEAKME"))
  assert.ok(!`${result.stdout}${result.stderr}`.includes(token))
})

test("final Azure or GitHub drift requires operator review after completed deletes", () => {
  for (const scenario of [
    "final-azure-drift",
    "final-gh-drift",
    "final-current-missing",
  ]) {
    const result = runCase(scenario, baseArgs.slice(0, -1).concat("--apply"))
    assert.notEqual(result.status, 0, scenario)
    assert.deepEqual(result.state.deleted, [11, 12], scenario)
    assert.match(result.stderr, /operator review/, scenario)
  }
})

test("apply with no candidates succeeds after validated initial reads without DELETE", () => {
  const now = new Date()
  assert.deepEqual(
    selectRetentionCandidates(
      Array.from({ length: 10 }, (_, i) => ({
        id: i + 1,
        name: digest(i + 1),
        created_at: now.toISOString(),
        updated_at: now.toISOString(),
      })),
      digest(10),
      now,
    ),
    [],
  )
  const result = runCase(
    "no-candidates",
    baseArgs.slice(0, -1).concat("--apply"),
  )
  assert.equal(
    result.status,
    0,
    JSON.stringify({
      stderr: result.stderr,
      stdout: result.stdout,
      error: result.error?.message,
      calls: result.calls,
    }),
  )
  assert.equal(deletes(result).length, 0)
  assert.equal(result.state.snapshotReads, 1)
  assert.match(result.stdout, /cleanup_status=succeeded/)
})

test("subprocess and JSON failures use fixed diagnostics and never leak bodies or token", () => {
  for (const scenario of [
    "az-command-error",
    "az-malformed-json",
    "gh-command-error",
    "package-malformed",
  ]) {
    const result = runCase(scenario, baseArgs.slice(0, -1).concat("--apply"))
    assert.notEqual(result.status, 0, scenario)
    assert.equal(deletes(result).length, 0, scenario)
    assert.ok(!`${result.stdout}${result.stderr}`.includes("LEAKME"), scenario)
    assert.ok(!`${result.stdout}${result.stderr}`.includes(token), scenario)
  }
})
