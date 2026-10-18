import assert from "node:assert/strict"
import { spawnSync } from "node:child_process"
import {
  chmod,
  mkdir,
  mkdtemp,
  readFile,
  readdir,
  rm,
  writeFile,
} from "node:fs/promises"
import { tmpdir } from "node:os"
import { dirname, join } from "node:path"
import { fileURLToPath } from "node:url"
import test from "node:test"

const repositoryRoot = fileURLToPath(new URL("../", import.meta.url))
const syftImage =
  "anchore/syft:v1.51.1-nonroot@sha256:277f11d9e3dd8a6853f6e102156c79578d0d9adffe563db613b4397377bbbc0a"
const trivyImage =
  "ghcr.io/aquasecurity/trivy:0.74.0@sha256:62b1e65e8869bc4b4c6aa4fa2b21595256c7c2f6018a9d9ad61caf87187c1969"
const requestedImage = "legal-callegarin:test-fixture"

async function temporaryDirectory(t) {
  const directory = await mkdtemp(join(tmpdir(), "scanner-targets-test-"))
  t.after(() => rm(directory, { recursive: true, force: true }))
  return directory
}

async function fakeEngine(t) {
  const root = await temporaryDirectory(t)
  const engine = join(root, "fake-container-engine.mjs")
  const log = join(root, "engine.jsonl")
  const engineTmp = join(root, "engine-tmp")
  await mkdir(engineTmp)
  await writeFile(
    engine,
    `#!/usr/bin/env node
import { appendFileSync, writeFileSync } from "node:fs"

const args = process.argv.slice(2)
appendFileSync(process.env.FAKE_ENGINE_LOG, JSON.stringify(args) + "\\n")

if (args[0] === "image" && args[1] === "save") {
  const exitCode = Number(process.env.FAKE_EXPORT_EXIT ?? "0")
  if (exitCode !== 0) process.exit(exitCode)
  const outputIndex = args.indexOf("--output")
  if (outputIndex < 0 || !args[outputIndex + 1] || !args[outputIndex + 2]) {
    process.exit(90)
  }
  writeFileSync(args[outputIndex + 1], "fake docker archive")
  process.exit(0)
}

if (args[0] === "run" && args.includes(${JSON.stringify(syftImage)})) {
  const exitCode = Number(process.env.FAKE_SYFT_EXIT ?? "0")
  if (exitCode !== 0) process.exit(exitCode)
  if (process.env.FAKE_SBOM_INVALID === "1") {
    process.stdout.write("{invalid-json")
    process.exit(0)
  }
  const sourceNameIndex = args.indexOf("--source-name")
  const name = process.env.FAKE_SBOM_NAME ?? args[sourceNameIndex + 1]
  process.stdout.write(JSON.stringify({
    bomFormat: "CycloneDX",
    metadata: { component: { name } },
  }))
  process.exit(0)
}

if (args[0] === "run" && args.includes(${JSON.stringify(trivyImage)})) {
  process.exit(Number(process.env.FAKE_SCAN_EXIT ?? "0"))
}

process.exit(91)
`,
  )
  await chmod(engine, 0o700)
  return { root, engine, log, engineTmp }
}

function runMake(target, fixture, variables = {}, environment = {}) {
  const assignments = {
    CONTAINER_ENGINE: fixture.engine,
    ...variables,
  }
  return spawnSync(
    "make",
    [
      "--no-print-directory",
      target,
      ...Object.entries(assignments).map(([name, value]) => `${name}=${value}`),
    ],
    {
      cwd: repositoryRoot,
      encoding: "utf8",
      env: {
        ...process.env,
        TMPDIR: fixture.engineTmp,
        FAKE_ENGINE_LOG: fixture.log,
        ...environment,
      },
    },
  )
}

async function engineCalls(log) {
  const contents = await readFile(log, "utf8")
  return contents
    .trim()
    .split("\n")
    .filter(Boolean)
    .map((line) => JSON.parse(line))
}

async function assertTemporaryDataRemoved(fixture) {
  assert.deepEqual(await readdir(fixture.engineTmp), [])
}

