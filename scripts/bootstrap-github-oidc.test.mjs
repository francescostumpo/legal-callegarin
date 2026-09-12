import assert from "node:assert/strict"
import { createHash, randomBytes } from "node:crypto"
import { chmod, mkdtemp, readFile, rm, writeFile } from "node:fs/promises"
import { tmpdir } from "node:os"
import { join, resolve } from "node:path"
import { fileURLToPath } from "node:url"
import test from "node:test"
import { spawnSync } from "node:child_process"

const root = resolve(fileURLToPath(new URL("../", import.meta.url)))
const script = join(root, "scripts", "bootstrap-github-oidc.sh")
const scriptSource = await readFile(script, "utf8").catch(() => "")
const subscription = "11111111-1111-4111-8111-111111111111"
const tenant = "22222222-2222-4222-8222-222222222222"
const appObject = "33333333-3333-4333-8333-333333333333"
const client = "44444444-4444-4444-8444-444444444444"
const spObject = "55555555-5555-4555-8555-555555555555"
const role = "358470bc-b998-42bd-ab17-a7e34c199c0f"
const subject =
  "repo:francescostumpo@55147498/legal-callegarin@1365534753:environment:production"
const baseArgs = [
  "--subscription-id",
  subscription,
  "--tenant-id",
  tenant,
  "--resource-group",
  "legal-callegarin-prod",
  "--github-owner-id",
  "55147498",
  "--github-repository-id",
  "1365534753",
  "--application-display-name",
  "legal-callegarin-github-production",
  "--federated-credential-name",
  "github-production",
]

function app(overrides = {}) {
  return {
    id: appObject,
    appId: client,
    displayName: "legal-callegarin-github-production",
    signInAudience: "AzureADMyOrg",
    passwordCredentials: [],
    keyCredentials: [],
    requiredResourceAccess: [],
    identifierUris: [],
    web: { redirectUris: [] },
    spa: { redirectUris: [] },
    publicClient: { redirectUris: [] },
    ...overrides,
  }
}

function sp(overrides = {}) {
  return {
    id: spObject,
    appId: client,
    accountEnabled: true,
    servicePrincipalType: "Application",
    passwordCredentials: [],
    keyCredentials: [],
    ...overrides,
  }
}

function fic(overrides = {}) {
  return {
    name: "github-production",
    issuer: "https://token.actions.githubusercontent.com",
    subject,
    audiences: ["api://AzureADTokenExchange"],
    claimsMatchingExpression: null,
    ...overrides,
  }
}

function assignment(overrides = {}) {
  return {
    id: "assignment-1",
    principalId: spObject,
    principalType: "ServicePrincipal",
    scope: `/subscriptions/${subscription}/resourceGroups/legal-callegarin-prod`,
    roleDefinitionId: `/subscriptions/${subscription}/providers/Microsoft.Authorization/roleDefinitions/${role}`,
    condition: null,
    conditionVersion: null,
    ...overrides,
  }
}

