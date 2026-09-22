import assert from "node:assert/strict"
import { readFile } from "node:fs/promises"
import test from "node:test"

const repositoryRoot = new URL("../", import.meta.url)

async function source(path, { optional = false } = {}) {
  try {
    return await readFile(new URL(path, repositoryRoot), "utf8")
  } catch (error) {
    if (optional && error?.code === "ENOENT") return ""
    throw error
  }
}

const [
  guide,
  readme,
  operations,
  articleRollout,
  makefile,
  packageManifest,
  goManifest,
  dockerfile,
  ciWorkflow,
  deployWorkflow,
  mainBicep,
  containerAppBicep,
  mainParameters,
  deployScript,
  containerSmoke,
  e2eRunner,
  compose,
] = await Promise.all([
  source("docs/development-and-first-release.md", { optional: true }),
  source("README.md"),
  source("docs/operations.md"),
  source("docs/article-storage-rollout.md"),
  source("Makefile"),
  source("package.json"),
  source("go.mod"),
  source("Dockerfile"),
  source(".github/workflows/ci.yml"),
  source(".github/workflows/deploy.yml"),
  source("infra/main.bicep"),
  source("infra/modules/container-app.bicep"),
  source("infra/main.example.bicepparam"),
  source("scripts/deploy-container-app.sh"),
  source("scripts/container-smoke.sh"),
  source("scripts/run-e2e.mjs"),
  source("compose.test.yaml"),
])

function requireTerms(document, terms) {
  for (const term of terms) assert.match(document, term)
}

function assertOrdered(document, terms) {
  let previous = -1
  for (const term of terms) {
    const match = term.exec(document.slice(previous + 1))
    assert.ok(match, `missing ordered term ${term}`)
    previous += match.index + 1
  }
}

function markdownSection(document, heading, nextHeadingPrefix = "## ") {
  const start = document.indexOf(heading)
  assert.ok(start >= 0, `missing section ${heading}`)
  const next = document.indexOf(
    `\n${nextHeadingPrefix}`,
    start + heading.length,
  )
  return document.slice(start, next < 0 ? document.length : next)
}

function makeTarget(document, target) {
  const lines = document.split("\n")
  const start = lines.findIndex((line) => line.startsWith(`${target}:`))
  assert.ok(start >= 0, `missing Make target ${target}`)
  const recipe = [lines[start]]
  for (let index = start + 1; index < lines.length; index += 1) {
    const line = lines[index]
    if (line === "" || line.startsWith("\t")) {
      recipe.push(line)
      continue
    }
    break
  }
  return recipe.join("\n")
}

function assertCheckRecipeExclusions(recipe) {
  const forbiddenGates = [
    ["Go race", /go\s+test[^\n]*[ \t]-race(?:[ \t]|$)/im],
    [
      "browser E2E",
      /npm\s+run\s+e2e(?:[ \t]|:|$)|(?:^|[\/\s])playwright(?:[ \t]|$)/im,
    ],
    [
      "Go integration tags",
      /go\s+test[^\n]*[ \t]-tags(?:=|[ \t]+)integration(?:[ \t]|$)/im,
    ],
    [
      "actionlint",
      /(?:\$\(\s*MAKE\s*\)|\bmake)\s+actionlint(?:[ \t]|$)|(?:^|[\/\s])actionlint(?:[ \t]|$)/im,
    ],
    ["Azurite or Compose", /\bazurite\b|docker\s+compose|compose\.test/i],
    ["container smoke", /container-smoke/i],
    [
      "SBOM",
      /(?:\$\(\s*MAKE\s*\)|\bmake)\s+sbom(?:[ \t]|$)|(?:^|[\/\s])sbom(?:[ \t]|$)/im,
    ],
    [
      "image scan",
      /(?:\$\(\s*MAKE\s*\)|\bmake)\s+scan(?:[ \t]|$)|(?:^|[\/\s])scan(?:[ \t]|$)/im,
    ],
  ]
  for (const [name, pattern] of forbiddenGates) {
    assert.doesNotMatch(recipe, pattern, `make check must exclude ${name}`)
  }
}

