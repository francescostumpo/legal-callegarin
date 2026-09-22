import assert from "node:assert/strict"
import { spawnSync } from "node:child_process"
import { closeSync, mkdtempSync, openSync, readFileSync, rmSync } from "node:fs"
import {
  chmod,
  mkdir,
  mkdtemp,
  readFile,
  rm,
  writeFile,
} from "node:fs/promises"
import { tmpdir } from "node:os"
import { basename, dirname, join } from "node:path"
import { fileURLToPath } from "node:url"
import test from "node:test"

const script = fileURLToPath(new URL("check-workflows.mjs", import.meta.url))
const pinnedAction = "0123456789abcdef0123456789abcdef01234567"
const pinnedImage = "a".repeat(64)

async function temporaryDirectory(t) {
  const directory = await mkdtemp(join(tmpdir(), "workflow-policy-test-"))
  t.after(() => rm(directory, { recursive: true, force: true }))
  return directory
}

async function writeFixture(root, relativePath, contents) {
  const path = join(root, relativePath)
  await mkdir(dirname(path), { recursive: true })
  await writeFile(path, contents)
  return path
}

function runPolicy(cwd, paths = []) {
  const capture = mkdtempSync(join(tmpdir(), "workflow-policy-output-"))
  const stdoutPath = join(capture, "stdout")
  const stderrPath = join(capture, "stderr")
  const stdout = openSync(stdoutPath, "w")
  const stderr = openSync(stderrPath, "w")
  try {
    const result = spawnSync(process.execPath, [script, ...paths], {
      cwd,
      encoding: "utf8",
      stdio: ["ignore", stdout, stderr],
    })
    closeSync(stdout)
    closeSync(stderr)
    return {
      ...result,
      stdout: readFileSync(stdoutPath, "utf8"),
      stderr: readFileSync(stderrPath, "utf8"),
    }
  } finally {
    try {
      closeSync(stdout)
    } catch {}
    try {
      closeSync(stderr)
    } catch {}
    rmSync(capture, { recursive: true, force: true })
  }
}

function secureDeployment() {
  return [
    "name: secure production",
    "on:",
    "  workflow_dispatch:",
    "permissions:",
    "  contents: read",
    "concurrency:",
    "  group: production-deploy",
    "  cancel-in-progress: false",
    "jobs:",
    "  deploy-production:",
    "    name: Deploy production",
    "    environment: production",
    "    permissions:",
    "      contents: read",
    "      id-token: write",
    "    steps:",
    "      - uses: ./local-action",
    `      - uses: actions/checkout@${pinnedAction} # v4.2.2`,
    `      - uses: azure/login@${pinnedAction} # v2.3.0`,
    `      - uses: docker://ghcr.io/example/tool@sha256:${pinnedImage} # v1.0.0`,
    "      - env:",
    "          EVENT_VALUE: ${{ github.event.pull_request.title }}",
    "        run: printf '%s\\n' \"$EVENT_VALUE\"",
    "",
  ].join("\n")
}

