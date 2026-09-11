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