function assertNoInventedToolMinimums(prerequisites) {
  for (const pattern of [
    /Docker(?: Engine| CLI)?\s+(?:(?:versione|version)\s*)?(?:>=?\s*)?v?\d+\.\d+/i,
    /Azure CLI\s+(?:(?:versione|version)\s*)?(?:>=?\s*)?v?\d+\.\d+/i,
  ]) {
    assert.doesNotMatch(
      prerequisites,
      pattern,
      "Docker and Azure CLI must not gain invented numeric minimums",
    )
  }
}

function numberedCredentialItems(document) {
  const starts = [...document.matchAll(/^(\d+)\. /gm)]
  return starts.map((match, index) =>
    document.slice(match.index, starts[index + 1]?.index ?? document.length),
  )
}

function assertCredentialIsolation(document) {
  const items = numberedCredentialItems(document)
  assert.equal(items.length, 3)
  assert.match(items[0], /bootstrap[^.]*PAT[^.]*classic[^.]*`write:packages`/is)
  assert.doesNotMatch(items[0], /`read:packages`|`Actions: read`/)
  assert.match(items[1], /runtime[^.]*PAT[^.]*classic[^.]*`read:packages`/is)
  assert.doesNotMatch(items[1], /`write:packages`|`Actions: read`/)
  assert.match(items[2], /OIDC[^.]*fine-grained[^.]*`Actions: read`/is)
  assert.doesNotMatch(items[2], /`write:packages`|`read:packages`/)
}

function assertBuildDigestComparison(document) {
  requireTerms(document, [
    /BOOTSTRAP_METADATA="\$BOOTSTRAP_DOCKER_CONFIG\/build-metadata\.json"/,
    /--metadata-file "\$BOOTSTRAP_METADATA"/,
    /"containerimage\.digest"/,
    /if \[\[ ! "\$BUILD_DIGEST" =~ \^sha256:\[0-9a-f\]\{64\}\$ \]\]/,
    /if \[\[ ! "\$REGISTRY_DIGEST" =~ \^sha256:\[0-9a-f\]\{64\}\$ \]\]/,
    /if \[\[ "\$REGISTRY_DIGEST" != "\$BUILD_DIGEST" \]\]/,
    /IMAGE_REFERENCE="\$IMAGE_REPOSITORY@\$BUILD_DIGEST"/,
    /rm -f -- "\$BOOTSTRAP_METADATA"/,
    /rm -rf -- "\$BOOTSTRAP_DOCKER_CONFIG"/,
  ])
}

function assertWarningFreeBicepWrapper(document) {
  requireTerms(document, [
    /BICEP_STDERR="\$AZURE_CONFIG_DIR\/bicep-stderr"/,
    /run_bicep_warning_free\(\)/,
    /: > "\$BICEP_STDERR"/,
    /if ! "\$@" 2>"\$BICEP_STDERR"/,
    /if \[ -s "\$BICEP_STDERR" \]/,
    /unset GHCR_TOKEN ADMIN_PASSWORD_HASH SESSION_KEY_BASE64/,
  ])
  assert.doesNotMatch(document, /cat[^\n]*BICEP_STDERR/)
  const commands = document
    .split("\n")
    .filter((line) => line.includes("az bicep "))
  assert.ok(commands.length >= 6)
  for (const command of commands) {
    assert.match(command, /run_bicep_warning_free az bicep /)
  }
}