test("Make exposes exact pinned, hardened scanner contracts and ignores artifacts", async () => {
  const [makefile, gitignore] = await Promise.all([
    readFile(join(repositoryRoot, "Makefile"), "utf8"),
    readFile(join(repositoryRoot, ".gitignore"), "utf8"),
  ])

  assert.match(makefile, /^CONTAINER_ENGINE \?= docker$/m)
  assert.match(makefile, /^SBOM_OUTPUT \?= artifacts\/sbom\.cdx\.json$/m)
  assert.match(makefile, /^SEVERITY \?= HIGH,CRITICAL$/m)
  assert.match(makefile, new RegExp(`^SYFT_IMAGE := ${syftImage}$`, "m"))
  assert.match(makefile, new RegExp(`^TRIVY_IMAGE := ${trivyImage}$`, "m"))
  assert.match(makefile, /^\.PHONY:.*\bsbom\b.*\bscan\b.*\bworkflow-policy\b/m)
  assert.match(makefile, /^check: workflow-policy$/m)
  assert.match(
    makefile,
    /^workflow-policy:\n\tnode scripts\/check-workflows\.mjs$/m,
  )
  assert.match(makefile, /docker-archive:\/scan\/image\.tar/)
  assert.match(makefile, /type=bind,src=.*dst=\/scan,readonly/)
  assert.match(makefile, /--tmpfs \/tmp:rw,nosuid,nodev,noexec,size=64m/)
  assert.match(makefile, /--output cyclonedx-json/)
  assert.match(makefile, /--source-name/)
  assert.match(makefile, /--scanners vuln/)
  assert.match(makefile, /--severity/)
  assert.match(makefile, /--ignore-unfixed=false/)
  assert.match(makefile, /--exit-code 1/)
  assert.doesNotMatch(makefile, /docker\.sock|\/var\/run|:latest\b/)
  assert.doesNotMatch(makefile, /\bcurl\b|\bwget\b|\bsudo\b/)
  assert.match(gitignore, /^\/artifacts\/$/m)
})

test("scanner targets reject an empty IMAGE before invoking the engine", async (t) => {
  const fixture = await fakeEngine(t)

  for (const target of ["sbom", "scan"]) {
    const result = runMake(target, fixture)
    assert.notEqual(result.status, 0)
    assert.match(`${result.stdout}\n${result.stderr}`, /IMAGE is required/i)
  }

  await assert.rejects(readFile(fixture.log, "utf8"), { code: "ENOENT" })
  await assertTemporaryDataRemoved(fixture)
})

test("sbom exports once, validates CycloneDX identity, writes atomically, and cleans", async (t) => {
  const fixture = await fakeEngine(t)
  const output = join(fixture.root, "artifacts", "requested.cdx.json")
  await mkdir(dirname(output), { recursive: true })
  await writeFile(output, "previous output")

  const result = runMake("sbom", fixture, {
    IMAGE: requestedImage,
    SBOM_OUTPUT: output,
  })
  assert.equal(result.status, 0, `${result.stdout}\n${result.stderr}`)

  const document = JSON.parse(await readFile(output, "utf8"))
  assert.equal(document.bomFormat, "CycloneDX")
  assert.equal(document.metadata.component.name, requestedImage)
  assert.deepEqual((await readdir(dirname(output))).sort(), [
    "requested.cdx.json",
  ])

  const calls = await engineCalls(fixture.log)
  assert.equal(calls.length, 2)
  assert.deepEqual(calls[0].slice(0, 3), ["image", "save", "--output"])
  assert.equal(calls[0][4], requestedImage)
  const archive = calls[0][3]
  assert.match(archive, /legal-callegarin-sbom\.[^/]+\/image\.tar$/)

  const syft = calls[1]
  assert.equal(syft[0], "run")
  assert.ok(syft.includes("--rm"))
  assert.ok(syft.includes("--network"))
  assert.ok(syft.includes("none"))
  assert.ok(syft.includes("--read-only"))
  assert.ok(syft.includes("--cap-drop"))
  assert.ok(syft.includes("ALL"))
  assert.ok(syft.includes("--security-opt"))
  assert.ok(syft.includes("no-new-privileges"))
  assert.deepEqual(
    syft.slice(syft.indexOf("--tmpfs"), syft.indexOf("--tmpfs") + 2),
    ["--tmpfs", "/tmp:rw,nosuid,nodev,noexec,size=64m"],
  )
  assert.ok(syft.includes(syftImage))
  assert.ok(syft.includes("docker-archive:/scan/image.tar"))
  assert.ok(syft.includes("--source-name"))
  assert.equal(syft[syft.indexOf("--source-name") + 1], requestedImage)
  assert.deepEqual(
    syft.slice(syft.indexOf("--output"), syft.indexOf("--output") + 2),
    ["--output", "cyclonedx-json"],
  )
  assert.equal(syft.includes("root"), false)
  const mount = syft[syft.indexOf("--mount") + 1]
  assert.equal(mount, `type=bind,src=${dirname(archive)},dst=/scan,readonly`)
  assert.equal(
    syft.some((argument) => argument.includes("docker.sock")),
    false,
  )
  assert.equal(
    syft.some((argument) => argument.includes(repositoryRoot)),
    false,
  )
  await assertTemporaryDataRemoved(fixture)
})