async function harness(t, scenario = {}) {
  const dir = await mkdtemp(join(tmpdir(), "oidc-bootstrap-test-"))
  t.after(() => rm(dir, { recursive: true, force: true }))
  const bin = join(dir, "bin")
  await (await import("node:fs/promises")).mkdir(bin)
  const log = join(dir, "calls.jsonl")
  const state = join(dir, "state.json")
  const githubToken = `github_pat_${randomBytes(32).toString("hex")}`
  const tokenHash = createHash("sha256").update(githubToken).digest("hex")
  const headerHash = createHash("sha256")
    .update(`Authorization: Bearer ${githubToken}\n`)
    .digest("hex")
  await writeFile(
    state,
    JSON.stringify({
      app: scenario.app === undefined ? null : scenario.app,
      sp: scenario.sp === undefined ? null : scenario.sp,
      fic: scenario.fic === undefined ? null : scenario.fic,
      rbac: scenario.rbac === undefined ? null : scenario.rbac,
      ...scenario.state,
    }),
  )

  const fake = `#!/usr/bin/node
import crypto from "node:crypto"
import fs from "node:fs"
const tool = process.argv[1].split("/").pop()
const args = process.argv.slice(2)
const digest = value => crypto.createHash("sha256").update(value).digest("hex")
const tokenLength = Number(process.env.EXPECTED_GITHUB_TOKEN_LENGTH)
const containsCredential = value => {
  for (let offset = 0; offset + tokenLength <= value.length; offset += 1) {
    if (digest(value.slice(offset, offset + tokenLength)) === process.env.EXPECTED_GITHUB_TOKEN_HASH) return true
  }
  return false
}
if (
  Object.hasOwn(process.env, "GITHUB_API_TOKEN") ||
  Object.hasOwn(process.env, "github_api_token") ||
  Object.values(process.env).some(containsCredential)
) {
  process.stderr.write("credential leaked to child environment\\n")
  process.exit(90)
}
fs.appendFileSync(process.env.CALL_LOG, JSON.stringify([tool, ...args]) + "\\n")
const scenario = JSON.parse(process.env.SCENARIO)
const statePath = process.env.STATE_FILE
const state = JSON.parse(fs.readFileSync(statePath, "utf8"))
const save = () => fs.writeFileSync(statePath, JSON.stringify(state))
const out = value => process.stdout.write(typeof value === "string" ? value : JSON.stringify(value))
if (tool === "sleep") process.exit(0)
if (tool === "curl") {
  if (!args.some((value, index) => value === "--header" && args[index + 1] === "@-")) {
    process.stderr.write("missing authorization header stdin option\\n")
    process.exit(91)
  }
  const headers = fs.readFileSync(0, "utf8")
  if (digest(headers) !== process.env.EXPECTED_GITHUB_HEADER_HASH) {
    process.stderr.write("missing or invalid authorization header\\n")
    process.exit(91)
  }
  const url = args.at(-1)
  state.curlCalls = (state.curlCalls ?? 0) + 1
  save()
  if (scenario.githubFailureAt === state.curlCalls) {
    const status = scenario.githubFailureStatus
    process.stderr.write(status ? "GitHub request failed with HTTP " + status + "\\n" : "GitHub request failed\\n")
    process.exit(scenario.githubFailureCode ?? 22)
  }
  if (scenario.githubMismatch) {
    out(url.endsWith("customization/sub") ? { use_default: true, use_immutable_subject: false, sub_claim_prefix: "legacy" } : { full_name: "wrong/repo" })
  } else if (url.endsWith("customization/sub")) {
    out({ use_default: true, use_immutable_subject: true, sub_claim_prefix: "repo:francescostumpo@55147498/legal-callegarin@1365534753" })
  } else {
    out({ full_name: "francescostumpo/legal-callegarin", id: 1365534753, owner: { id: 55147498 }, created_at: scenario.invalidCreated ? "not-a-date" : "2026-09-11T07:13:11Z", archived: false, disabled: false })
  }
  process.exit(0)
}
const command = args.join(" ")
if (command.startsWith("account show ")) out(scenario.contextMismatch ? "${subscription}\\twrong\\tEnabled\\n" : scenario.accountExtra ? "${subscription}\\t${tenant}\\tEnabled\\nunexpected\\n" : "${subscription}\\t${tenant}\\tEnabled\\n")
else if (command.startsWith("group show ")) out(scenario.groupMismatch ? "/subscriptions/${subscription}/resourceGroups/wrong\\n" : "/subscriptions/${subscription}/resourceGroups/legal-callegarin-prod\\n")
else if (command.startsWith("ad app list ")) out(state.app === null ? [] : Array.isArray(state.app) ? state.app : [state.app])
else if (command.startsWith("ad app create ")) { state.app = ${JSON.stringify(app())}; save(); out({ id: "${appObject}", appId: "${client}" }) }
else if (command.startsWith("ad app credential list ")) out(scenario.appCredential ? [{ keyId: "SENTINEL_SECRET" }] : [])
else if (command.startsWith("ad sp list ")) out(state.sp === null || scenario.spTimeout ? [] : Array.isArray(state.sp) ? state.sp : [state.sp])
else if (command.startsWith("ad sp create ")) { state.sp = ${JSON.stringify(sp())}; save() }
else if (command.startsWith("ad sp credential list ")) out(scenario.spCredential ? [{ keyId: "SENTINEL_SECRET" }] : [])
else if (command.startsWith("ad app federated-credential list ")) out(scenario.ficTimeout ? [] : state.fic === null ? [] : Array.isArray(state.fic) ? state.fic : [state.fic])
else if (command.startsWith("ad app federated-credential create ")) {
  const parameter = args[args.indexOf("--parameters") + 1]
  const path = parameter.startsWith("@") ? parameter.slice(1) : parameter
  state.ficCapture = { path, body: JSON.parse(fs.readFileSync(path, "utf8")), mode: fs.statSync(path).mode & 0o777 }
  save()
  if (scenario.ficConflictIncompatible) {
    state.fic = ${JSON.stringify(fic({ subject: "incompatible-after-conflict" }))}
    save()
    process.exit(1)
  }
  if (!scenario.ficTimeout) { state.fic = ${JSON.stringify(fic())}; save() }
  if (scenario.ficConflict) process.exit(1)
}
else if (command.startsWith("role assignment list ")) out(scenario.rbacTimeout ? [] : scenario.rbacDirectEmpty && args.includes("--all") ? [] : state.rbac === null ? [] : Array.isArray(state.rbac) ? state.rbac : [state.rbac])
else if (command.startsWith("role assignment create ")) {
  if (scenario.rbacConflictIncompatible) {
    state.rbac = ${JSON.stringify(assignment({ roleDefinitionId: `/subscriptions/${subscription}/providers/Microsoft.Authorization/roleDefinitions/66666666-6666-4666-8666-666666666666` }))}
    save()
    process.exit(1)
  }
  if (!scenario.rbacTimeout) { state.rbac = ${JSON.stringify(assignment())}; save() }
  if (scenario.rbacConflict) process.exit(1)
}
else { process.stderr.write("unexpected az command: " + command); process.exit(64) }
`
  for (const name of ["az", "curl", "sleep"]) {
    const path = join(bin, name)
    await writeFile(path, fake)
    await chmod(path, 0o700)
  }

  const fakeNode = `#!/usr/bin/node
import crypto from "node:crypto"
import fs from "node:fs"
import { spawnSync } from "node:child_process"
const digest = value => crypto.createHash("sha256").update(value).digest("hex")
const tokenLength = Number(process.env.EXPECTED_GITHUB_TOKEN_LENGTH)
const containsCredential = value => {
  for (let offset = 0; offset + tokenLength <= value.length; offset += 1) {
    if (digest(value.slice(offset, offset + tokenLength)) === process.env.EXPECTED_GITHUB_TOKEN_HASH) return true
  }
  return false
}
if (
  Object.hasOwn(process.env, "GITHUB_API_TOKEN") ||
  Object.hasOwn(process.env, "github_api_token") ||
  Object.values(process.env).some(containsCredential)
) {
  process.stderr.write("credential leaked to node environment\\n")
  process.exit(90)
}
fs.appendFileSync(process.env.CALL_LOG, JSON.stringify(["node", ...process.argv.slice(2)]) + "\\n")
const result = spawnSync("/usr/bin/node", process.argv.slice(2), { stdio: "inherit", env: process.env })
if (result.error || result.signal || result.status === null) process.exit(92)
process.exit(result.status)
`
  const nodePath = join(bin, "node")
  await writeFile(nodePath, fakeNode)
  await chmod(nodePath, 0o700)

  function run(args, options = {}) {
    const environment = {
      ...process.env,
      PATH: `${bin}:${process.env.PATH}`,
      CALL_LOG: log,
      STATE_FILE: state,
      SCENARIO: JSON.stringify(scenario),
      SENTINEL_SECRET: "never-print-this",
      EXPECTED_GITHUB_TOKEN_HASH: tokenHash,
      EXPECTED_GITHUB_TOKEN_LENGTH: String(githubToken.length),
      EXPECTED_GITHUB_HEADER_HASH: headerHash,
    }
    delete environment.github_api_token
    if (options.omitToken) {
      delete environment.GITHUB_API_TOKEN
    } else {
      environment.GITHUB_API_TOKEN = options.token ?? githubToken
    }
    if (options.exportLowercase) {
      environment.github_api_token = githubToken
    }
    const shellOptions = [
      ...(options.allexport ? ["-a"] : []),
      ...(options.xtrace ? ["-x"] : []),
    ]
    return spawnSync("bash", [...shellOptions, script, ...args], {
      cwd: root,
      encoding: "utf8",
      env: environment,
      input: "",
    })
  }

  function probeWrappedCredential(tool) {
    const environment = {
      ...process.env,
      CALL_LOG: log,
      STATE_FILE: state,
      SCENARIO: JSON.stringify(scenario),
      EXPECTED_GITHUB_TOKEN_HASH: tokenHash,
      EXPECTED_GITHUB_TOKEN_LENGTH: String(githubToken.length),
      EXPECTED_GITHUB_HEADER_HASH: headerHash,
      LEAK_PROBE: `Bearer ${githubToken}`,
    }
    delete environment.GITHUB_API_TOKEN
    delete environment.github_api_token
    const args = tool === "node" ? ["-e", "process.exit(0)"] : []
    return spawnSync(join(bin, tool), args, {
      cwd: root,
      encoding: "utf8",
      env: environment,
      input: "",
    })
  }

  async function calls() {
    try {
      return (await readFile(log, "utf8"))
        .trim()
        .split("\n")
        .filter(Boolean)
        .map((line) => JSON.parse(line))
    } catch (error) {
      if (error.code === "ENOENT") return []
      throw error
    }
  }

  async function artifacts() {
    const paths = [log, state, ...["az", "curl", "sleep", "node"].map((name) => join(bin, name))]
    const contents = await Promise.all(
      paths.map((path) => readFile(path, "utf8").catch(() => "")),
    )
    return contents.join("\n")
  }

  return {
    run,
    calls,
    artifacts,
    probeWrappedCredential,
    token: githubToken,
    state: () => readFile(state, "utf8").then(JSON.parse),
  }
}