test("the guide is linked and its prerequisites match repository toolchain evidence", () => {
  assert.match(goManifest, /^go 1\.27\.1$/m)
  assert.match(packageManifest, /"node":\s*"24\.x"/)
  assert.match(dockerfile, /FROM node:24\.21\.0-/)
  assert.match(dockerfile, /FROM golang:1\.27\.1-/)
  assert.match(ciWorkflow, /node-version:\s*24\.21\.0/)
  assert.match(ciWorkflow, /az bicep install --version v0\.45\.15/)

  requireTerms(guide, [
    /Go `1\.27\.1`/,
    /Node(?:\.js)? `24\.21\.0`/,
    /Bicep `0\.45\.15`/,
    /npm[^.]*toolchain Node/is,
    /Git[^.]*Make/is,
    /Azure CLI[^.]*versione esatta[^.]*Bicep/is,
    /Docker Engine[^.]*CLI[^.]*Buildx[^.]*Compose/is,
    /bash[^.]*POSIX[^.]*curl[^.]*dig[^.]*OpenSSL/is,
    /primi download[^.]*browser[^.]*pull[^.]*rete[^.]*privilegi/is,
    /nessun[^.]*minimo numerico[^.]*Docker[^.]*Azure CLI/is,
  ])
  assert.match(
    readme,
    /\[(?:guida|development)[^\]]*(?:sviluppo|primo rilascio|first-release)[^\]]*\]\(docs\/development-and-first-release\.md\)/i,
  )
  assert.match(
    operations,
    /\[guida[^\]]*(?:sviluppo|primo rilascio)[^\]]*\]\(development-and-first-release\.md\)/i,
  )

  const prerequisites = markdownSection(guide, "## Prerequisiti")
  assertNoInventedToolMinimums(prerequisites)
  for (const inventedMinimum of ["- Docker Engine 27.0", "- Azure CLI 2.80"]) {
    assert.throws(
      () =>
        assertNoInventedToolMinimums(`${prerequisites}\n${inventedMinimum}`),
      /invented numeric minimums/,
    )
  }
})

test("make check is described exactly, including the checks it deliberately excludes", () => {
  requireTerms(makefile, [
    /^check: workflow-policy$/m,
    /gofmt -l cmd internal/,
    /go vet \.\/\.\.\./,
    /go tool staticcheck \.\/\.\.\./,
    /go test \.\/\.\.\./,
    /node --test scripts\/\*\.test\.mjs/,
    /scripts\/check-clean-build\.sh/,
    /npm ci/,
    /npm run format:check/,
    /npm run typecheck/,
    /npm test -- --run/,
    /npm run build/,
  ])
  requireTerms(guide, [
    /workflow policy/i,
    /`npm ci`[^.]*Prettier[^.]*TypeScript[^.]*Vitest[^.]*build frontend/is,
    /generazione degli asset[^.]*contratti Node[^.]*gofmt[^.]*fail-on-difference/is,
    /`go vet`[^.]*Staticcheck[^.]*test Go[^.]*tag predefinit[^.]*build Go[^.]*pulit/is,
    /non include[^.]*actionlint[^.]*race[^.]*Playwright[^.]*Azurite[^.]*container smoke[^.]*SBOM[^.]*vulnerabilit/is,
  ])
  assert.doesNotMatch(readme, /run every repository check/i)
  assert.match(readme, /does\s+not include[^.]*actionlint[^.]*race/is)

  const checkRecipe = makeTarget(makefile, "check")
  requireTerms(checkRecipe, [
    /workflow-policy/,
    /gofmt -l cmd internal/,
    /go vet \.\/\.\.\./,
    /go tool staticcheck \.\/\.\.\./,
    /go test \.\/\.\.\./,
    /node --test scripts\/\*\.test\.mjs/,
    /scripts\/check-clean-build\.sh/,
    /npm ci/,
    /npm run format:check/,
    /npm run typecheck/,
    /npm test -- --run/,
    /npm run build/,
  ])
  const frontendBuild = checkRecipe.indexOf("\tnpm run build")
  for (const goConsumer of [
    "\tgo vet ./...",
    "\tgo tool staticcheck ./...",
    "\tgo test ./...",
    "\t./scripts/check-clean-build.sh",
  ]) {
    assert.ok(
      checkRecipe.indexOf(goConsumer) > frontendBuild,
      `${goConsumer.trim()} must follow the frontend build`,
    )
  }
  assertCheckRecipeExclusions(checkRecipe)
})