test("invalid or mismatched SBOM never replaces an existing output", async (t) => {
  for (const environment of [
    { FAKE_SBOM_INVALID: "1" },
    { FAKE_SBOM_NAME: "different:image" },
  ]) {
    const fixture = await fakeEngine(t)
    const output = join(fixture.root, "artifacts", "requested.cdx.json")
    await mkdir(dirname(output), { recursive: true })
    await writeFile(output, "trusted previous output")

    const result = runMake(
      "sbom",
      fixture,
      { IMAGE: requestedImage, SBOM_OUTPUT: output },
      environment,
    )
    assert.notEqual(result.status, 0)
    assert.equal(await readFile(output, "utf8"), "trusted previous output")
    assert.equal((await engineCalls(fixture.log)).length, 2)
    assert.match(
      `${result.stdout}\n${result.stderr}`,
      /valid CycloneDX JSON.*requested image/i,
    )
    assert.deepEqual((await readdir(dirname(output))).sort(), [
      "requested.cdx.json",
    ])
    await assertTemporaryDataRemoved(fixture)
  }
})

test("scan uses image vulnerabilities, exact severity, unfixed findings, and propagates failure", async (t) => {
  const fixture = await fakeEngine(t)
  const result = runMake(
    "scan",
    fixture,
    { IMAGE: requestedImage },
    { FAKE_SCAN_EXIT: "1" },
  )

  assert.notEqual(result.status, 0)
  const calls = await engineCalls(fixture.log)
  assert.equal(calls.length, 2)
  assert.deepEqual(calls[0].slice(0, 3), ["image", "save", "--output"])
  assert.equal(calls[0][4], requestedImage)

  const trivy = calls[1]
  assert.equal(trivy[0], "run")
  assert.ok(trivy.includes("--rm"))
  assert.ok(trivy.includes("--cap-drop"))
  assert.ok(trivy.includes("ALL"))
  assert.ok(trivy.includes("--security-opt"))
  assert.ok(trivy.includes("no-new-privileges"))
  assert.ok(trivy.includes(trivyImage))
  assert.deepEqual(trivy.slice(trivy.indexOf(trivyImage) + 1), [
    "image",
    "--input",
    "/scan/image.tar",
    "--scanners",
    "vuln",
    "--severity",
    "HIGH,CRITICAL",
    "--ignore-unfixed=false",
    "--exit-code",
    "1",
    "--no-progress",
  ])
  const mount = trivy[trivy.indexOf("--mount") + 1]
  assert.match(mount, /^type=bind,src=.*dst=\/scan,readonly$/)
  assert.equal(
    trivy.some((argument) => argument.includes("docker.sock")),
    false,
  )
  assert.equal(
    trivy.some((argument) => argument.includes(repositoryRoot)),
    false,
  )
  await assertTemporaryDataRemoved(fixture)
})

test("an image export failure is propagated before a scanner starts and still cleans", async (t) => {
  const fixture = await fakeEngine(t)
  const result = runMake(
    "scan",
    fixture,
    { IMAGE: requestedImage },
    { FAKE_EXPORT_EXIT: "17" },
  )

  assert.notEqual(result.status, 0)
  const calls = await engineCalls(fixture.log)
  assert.equal(calls.length, 1)
  assert.deepEqual(calls[0].slice(0, 3), ["image", "save", "--output"])
  await assertTemporaryDataRemoved(fixture)
})
