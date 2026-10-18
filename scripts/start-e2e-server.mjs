import { spawn } from "node:child_process"
import { mkdirSync } from "node:fs"
import { fileURLToPath } from "node:url"
import path from "node:path"

const repositoryRoot = path.resolve(
  path.dirname(fileURLToPath(import.meta.url)),
  "..",
)
const binaryPath = path.join(repositoryRoot, "bin", "legal-callegarin-e2e")

function buildEnvironment(source) {
  const environment = { ...source }
  for (const key of [
    "ADMIN_USERNAME",
    "ADMIN_PASSWORD_HASH",
    "SESSION_KEY_BASE64",
    "E2E_ADMIN_USERNAME",
    "E2E_ADMIN_PASSWORD",
    "E2E_SESSION_KEY_BASE64",
    "E2E_RUN_SECRET",
  ]) {
    delete environment[key]
  }
  environment.GOCACHE = path.join(
    repositoryRoot,
    "artifacts",
    "playwright",
    "go-build-cache",
  )
  return environment
}

function run(command, args, environment) {
  return new Promise((resolve, reject) => {
    const child = spawn(command, args, {
      cwd: repositoryRoot,
      env: environment,
      shell: false,
      stdio: "inherit",
    })
    child.once("error", reject)
    child.once("exit", (code, signal) => {
      if (code === 0 && signal === null) resolve()
      else reject(new Error("E2E build command failed"))
    })
  })
}

try {
  const safeBuildEnvironment = buildEnvironment(process.env)
  await run("npm", ["run", "build"], safeBuildEnvironment)
  mkdirSync(path.dirname(binaryPath), { recursive: true })
  await run(
    "go",
    ["build", "-tags=e2e", "-o", binaryPath, "./cmd/web"],
    safeBuildEnvironment,
  )

  const server = spawn(binaryPath, [], {
    cwd: repositoryRoot,
    env: process.env,
    shell: false,
    stdio: "inherit",
  })
  for (const signal of ["SIGINT", "SIGTERM"]) {
    process.once(signal, () => server.kill(signal))
  }
  server.once("error", () => {
    process.stderr.write("Unable to start the local E2E server.\n")
    process.exitCode = 1
  })
  server.once("exit", (code, signal) => {
    process.exitCode = signal === null ? (code ?? 1) : 1
  })
} catch {
  process.stderr.write("Unable to prepare the local E2E server.\n")
  process.exitCode = 1
}