test("the make-check exclusion guard rejects every out-of-band gate", () => {
  const checkRecipe = makeTarget(makefile, "check")
  const forbiddenSamples = [
    ["Go race", "\tgo test -race -count=1 ./..."],
    ["npm E2E", "\tnpm run e2e -- --project=chromium"],
    ["Playwright", "\t./node_modules/.bin/playwright test"],
    [
      "integration tags",
      "\tgo test -tags=integration ./internal/storage/azure",
    ],
    ["recursive actionlint", "\t$(MAKE) actionlint"],
    [
      "Azurite Compose",
      "\tdocker compose -f compose.test.yaml up --detach azurite",
    ],
    ["container smoke", "\t$(MAKE) container-smoke"],
    ["SBOM", "\t$(MAKE) sbom IMAGE=legal-callegarin:local"],
    ["scan", "\t$(MAKE) scan IMAGE=legal-callegarin:local"],
  ]

  for (const [name, sample] of forbiddenSamples) {
    assert.throws(
      () => assertCheckRecipeExclusions(`${checkRecipe}\n${sample}`),
      undefined,
      `${name} must be detected inside make check`,
    )
  }
})

test("the local release-candidate matrix mirrors CI and has bounded cleanup", () => {
  requireTerms(ciWorkflow, [
    /make actionlint/,
    /go test -race -count=1 \.\/\.\.\./,
    /playwright install --with-deps chromium firefox webkit/,
    /npm run e2e -- --project=chromium/,
    /npm run e2e:smoke/,
    /docker compose -f compose\.test\.yaml up --detach azurite/,
    /-tags=integration\s+\\?\s*\.\/internal\/storage\/azure\s+\.\/internal\/storage\/contracttest/,
    /docker compose -f compose\.test\.yaml down --volumes --remove-orphans/,
    /make sbom IMAGE=/,
    /make scan IMAGE=.*SEVERITY=HIGH,CRITICAL/,
  ])
  assert.match(e2eRunner, /args: \["test", \.\.\.forwardedArguments\]/)
  assert.match(compose, /127\.0\.0\.1:11000:10000/)
  assert.match(containerSmoke, /git status --porcelain --untracked-files=all/)

  const ciConnectionString = ciWorkflow.match(
    /^\s*AZURITE_CONNECTION_STRING:\s*([^\n]+)$/m,
  )?.[1]
  const guideConnectionString = guide.match(
    /export AZURITE_CONNECTION_STRING='([^']+)'/,
  )?.[1]
  assert.ok(ciConnectionString)
  assert.ok(guideConnectionString)
  assert.equal(guideConnectionString, ciConnectionString.trim())

  requireTerms(guide, [
    /`npm ci`/,
    /`make check`/,
    /make actionlint/,
    /go test -race -count=1 \.\/\.\.\./,
    /\.\/node_modules\/\.bin\/playwright install[^\n]*chromium firefox webkit/,
    /npm run e2e -- --project=chromium/,
    /npm run e2e:smoke/,
    /docker compose -f compose\.test\.yaml up[^\n]*azurite/,
    /AZURITE_CONNECTION_STRING=/,
    /-tags=integration\s+\\?\s*\.\/internal\/storage\/azure\s+\.\/internal\/storage\/contracttest/,
    /trap[^.]*down --volumes --remove-orphans/is,
    /if ! timeout[\s\S]{0,900}docker compose -f compose\.test\.yaml ps[\s\S]{0,240}docker compose -f compose\.test\.yaml logs --no-color azurite[\s\S]{0,120}exit 1/,
    /`make container-smoke`[^.]*HEAD[^.]*worktree[^.]*pulit/is,
    /make sbom IMAGE=/,
    /make scan IMAGE=[^\n]*SEVERITY=HIGH,CRITICAL/,
    /az bicep install --version v0\.45\.15/,
    /az bicep version/,
    /build-params[^.]*stdout[^.]*scart/is,
  ])
})

