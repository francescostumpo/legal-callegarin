import assert from "node:assert/strict"
import { readFile, stat } from "node:fs/promises"
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
  const runbook = await readRepositoryFile("docs/article-storage-rollout.md")

  const shellBlocks = [...runbook.matchAll(/```bash\n([\s\S]*?)```/g)]
  assert.ok(shellBlocks.length > 0)
  for (const block of shellBlocks) {
    assert.match(block[1], /^set -eu\n/)
  }
  assert.match(runbook, /length\(items\[\?id == `null` \|\| RowKey == id\]\)/)
  assert.match(runbook, /test "\$LEGACY_COUNT" = 0 \|\|/)
  assert.equal(
    [...runbook.matchAll(/^assert_no_legacy_article_rows$/gm)].length,
    2,
  )

  const trafficCommands = [
    ...runbook.matchAll(/az containerapp ingress traffic set/g),
  ]
  assert.equal(trafficCommands.length, 3)
  for (const command of trafficCommands) {
    const preflight = runbook.slice(
      Math.max(0, command.index - 700),
      command.index,
    )
    assert.match(
      preflight,
      /az containerapp revision set-mode[\s\S]*--mode multiple[\s\S]*ACTIVE_REVISIONS_MODE="\$\([\s\S]*properties\.configuration\.activeRevisionsMode[\s\S]*test "\$ACTIVE_REVISIONS_MODE" = Multiple/,
    )
  }
})
