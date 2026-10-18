import assert from "node:assert/strict"
import { EventEmitter } from "node:events"
import { readFileSync } from "node:fs"
import { test } from "node:test"

import {
  createE2EEnvironment,
  playwrightInvocation,
  runE2E,
} from "./run-e2e.mjs"

test("launcher creates fresh strong secrets without exposing normal config keys", () => {
  let byte = 0
  const randomBytes = (length) =>
    Buffer.from(Array.from({ length }, () => (byte++ % 251) + 1))

  const environment = createE2EEnvironment(
    {
      PATH: "/usr/bin",
      ADMIN_PASSWORD_HASH: "discard-me",
      SESSION_KEY_BASE64: "discard-me",
    },
    randomBytes,
  )

  assert.equal(environment.PATH, "/usr/bin")
  assert.equal(environment.ADMIN_PASSWORD_HASH, undefined)
  assert.equal(environment.SESSION_KEY_BASE64, undefined)
  assert.equal(environment.E2E_ADMIN_USERNAME, "e2e-admin")
  assert.match(environment.E2E_ADMIN_PASSWORD, /^[A-Za-z0-9_-]{43}$/)
  assert.match(environment.E2E_RUN_SECRET, /^[A-Za-z0-9_-]{43}$/)
  assert.equal(
    Buffer.from(environment.E2E_SESSION_KEY_BASE64, "base64").length,
    32,
  )
  assert.notEqual(environment.E2E_ADMIN_PASSWORD, environment.E2E_RUN_SECRET)

  const nextEnvironment = createE2EEnvironment({}, randomBytes)
  assert.notEqual(nextEnvironment.E2E_RUN_SECRET, environment.E2E_RUN_SECRET)
})

test("launcher invokes the repository-local Playwright executable without a shell", () => {
  const invocation = playwrightInvocation("/workspace", ["--project=chromium"])

  assert.equal(invocation.command, "/workspace/node_modules/.bin/playwright")
  assert.deepEqual(invocation.args, ["test", "--project=chromium"])
  assert.equal(invocation.options.shell, false)
  assert.equal(invocation.options.stdio, "inherit")
})

test("launcher isolates implicit browser projects in sequential fresh processes", async () => {
  const spawned = []
  const signalTarget = new EventEmitter()
  let generatedByte = 0

  const result = await runE2E([], {
    baseEnvironment: { PATH: "/usr/bin" },
    generate: (length) => Buffer.alloc(length, ++generatedByte),
    signalTarget,
    spawnChild: (command, args, options) => {
      const child = new EventEmitter()
      child.kill = () => {}
      spawned.push({ command, args, environment: options.env })
      queueMicrotask(() => child.emit("exit", 0, null))
      return child
    },
  })

  assert.equal(result, 0)
  assert.deepEqual(
    spawned.map(({ args }) => args),
    [
      ["test", "--project=chromium"],
      ["test", "--project=firefox"],
      ["test", "--project=webkit"],
    ],
  )
  assert.equal(
    new Set(spawned.map(({ environment }) => environment.E2E_RUN_SECRET)).size,
    3,
  )
})

test("launcher forwards termination signals and removes its handlers", async () => {
  const child = new EventEmitter()
  const signalTarget = new EventEmitter()
  const forwarded = []
  child.kill = (signal) => forwarded.push(signal)

  const result = runE2E(["--list"], {
    baseEnvironment: {},
    generate: (length) => Buffer.alloc(length, 7),
    signalTarget,
    spawnChild: () => child,
  })
  signalTarget.emit("SIGINT")
  child.emit("exit", null, "SIGINT")

  assert.equal(await result, 1)
  assert.deepEqual(forwarded, ["SIGINT"])
  assert.equal(signalTarget.listenerCount("SIGINT"), 0)
  assert.equal(signalTarget.listenerCount("SIGTERM"), 0)
})

test("credential smoke opts out of traces that could persist secrets", () => {
  const source = readFileSync(
    new URL("../e2e/smoke.spec.ts", import.meta.url),
    "utf8",
  )
  const traceOptOut = source.indexOf('test.use({ trace: "off" })')
  const credentialUse = source.indexOf("process.env.E2E_ADMIN_PASSWORD")

  assert.ok(traceOptOut >= 0 && traceOptOut < credentialUse)
  assert.doesNotMatch(source, /storageState/)
})

test("self-signed loopback server and browser both ignore certificate trust", () => {
  const source = readFileSync(
    new URL("../playwright.config.ts", import.meta.url),
    "utf8",
  )

  assert.match(
    source,
    /use:\s*{[\s\S]*?ignoreHTTPSErrors:\s*true[\s\S]*?webServer:/,
  )
  assert.match(source, /webServer:\s*{[\s\S]*?ignoreHTTPSErrors:\s*true/)
})