test("dry-run prints exact immutable intent and performs no external calls", async (t) => {
  const h = await harness(t)
  const result = h.run([...baseArgs, "--dry-run"], { omitToken: true })
  assert.equal(result.status, 0)
  assert.equal(
    result.stdout,
    `repository=francescostumpo/legal-callegarin\ngithub_owner_id=55147498\ngithub_repository_id=1365534753\napplication_display_name=legal-callegarin-github-production\nfederated_credential_name=github-production\nissuer=https://token.actions.githubusercontent.com\nsubject=${subject}\naudience=api://AzureADTokenExchange\nrole_definition_id=${role}\nAZURE_TENANT_ID=${tenant}\nAZURE_SUBSCRIPTION_ID=${subscription}\nAZURE_RESOURCE_GROUP=legal-callegarin-prod\nresource_group_scope=/subscriptions/${subscription}/resourceGroups/legal-callegarin-prod\nchanges_applied=false\n`,
  )
  assert.doesNotMatch(`${result.stdout}${result.stderr}`, /GITHUB_API_TOKEN|Authorization|Bearer/i)
  assert.deepEqual(await h.calls(), [])
})

test("live token validation fails before every child tool without disclosure", async (t) => {
  const cases = [
    ["missing", { omitToken: true }],
    ["empty", { token: "" }],
    ["short", { token: "short" }],
    ["space", { token: "github_pat_invalid token" }],
    ["tab", { token: "github_pat_invalid\ttoken" }],
    ["carriage return", { token: "github_pat_invalid\rtoken" }],
    ["line feed", { token: "github_pat_invalid\ntoken" }],
    ["colon", { token: "github_pat_invalid:token" }],
    ["excessive", { token: "a".repeat(256) }],
  ]

  for (const [name, options] of cases) {
    const h = await harness(t)
    const result = h.run(baseArgs, options)
    assert.notEqual(result.status, 0, name)
    assert.equal(result.stdout, "", name)
    assert.equal(result.stderr, "error: invalid GitHub API token\n", name)
    assert.deepEqual(await h.calls(), [], name)
    if ("token" in options && options.token) {
      assert.doesNotMatch(`${result.stdout}${result.stderr}`, new RegExp(options.token.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")), name)
    }
  }
})

test("help and invalid invocations fail offline without calling tools", async (t) => {
  const invalid = [
    ["--help"],
    [],
    [...baseArgs, "--dry-run", "--dry-run"],
    [...baseArgs, "--unknown", "x"],
    [...baseArgs, "positional"],
    baseArgs.map((v) => (v === subscription ? "not-a-uuid" : v)),
    baseArgs.map((v) => (v === "55147498" ? "55147499" : v)),
    baseArgs.map((v) => (v === "legal-callegarin-prod" ? "invalid/name" : v)),
    baseArgs.map((v) =>
      v === "legal-callegarin-github-production" ? `${v} ` : v,
    ),
    baseArgs.map((v) => (v === "github-production" ? "invalid/name" : v)),
    [...baseArgs, "--subscription-id", subscription],
  ]
  for (const args of invalid) {
    const h = await harness(t)
    const result = h.run(args)
    assert.equal(result.status === 0, args[0] === "--help")
    assert.deepEqual(await h.calls(), [])
  }
})

test("create path performs verified ordered mutations and secure FIC handoff", async (t) => {
  const h = await harness(t)
  const result = h.run(baseArgs)
  assert.equal(result.status, 0, `${result.stdout}\n${result.stderr}`)
  assert.match(result.stdout, new RegExp(`AZURE_CLIENT_ID=${client}`))
  assert.match(result.stdout, new RegExp(`subject=${subject}`))
  assert.match(result.stdout, /changes_applied=true/)
  assert.doesNotMatch(`${result.stdout}${result.stderr}`, /SENTINEL_SECRET/)
  const calls = await h.calls()
  const joined = calls.map((call) => call.join(" ")).join("\n")
  const curlCalls = calls.filter((call) => call[0] === "curl")
  assert.equal(curlCalls.length, 2)
  for (const call of curlCalls) {
    assert.deepEqual(call.slice(0, 9), [
      "curl",
      "--disable",
      "--no-location",
      "--fail",
      "--silent",
      "--show-error",
      "--header",
      "@-",
      "--header",
    ])
    assert.equal(call.includes("--location"), false)
    assert.equal(call.includes("-L"), false)
    assert.doesNotMatch(call.join(" "), new RegExp(h.token))
  }
  assert.match(joined, /Accept: application\/vnd\.github\+json/)
  assert.match(joined, /X-GitHub-Api-Version: 2026-03-10/)
  assert.match(
    joined,
    /--user-agent legal-callegarin-oidc-bootstrap\/1\.0 https:\/\/api\.github\.com\/repos\/francescostumpo\/legal-callegarin$/m,
  )
  assert.match(
    joined,
    /https:\/\/api\.github\.com\/repos\/francescostumpo\/legal-callegarin\/actions\/oidc\/customization\/sub/,
  )
  assert.match(
    joined,
    /az account show --query \[id,tenantId,state\] --output tsv/,
  )
  assert.match(
    joined,
    /az ad app create .*--sign-in-audience AzureADMyOrg.*--output json/,
  )
  assert.match(
    joined,
    new RegExp(`az ad sp create --id ${client} --output none`),
  )
  assert.match(
    joined,
    new RegExp(`federated-credential create --id ${appObject}`),
  )
  assert.match(
    joined,
    new RegExp(
      `role assignment create .*--role ${role}.*--assignee-object-id ${spObject}.*--assignee-principal-type ServicePrincipal`,
    ),
  )
  const roleLists = calls.filter(
    (call) =>
      call[0] === "az" && call.slice(1, 4).join(" ") === "role assignment list",
  )
  const directArgs = [
    "az",
    "role",
    "assignment",
    "list",
    "--subscription",
    subscription,
    "--assignee-object-id",
    spObject,
    "--all",
    "--fill-principal-name",
    "false",
    "--fill-role-definition-name",
    "false",
    "--output",
    "json",
  ]
  const visibleArgs = [
    "az",
    "role",
    "assignment",
    "list",
    "--subscription",
    subscription,
    "--assignee-object-id",
    spObject,
    "--scope",
    `/subscriptions/${subscription}/resourceGroups/legal-callegarin-prod`,
    "--include-inherited",
    "--fill-principal-name",
    "false",
    "--fill-role-definition-name",
    "false",
    "--output",
    "json",
  ]
  assert.ok(
    roleLists.some(
      (call) => JSON.stringify(call) === JSON.stringify(directArgs),
    ),
  )
  assert.ok(
    roleLists.some(
      (call) => JSON.stringify(call) === JSON.stringify(visibleArgs),
    ),
  )
  assert.doesNotMatch(joined, /--include-inherited (?:false|true)/)
  assert.match(
    joined,
    new RegExp(
      `role assignment create --subscription ${subscription} --role ${role} --scope /subscriptions/${subscription}/resourceGroups/legal-callegarin-prod --assignee-object-id ${spObject} --assignee-principal-type ServicePrincipal --output none`,
    ),
  )
  assert.doesNotMatch(
    joined,
    /login|create-for-rbac|credential reset|access-token|\bdelete\b|\bupdate\b|github\.com.*(?:PUT|POST|PATCH)/i,
  )
  assert.doesNotMatch(`${result.stdout}${result.stderr}`, new RegExp(h.token))
  assert.doesNotMatch(await h.artifacts(), new RegExp(h.token))
  const firstMutation = calls.findIndex((call) => call.includes("create"))
  assert.ok(firstMutation > calls.findIndex((call) => call[1] === "group"))
  const final = await h.state()
  assert.equal(final.ficCapture.mode, 0o600)
  assert.deepEqual(final.ficCapture.body, {
    name: "github-production",
    issuer: "https://token.actions.githubusercontent.com",
    subject,
    audiences: ["api://AzureADTokenExchange"],
  })
  assert.equal(typeof final.ficCapture.path, "string")
  await assert.rejects(
    readFile(final.ficCapture.path),
    (error) => error.code === "ENOENT",
  )
  assert.match(scriptSource, /trap 'cleanup_fic; exit 130' INT/)
  assert.match(scriptSource, /trap 'cleanup_fic; exit 143' TERM/)
})

for (const [name, scenario, expectedCurlCalls] of [
  ["first GitHub request 401", { githubFailureAt: 1, githubFailureStatus: 401 }, 1],
  ["second GitHub request 403", { githubFailureAt: 2, githubFailureStatus: 403 }, 2],
  ["first GitHub transport failure", { githubFailureAt: 1 }, 1],
]) {
  test(`${name} stops before Azure and Node`, async (t) => {
    const h = await harness(t, scenario)
    const result = h.run(baseArgs)
    assert.notEqual(result.status, 0)
    const calls = await h.calls()
    assert.equal(calls.filter((call) => call[0] === "curl").length, expectedCurlCalls)
    assert.equal(calls.filter((call) => call[0] === "az").length, 0)
    assert.equal(calls.filter((call) => call[0] === "node").length, 0)
    assert.doesNotMatch(`${result.stdout}${result.stderr}`, new RegExp(h.token))
    assert.doesNotMatch(await h.artifacts(), new RegExp(h.token))
  })
}

test("bash xtrace is disabled before the live credential is handled", async (t) => {
  const h = await harness(t, {
    app: app(),
    sp: sp(),
    fic: fic(),
    rbac: assignment(),
  })
  const result = h.run(baseArgs, { xtrace: true })
  assert.equal(result.status, 0, result.stderr)
  assert.doesNotMatch(`${result.stdout}${result.stderr}`, new RegExp(h.token))
  assert.doesNotMatch(await h.artifacts(), new RegExp(h.token))
})

for (const [name, options] of [
  ["an inherited exported lowercase token", { exportLowercase: true }],
  ["bash allexport", { allexport: true }],
]) {
  test(`${name} cannot export the private token to child tools`, async (t) => {
    const h = await harness(t, {
      app: app(),
      sp: sp(),
      fic: fic(),
      rbac: assignment(),
    })
    const result = h.run(baseArgs, options)
    assert.equal(result.status, 0, result.stderr)
    const calls = await h.calls()
    for (const tool of ["curl", "node", "az"]) {
      assert.ok(calls.some((call) => call[0] === tool), tool)
    }
    assert.doesNotMatch(`${result.stdout}${result.stderr}`, new RegExp(h.token))
    assert.doesNotMatch(await h.artifacts(), new RegExp(h.token))
  })
}

test("fake child scanners detect a credential inside an environment wrapper", async (t) => {
  const h = await harness(t)
  for (const tool of ["sleep", "node"]) {
    const result = h.probeWrappedCredential(tool)
    assert.equal(result.status, 90, tool)
    assert.doesNotMatch(`${result.stdout}${result.stderr}`, new RegExp(h.token))
  }
  assert.deepEqual(await h.calls(), [])
  assert.doesNotMatch(await h.artifacts(), new RegExp(h.token))
})

test("exact reuse is idempotent and applies no mutation", async (t) => {
  const h = await harness(t, {
    app: app(),
    sp: sp(),
    fic: fic(),
    rbac: assignment(),
  })
  const result = h.run(baseArgs)
  assert.equal(result.status, 0, result.stderr)
  assert.match(result.stdout, /changes_applied=false/)
  assert.doesNotMatch(
    (await h.calls()).map((v) => v.join(" ")).join("\n"),
    /\bcreate\b/,
  )
})

test("partial state resumes safely", async (t) => {
  const h = await harness(t, { app: app() })
  const result = h.run(baseArgs)
  assert.equal(result.status, 0, result.stderr)
  assert.match(result.stdout, /changes_applied=true/)
})

for (const [name, scenario] of [
  ["GitHub mismatch", { githubMismatch: true }],
  ["invalid GitHub creation date", { invalidCreated: true }],
  ["Azure context mismatch", { contextMismatch: true }],
  ["multiline Azure context", { accountExtra: true }],
  ["resource group mismatch", { groupMismatch: true }],
]) {
  test(`${name} fails before mutation`, async (t) => {
    const h = await harness(t, scenario)
    const result = h.run(baseArgs)
    assert.notEqual(result.status, 0)
    assert.doesNotMatch(
      (await h.calls()).map((v) => v.join(" ")).join("\n"),
      /\bcreate\b/,
    )
  })
}

for (const [name, scenario] of [
  [
    "duplicate app",
    { app: [app(), app({ id: "66666666-6666-4666-8666-666666666666" })] },
  ],
  ["app credential", { app: app(), appCredential: true }],
  ["incompatible SP", { app: app(), sp: sp({ accountEnabled: false }) }],
  ["SP credential", { app: app(), sp: sp(), spCredential: true }],
  [
    "incompatible FIC",
    { app: app(), sp: sp(), fic: fic({ subject: "legacy" }) },
  ],
  [
    "extra FIC",
    {
      app: app(),
      sp: sp(),
      fic: [fic(), fic({ name: "unexpected-extra" })],
    },
  ],
  [
    "duplicate RBAC",
    {
      app: app(),
      sp: sp(),
      fic: fic(),
      rbac: [assignment(), assignment({ id: "assignment-2" })],
    },
  ],
  [
    "different-role RBAC",
    {
      app: app(),
      sp: sp(),
      fic: fic(),
      rbac: assignment({
        roleDefinitionId: `/subscriptions/${subscription}/providers/Microsoft.Authorization/roleDefinitions/66666666-6666-4666-8666-666666666666`,
      }),
    },
  ],
  [
    "broader RBAC",
    {
      app: app(),
      sp: sp(),
      fic: fic(),
      rbac: assignment({ scope: `/subscriptions/${subscription}` }),
    },
  ],
  [
    "inherited-only RBAC",
    {
      app: app(),
      sp: sp(),
      fic: fic(),
      rbac: assignment(),
      rbacDirectEmpty: true,
    },
  ],
  [
    "conditional RBAC",
    {
      app: app(),
      sp: sp(),
      fic: fic(),
      rbac: assignment({ condition: "@Resource[foo]" }),
    },
  ],
]) {
  test(`${name} is rejected without repair`, async (t) => {
    const h = await harness(t, scenario)
    const result = h.run(baseArgs)
    assert.notEqual(result.status, 0)
    const joined = (await h.calls()).map((v) => v.join(" ")).join("\n")
    assert.doesNotMatch(joined, /\bdelete\b|\bupdate\b|credential reset/)
    if (name === "app credential" || name === "SP credential") {
      assert.doesNotMatch(`${result.stdout}${result.stderr}`, /SENTINEL_SECRET/)
    }
  })
}

test("SP visibility polling is bounded to 12 sleeps", async (t) => {
  const h = await harness(t, { app: app(), spTimeout: true })
  const result = h.run(baseArgs)
  assert.notEqual(result.status, 0)
  assert.equal(
    (await h.calls()).filter(([tool]) => tool === "sleep").length,
    12,
  )
})

for (const [name, scenario] of [
  ["FIC", { app: app(), sp: sp(), ficTimeout: true }],
  ["RBAC", { app: app(), sp: sp(), fic: fic(), rbacTimeout: true }],
]) {
  test(`${name} visibility polling is bounded to 12 sleeps`, async (t) => {
    const h = await harness(t, scenario)
    const result = h.run(baseArgs)
    assert.notEqual(result.status, 0)
    assert.equal(
      (await h.calls()).filter(([tool]) => tool === "sleep").length,
      12,
    )
  })
}

test("failed FIC creation removes its mode-0600 temporary payload", async (t) => {
  const h = await harness(t, { app: app(), sp: sp(), ficTimeout: true })
  const result = h.run(baseArgs)
  assert.notEqual(result.status, 0)
  const final = await h.state()
  assert.equal(final.ficCapture.mode, 0o600)
  assert.equal(typeof final.ficCapture.path, "string")
  await assert.rejects(
    readFile(final.ficCapture.path),
    (error) => error.code === "ENOENT",
  )
})

for (const [name, scenario] of [
  ["FIC", { app: app(), sp: sp(), ficConflictIncompatible: true }],
  [
    "RBAC",
    {
      app: app(),
      sp: sp(),
      fic: fic(),
      rbacConflictIncompatible: true,
    },
  ],
]) {
  test(`${name} nonzero create with incompatible reread fails closed`, async (t) => {
    const h = await harness(t, scenario)
    const result = h.run(baseArgs)
    assert.notEqual(result.status, 0)
    assert.doesNotMatch(result.stdout, /changes_applied=/)
  })
}

for (const [name, scenario] of [
  ["FIC", { app: app(), sp: sp(), ficConflict: true }],
  ["RBAC", { app: app(), sp: sp(), fic: fic(), rbacConflict: true }],
]) {
  test(`${name} create conflict is accepted only after exact re-audit`, async (t) => {
    const h = await harness(t, scenario)
    const result = h.run(baseArgs)
    assert.equal(result.status, 0, result.stderr)
    assert.match(result.stdout, /changes_applied=true/)
  })
}