test("Bicep compilation is warning-free and uses only ephemeral synthetic compile fixtures", () => {
  const bicepGate = markdownSection(
    guide,
    "### 5. Bicep esatto e warning-free",
    "### ",
  )
  assertWarningFreeBicepWrapper(bicepGate)

  const firstBuildParams = bicepGate.indexOf(
    "run_bicep_warning_free az bicep build-params",
  )
  assert.ok(firstBuildParams >= 0)
  for (const name of [
    "GHCR_TOKEN",
    "ADMIN_PASSWORD_HASH",
    "SESSION_KEY_BASE64",
  ]) {
    const assignment = bicepGate.indexOf(`${name}='synthetic-compile-only-`)
    assert.ok(assignment >= 0, `missing synthetic fixture for ${name}`)
    assert.ok(
      assignment < firstBuildParams,
      `${name} must exist before build-params`,
    )
  }
  requireTerms(bicepGate, [
    /sintetic[^.]*effimer[^.]*solo[^.]*compilazion/is,
    /mai[^.]*produzione/is,
    /export GHCR_TOKEN ADMIN_PASSWORD_HASH SESSION_KEY_BASE64/,
    /infra\/main\.example\.bicepparam/,
    /infra\/custom-domain\.example\.bicepparam/,
    /build-params[^\n]*--stdout >\/dev\/null/,
  ])
  const mutatedWrapper = bicepGate.replace(
    'if [ -s "$BICEP_STDERR" ]',
    'if [ ! -s "$BICEP_STDERR" ]',
  )
  assert.notEqual(mutatedWrapper, bicepGate)
  assert.throws(
    () => assertWarningFreeBicepWrapper(mutatedWrapper),
    /did not match the regular expression/,
  )
})

test("release evidence is bound to one exact clean commit", () => {
  requireTerms(guide, [
    /git status --porcelain --untracked-files=all/,
    /output[^.]*vuoto/is,
    /git rev-parse HEAD/,
    /prima[^.]*dopo[^.]*gate/is,
    /stesso[^.]*commit[^.]*CI[^.]*green/is,
    /ignorati[^.]*generati[^.]*non[^.]*dimostrano[^.]*pulizia/is,
  ])
})

test("bootstrap publishing uses an isolated write credential and canonical digest", () => {
  assert.match(mainBicep, /param imageReference string/)
  assert.match(
    mainParameters,
    /imageReference = 'ghcr\.io\/[^']+@sha256:[0-9a-f]{64}'/,
  )
  assert.match(deployWorkflow, /Build and publish immutable image/)
  assert.match(deployWorkflow, /needs: publish/)

  requireTerms(guide, [
    /cycle breaker/i,
    /PAT[^.]*classic[^.]*solo[^.]*`write:packages`/is,
    /input[^.]*nascost/is,
    /docker login[\s\S]{0,100}--password-stdin/,
    /DOCKER_CONFIG[^.]*temporane[^.]*restrittiv/is,
    /trap[^.]*logout[^.]*rimozion/is,
    /tracing[^.]*argv[^.]*log[^.]*ticket[^.]*chat[^.]*file/is,
    /linux\/amd64/,
    /commit[^.]*esatt/is,
    /org\.opencontainers\.image\.source/,
    /package[^.]*privat[^.]*collegat[^.]*repository[^.]*Actions[^.]*admin/is,
    /@sha256:<64[^>]*hex>/i,
    /risoluzione[^.]*indipendente[^.]*digest/is,
    /revoca[^.]*immediat/is,
    /non[^.]*riutilizz[^.]*PAT[^.]*runtime/is,
  ])
  assert.match(
    guide,
    /if \[\[ ! "\$REGISTRY_DIGEST" =~ \^sha256:\[0-9a-f\]\{64\}\$ \]\]/,
  )
  const bootstrap = markdownSection(
    guide,
    "## Pubblicazione una tantum della prima immagine",
  )
  assertBuildDigestComparison(bootstrap)
  const withoutEquality = bootstrap.replace(
    'if [[ "$REGISTRY_DIGEST" != "$BUILD_DIGEST" ]]',
    'if [[ -z "$REGISTRY_DIGEST" ]]',
  )
  assert.notEqual(withoutEquality, bootstrap)
  assert.throws(
    () => assertBuildDigestComparison(withoutEquality),
    /did not match the regular expression/,
  )
})

