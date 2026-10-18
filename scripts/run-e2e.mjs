import { randomBytes } from "node:crypto"
import { spawn } from "node:child_process"
import { fileURLToPath } from "node:url"
import path from "node:path"

const repositoryRoot = path.resolve(
  path.dirname(fileURLToPath(import.meta.url)),
  "..",
)
const browserProjects = ["chromium", "firefox", "webkit"]

export function createE2EEnvironment(baseEnvironment, generate = randomBytes) {
  const environment = { ...baseEnvironment }
  delete environment.ADMIN_USERNAME
  delete environment.ADMIN_PASSWORD_HASH
  delete environment.SESSION_KEY_BASE64
  environment.E2E_ADMIN_USERNAME = "e2e-admin"
  environment.E2E_ADMIN_PASSWORD = generate(32).toString("base64url")
  environment.E2E_SESSION_KEY_BASE64 = generate(32).toString("base64")
  environment.E2E_RUN_SECRET = generate(32).toString("base64url")
  return environment
}

export function playwrightInvocation(root, forwardedArguments, environment) {
  return {
    command: path.join(root, "node_modules", ".bin", "playwright"),
    args: ["test", ...forwardedArguments],
    options: {
      cwd: root,
      env: environment,
      shell: false,
      stdio: "inherit",
    },
  }
}

function projectInvocations(forwardedArguments) {
  const hasExplicitProject = forwardedArguments.some(
    (argument) =>
      argument === "--project" ||
      argument.startsWith("--project=") ||
      argument === "-p",
  )
  if (hasExplicitProject) return [forwardedArguments]
  return browserProjects.map((project) => [
    ...forwardedArguments,
    `--project=${project}`,
  ])
}

export async function runE2E(
  forwardedArguments,
  {
    baseEnvironment = process.env,
    generate = randomBytes,
    signalTarget = process,
    spawnChild = spawn,
  } = {},
) {
  for (const projectArguments of projectInvocations(forwardedArguments)) {
    const environment = createE2EEnvironment(baseEnvironment, generate)
    const invocation = playwrightInvocation(
      repositoryRoot,
      projectArguments,
      environment,
    )
    const child = spawnChild(
      invocation.command,
      invocation.args,
      invocation.options,
    )
    const signals = ["SIGINT", "SIGTERM"]
    const signalHandlers = new Map(
      signals.map((signal) => [signal, () => child.kill(signal)]),
    )
    for (const signal of signals)
      signalTarget.once(signal, signalHandlers.get(signal))

    const result = await new Promise((resolve, reject) => {
      child.once("error", reject)
      child.once("exit", (code, signal) => resolve({ code, signal }))
    }).finally(() => {
      for (const signal of signals)
        signalTarget.off(signal, signalHandlers.get(signal))
    })
    if (result.signal !== null || result.code !== 0) return 1
  }
  return 0
}

const invokedDirectly =
  process.argv[1] !== undefined &&
  path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)

if (invokedDirectly) {
  try {
    process.exitCode = await runE2E(process.argv.slice(2))
  } catch {
    process.stderr.write("Unable to start the local E2E runner.\n")
    process.exitCode = 1
  }
}