test("the bounded source check keeps actionlint explicitly mandatory", async () => {
  const source = await readFile(script, "utf8")
  assert.match(source, /defense-in-depth.*actionlint.*mandatory/is)
  assert.doesNotMatch(source, /from ["'](?:yaml|js-yaml)["']/)
})

test("the default workflow directory may be absent or empty", async (t) => {
  const root = await temporaryDirectory(t)

  let result = runPolicy(root)
  assert.equal(result.status, 0, result.stderr)

  await mkdir(join(root, ".github", "workflows"), { recursive: true })
  result = runPolicy(root)
  assert.equal(result.status, 0, result.stderr)
})

test("secure pinned workflows, local actions, comments, CRLF, and multiple files pass", async (t) => {
  const root = await temporaryDirectory(t)
  const first = await writeFixture(
    root,
    "secure-a.yml",
    secureDeployment().replaceAll("\n", "\r\n"),
  )
  const second = await writeFixture(
    root,
    "secure-b.yaml",
    [
      "# pull_request_target:",
      "# uses: actions/checkout@latest",
      "on:",
      "  push:",
      "permissions:",
      "  contents: read",
      "jobs:",
      "  test:",
      "    steps:",
      `      - uses: actions/setup-node@${pinnedAction} # v6.0.0`,
      `      - "uses": actions/checkout@${pinnedAction} # v4.2.2`,
      "      - 'run': node --version",
      "      - run: printf '%s' '[literal] {literal}'",
      "",
    ].join("\n"),
  )

  const explicit = runPolicy(root, [second, first])
  assert.equal(explicit.status, 0, explicit.stderr)
  assert.equal(explicit.stdout, "")

  await writeFixture(root, ".github/workflows/secure.yml", secureDeployment())
  const byDefault = runPolicy(root)
  assert.equal(byDefault.status, 0, byDefault.stderr)
})

test("every forbidden workflow construct fails with file, line, and rule", async (t) => {
  const root = await temporaryDirectory(t)
  const pushTrigger = "on:\n  push:"
  const base = [
    "name: fixture",
    pushTrigger,
    "permissions:",
    "  contents: read",
    "jobs:",
    "  test:",
    "    steps:",
    "      - run: node --version",
    "",
  ].join("\n")
  const cases = [
    {
      name: "pull-request-target",
      rule: "pull-request-target",
      marker: "pull_request_target:",
      source: base.replace(pushTrigger, "on:\n  pull_request_target:"),
    },
    {
      name: "pull-request-target-flow",
      rule: "pull-request-target",
      marker: "on: [pull_request_target]",
      source: base.replace(pushTrigger, "on: [pull_request_target]"),
    },
    {
      name: "pull-request-target-quoted-flow",
      rule: "pull-request-target",
      marker: 'on: ["pull_request_target"]',
      source: base.replace(pushTrigger, 'on: ["pull_request_target"]'),
    },
    {
      name: "pull-request-target-quoted-key",
      rule: "pull-request-target",
      marker: '"pull_request_target":',
      source: base.replace(pushTrigger, 'on:\n  "pull_request_target":'),
    },
    {
      name: "pull-request-target-scalar",
      rule: "pull-request-target",
      marker: "on: pull_request_target",
      source: base.replace(pushTrigger, "on: pull_request_target"),
    },
    {
      name: "pull-request-target-quoted-scalar",
      rule: "pull-request-target",
      marker: 'on: "pull_request_target"',
      source: base.replace(pushTrigger, 'on: "pull_request_target"'),
    },
    {
      name: "pull-request-target-block-sequence",
      rule: "pull-request-target",
      marker: "- pull_request_target",
      source: base.replace(
        pushTrigger,
        "on:\n  - push\n  - pull_request_target",
      ),
    },
    {
      name: "pull-request-target-flow-mapping",
      rule: "pull-request-target",
      marker: "pull_request_target: {}",
      source: base.replace(
        pushTrigger,
        "on: { push: {}, pull_request_target: {} }",
      ),
    },
    {
      name: "pull-request-target-multiline-flow-sequence",
      rule: "unsupported-flow-syntax",
      marker: "on: [",
      source: base.replace(
        pushTrigger,
        "on: [\n  push,\n  pull_request_target\n]",
      ),
    },
    {
      name: "pull-request-target-multiline-flow-mapping",
      rule: "unsupported-flow-syntax",
      marker: "on: {",
      source: base.replace(
        pushTrigger,
        "on: {\n  push: {},\n  pull_request_target: {}\n}",
      ),
    },
    {
      name: "anchored-multiline-flow-event-fails-closed",
      rule: "unsupported-flow-syntax",
      marker: "on: &events [",
      source: base.replace(
        pushTrigger,
        "on: &events [\n  push,\n  pull_request_target\n]",
      ),
    },
    {
      name: "unpinned-action",
      rule: "pinned-uses",
      marker: "actions/checkout@v4",
      source: base.replace(
        "      - run: node --version",
        "      - uses: actions/checkout@v4 # v4.2.2",
      ),
    },
    {
      name: "unpinned-action-quoted-key",
      rule: "pinned-uses",
      marker: '"uses": actions/checkout@v4',
      source: base.replace(
        "      - run: node --version",
        '      - "uses": actions/checkout@v4 # v4.2.2',
      ),
    },
    {
      name: "flow-style-uses-fails-closed",
      rule: "unsupported-uses-syntax",
      marker: "{ uses: actions/checkout@v4 }",
      source: base.replace(
        "      - run: node --version",
        "      - { uses: actions/checkout@v4 }",
      ),
    },
    {
      name: "inline-flow-sequence-uses-fails-closed",
      rule: "unsupported-uses-syntax",
      marker: "{ uses: actions/checkout@v4 }",
      source: base.replace(
        "    steps:\n      - run: node --version",
        "    steps: [{ uses: actions/checkout@v4 }]",
      ),
    },
    {
      name: "flow-style-run-fails-closed",
      rule: "unsupported-flow-syntax",
      marker: "{ run:",
      source: base.replace(
        "      - run: node --version",
        `      - { run: "printf '%s' '\${{ github.event.issue.title }}'" }`,
      ),
    },
    {
      name: "inline-flow-sequence-run-fails-closed",
      rule: "unsupported-flow-syntax",
      marker: "[{ run:",
      source: base.replace(
        "    steps:\n      - run: node --version",
        `    steps: [{ run: "printf '%s' '\${{ github.event.issue.title }}'" }]`,
      ),
    },
    {
      name: "uppercase-action-sha",
      rule: "pinned-uses",
      marker: pinnedAction.toUpperCase(),
      source: base.replace(
        "      - run: node --version",
        `      - uses: actions/checkout@${pinnedAction.toUpperCase()} # v4.2.2`,
      ),
    },
    {
      name: "missing-release-comment",
      rule: "release-comment",
      marker: `actions/checkout@${pinnedAction}`,
      source: base.replace(
        "      - run: node --version",
        `      - uses: actions/checkout@${pinnedAction}`,
      ),
    },
    {
      name: "write-all",
      rule: "workflow-permissions",
      marker: "permissions: write-all",
      source: base.replace(
        "permissions:\n  contents: read",
        "permissions: write-all",
      ),
    },
    {
      name: "workflow-write",
      rule: "workflow-permissions",
      marker: "packages: write",
      source: base.replace("contents: read", "packages: write"),
    },
    {
      name: "workflow-write-double-quoted",
      rule: "workflow-permissions",
      marker: 'packages: "write"',
      source: base.replace("contents: read", 'packages: "write"'),
    },
    {
      name: "workflow-write-single-quoted",
      rule: "workflow-permissions",
      marker: "packages: 'write'",
      source: base.replace("contents: read", "packages: 'write'"),
    },
    {
      name: "workflow-write-double-quoted-keys",
      rule: "workflow-permissions",
      marker: '"packages": "write"',
      source: base.replace(
        "permissions:\n  contents: read",
        '"permissions":\n  "packages": "write"',
      ),
    },
    {
      name: "workflow-write-single-quoted-keys",
      rule: "workflow-permissions",
      marker: "'packages': 'write'",
      source: base.replace(
        "permissions:\n  contents: read",
        "'permissions':\n  'packages': 'write'",
      ),
    },
    {
      name: "azure-creds",
      rule: "azure-credentials",
      marker: "creds: ${{ secrets.AZURE_CREDENTIALS }}",
      source: secureDeployment().replace(
        `      - uses: azure/login@${pinnedAction} # v2.3.0`,
        `      - uses: azure/login@${pinnedAction} # v2.3.0\n        with:\n          creds: \${{ secrets.AZURE_CREDENTIALS }}`,
      ),
    },
    {
      name: "azure-creds-double-quoted-key",
      rule: "azure-credentials",
      marker: '"creds": ${{ secrets.AZURE_CREDENTIALS }}',
      source: secureDeployment().replace(
        `      - uses: azure/login@${pinnedAction} # v2.3.0`,
        `      - uses: azure/login@${pinnedAction} # v2.3.0\n        with:\n          "creds": \${{ secrets.AZURE_CREDENTIALS }}`,
      ),
    },
    {
      name: "azure-creds-single-quoted-key",
      rule: "azure-credentials",
      marker: "'creds': ${{ secrets.AZURE_CREDENTIALS }}",
      source: secureDeployment().replace(
        `      - uses: azure/login@${pinnedAction} # v2.3.0`,
        `      - uses: azure/login@${pinnedAction} # v2.3.0\n        with:\n          'creds': \${{ secrets.AZURE_CREDENTIALS }}`,
      ),
    },
    {
      name: "azure-creds-flow-style-fails-closed",
      rule: "unsupported-flow-syntax",
      marker: "with: { creds:",
      source: secureDeployment().replace(
        `      - uses: azure/login@${pinnedAction} # v2.3.0`,
        `      - uses: azure/login@${pinnedAction} # v2.3.0\n        with: { creds: \${{ secrets.AZURE_CREDENTIALS }} }`,
      ),
    },
    {
      name: "azure-client-secret",
      rule: "azure-credentials",
      marker: "AZURE_CLIENT_SECRET:",
      source: base.replace(
        "    steps:",
        "    env:\n      AZURE_CLIENT_SECRET: ${{ secrets.CLIENT_SECRET }}\n    steps:",
      ),
    },
    {
      name: "latest-image",
      rule: "mutable-latest",
      marker: "image: ghcr.io/example/tool:latest",
      source: base.replace(
        "    steps:",
        "    container:\n      image: ghcr.io/example/tool:latest\n    steps:",
      ),
    },
    {
      name: "event-expression-inline-run",
      rule: "event-in-run",
      marker: "github.event.issue.title",
      source: base.replace(
        "node --version",
        "printf '%s' '${{ github.event.issue.title }}'",
      ),
    },
    {
      name: "event-expression-inline-double-quoted-run-key",
      rule: "event-in-run",
      marker: "github.event.issue.title",
      source: base.replace(
        "      - run: node --version",
        `      - "run": printf '%s' '\${{ github.event.issue.title }}'`,
      ),
    },
    {
      name: "event-expression-inline-single-quoted-run-key",
      rule: "event-in-run",
      marker: "github.event.issue.title",
      source: base.replace(
        "      - run: node --version",
        `      - 'run': printf '%s' '\${{ github.event.issue.title }}'`,
      ),
    },
    {
      name: "event-expression-block-run",
      rule: "event-in-run",
      marker: "github.event.pull_request.body",
      source: base.replace(
        "      - run: node --version",
        "      - run: |\n          node --version\n          printf '%s' '${{ github.event.pull_request.body }}'",
      ),
    },
    ...["|-", "|+", ">-", ">+"].map((indicator) => ({
      name: `event-expression-block-run-${indicator.replace("|", "literal").replace(">", "folded").replace("+", "keep").replace("-", "strip")}`,
      rule: "event-in-run",
      marker: "github.event.pull_request.body",
      source: base.replace(
        "      - run: node --version",
        `      - run: ${indicator}\n          node --version\n          printf '%s' '\${{ github.event.pull_request.body }}'`,
      ),
    })),
    {
      name: "event-expression-block-double-quoted-run-key",
      rule: "event-in-run",
      marker: "github.event.pull_request.body",
      source: base.replace(
        "      - run: node --version",
        `      - "run": >+\n          node --version\n          printf '%s' '\${{ github.event.pull_request.body }}'`,
      ),
    },
    {
      name: "event-expression-block-single-quoted-run-key",
      rule: "event-in-run",
      marker: "github.event.pull_request.body",
      source: base.replace(
        "      - run: node --version",
        `      - 'run': |-\n          node --version\n          printf '%s' '\${{ github.event.pull_request.body }}'`,
      ),
    },
    {
      name: "production-environment",
      rule: "production-environment",
      marker: "deploy-production:",
      source: secureDeployment().replace("    environment: production\n", ""),
    },
    {
      name: "production-concurrency",
      rule: "production-concurrency",
      marker: "deploy-production:",
      source: secureDeployment().replace(
        "concurrency:\n  group: production-deploy\n  cancel-in-progress: false\n",
        "",
      ),
    },
    {
      name: "production-cancellation",
      rule: "production-concurrency",
      marker: "cancel-in-progress: true",
      source: secureDeployment().replace(
        "cancel-in-progress: false",
        "cancel-in-progress: true",
      ),
    },
    {
      name: "unstable-production-group",
      rule: "production-concurrency",
      marker: "group: ${{ github.ref }}",
      source: secureDeployment().replace(
        "group: production-deploy",
        "group: ${{ github.ref }}",
      ),
    },
    {
      name: "quoted-jobs-key-preserves-production-checks",
      rule: "production-environment",
      marker: "deploy-production:",
      source: secureDeployment()
        .replace("jobs:", '"jobs":')
        .replace("    environment: production\n", ""),
    },
    {
      name: "quoted-production-job-id-preserves-production-checks",
      rule: "production-concurrency",
      marker: "'deploy-production':",
      source: secureDeployment()
        .replace("  deploy-production:", "  'deploy-production':")
        .replace(
          "concurrency:\n  group: production-deploy\n  cancel-in-progress: false\n",
          "",
        ),
    },
  ]

  for (const fixture of cases) {
    await t.test(fixture.name, async () => {
      const path = await writeFixture(
        root,
        `${fixture.name}.yml`,
        fixture.source,
      )
      const result = runPolicy(root, [path])
      const expectedLine = fixture.source
        .slice(0, fixture.source.indexOf(fixture.marker))
        .split("\n").length

      assert.notEqual(result.status, 0, "forbidden fixture unexpectedly passed")
      assert.match(
        result.stderr,
        new RegExp(
          `${basename(path).replaceAll(".", "\\.")}:${expectedLine}: \\[${fixture.rule}\\]`,
        ),
      )
    })
  }
})

test("explicit missing, unreadable, and non-file paths fail closed", async (t) => {
  const root = await temporaryDirectory(t)
  const missing = join(root, "missing.yml")

  let result = runPolicy(root, [missing])
  assert.notEqual(result.status, 0)
  assert.match(result.stderr, /missing\.yml:0: \[path\].*readable YAML file/i)

  const unreadable = await writeFixture(root, "unreadable.yml", "on: [push]\n")
  await chmod(unreadable, 0o000)
  result = runPolicy(root, [unreadable])
  assert.notEqual(result.status, 0)
  assert.match(
    result.stderr,
    /unreadable\.yml:0: \[path\].*readable YAML file/i,
  )
  await chmod(unreadable, 0o600)

  const directory = join(root, "directory.yml")
  await mkdir(directory)
  result = runPolicy(root, [directory])
  assert.notEqual(result.status, 0)
  assert.match(result.stderr, /directory\.yml:0: \[path\].*regular YAML file/i)
})