test("the three production credentials and stable session key never collapse together", () => {
  requireTerms(guide, [
    /bootstrap[^.]*PAT[^.]*classic[^.]*`write:packages`/is,
    /runtime[^.]*PAT[^.]*classic[^.]*`read:packages`[^.]*secret[^.]*ACA/is,
    /OIDC[^.]*PAT[^.]*fine-grained[^.]*repository[^.]*`Actions: read`/is,
    /unset[^.]*verifica/is,
    /workstation[^.]*fidat/is,
    /umask[^.]*restrittiv/is,
    /sottoprocesso[^.]*protett/is,
    /openssl rand -base64 32/,
    /decodificat[^.]*32 byte[^.]*senza[^.]*stamp/is,
    /environment-backed[^.]*Bicep/is,
    /genera[^.]*una sola volta/is,
    /(?:rotazione|ruotar)[^.]*invalida[^.]*tutte[^.]*session/is,
    /cmd\/adminhash[^.]*senza\s+argomenti/is,
    /Argon2id/i,
    /senza[^.]*persistenza[^.]*secret[^.]*argv[^.]*output/is,
  ])
  assert.match(guide, /ADMIN_PASSWORD_HASH="\$\(go run \.\/cmd\/adminhash\)"/)
  assert.match(guide, /read -r -s[^\n]*GHCR_TOKEN/)

  const credentials = markdownSection(guide, "## Tre credenziali distinte")
  assertCredentialIsolation(credentials)
  requireTerms(guide, [/BOOTSTRAP_GHCR_PAT/, /GHCR_TOKEN/, /GITHUB_API_TOKEN/])
  const credentialNames = [
    "BOOTSTRAP_GHCR_PAT",
    "GHCR_TOKEN",
    "GITHUB_API_TOKEN",
  ]
  assert.equal(new Set(credentialNames).size, 3)
  const overlapping = credentials.replace(
    "`write:packages`",
    "`write:packages` e `read:packages`",
  )
  assert.notEqual(overlapping, credentials)
  assert.throws(
    () => assertCredentialIsolation(overlapping),
    /expected to not match the regular expression/,
  )
})

test("the first-production sequence is strict and migrate is mandatory before ordinary deploy", () => {
  assert.match(
    containerAppBicep,
    /name:\s*'ARTICLE_STORAGE_SCHEMA_MODE'[\s\S]{0,80}value:\s*'compat'/,
  )
  assert.match(deployScript, /schema mode must be one literal migrate value/)

  assertOrdered(guide, [
    /commit revisionato[^.]*gate locali[^.]*CI/is,
    /environment GitHub `production`[^.]*protett/is,
    /resource group[^.]*OIDC[^.]*precondizion/is,
    /pubblic[^.]*prima immagine[^.]*digest[^.]*immutabil/is,
    /PAT runtime[^.]*Argon2id[^.]*chiave di sessione/is,
    /`main\.bicep`[^.]*`customDomainApex\s*=\s*''`[^.]*`compat`/is,
    /traffico[^.]*`latest=0`[^.]*100%/is,
    /rollout[^.]*`migrate`[^.]*obbligatori[^.]*marker[^.]*current-only/is,
    /propagazione DNS[^.]*certificat[^.]*binding/is,
    /ripet[^.]*normalizzazione[^.]*traffico nominato/is,
    /sei variabili[^.]*OIDC[^.]*workflow[^.]*ordinari/is,
    /retention[^.]*sol(?:o|tanto) dopo[^.]*traffico[^.]*permessi package/is,
  ])
  assert.match(guide, /migrate[^.]*non[^.]*opzional/is)
  assert.match(guide, /workflow[^.]*ordinari[^.]*non[^.]*prima[^.]*migrate/is)
})

test("affected runbooks defer to the ordered guide without stale rollout claims", () => {
  assert.doesNotMatch(
    operations,
    /successiv[oa][^.]*rollout[^.]*deploy engine[^.]*crea[^.]*`migrate`/is,
  )
  assert.match(
    operations,
    /migrazione iniziale[^.]*obbligatori[^.]*prima[^.]*deploy[^.]*ordinari/is,
  )
  assert.doesNotMatch(articleRollout, /Tasks 11[–-]13 must wire/i)
  assert.match(
    articleRollout,
    /development and first-release guide[\s\S]{0,180}mandatory order/is,
  )
})

test("all external mutations and production gates remain explicitly blocked", () => {
  requireTerms(guide, [
    /operator-only/i,
    /non[^.]*eseguit/is,
    /nessuna[^.]*chiamata[^.]*live/is,
    /produzione[^.]*BLOCKED/is,
    /launch-readiness\.md/,
    /storage-recovery\.md/,
  ])
})
