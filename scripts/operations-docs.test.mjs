import assert from "node:assert/strict"
import { readFile } from "node:fs/promises"
import test from "node:test"

const repositoryRoot = new URL("../", import.meta.url)

async function source(path) {
  return readFile(new URL(path, repositoryRoot), "utf8")
}

const [
  operations,
  passwordRecovery,
  articleRollout,
  mainBicep,
  customDomainBicep,
  containerAppBicep,
  deployWorkflow,
  retentionWorkflow,
  oidcBootstrap,
  sessionService,
] = await Promise.all([
  source("docs/operations.md"),
  source("docs/password-recovery.md"),
  source("docs/article-storage-rollout.md"),
  source("infra/main.bicep"),
  source("infra/custom-domain.bicep"),
  source("infra/modules/container-app.bicep"),
  source(".github/workflows/deploy.yml"),
  source(".github/workflows/retention.yml"),
  source("scripts/bootstrap-github-oidc.sh"),
  source("internal/auth/service.go"),
])

const expectedVariables = [
  "AZURE_CLIENT_ID",
  "AZURE_TENANT_ID",
  "AZURE_SUBSCRIPTION_ID",
  "AZURE_RESOURCE_GROUP",
  "AZURE_CONTAINER_APP_NAME",
  "PUBLIC_BASE_URL",
]

function uniqueMatches(value, pattern) {
  return [
    ...new Set([...value.matchAll(pattern)].map((match) => match[1])),
  ].sort()
}

function shellBlocks(value) {
  return [...value.matchAll(/```sh\n([\s\S]*?)```/g)].map((match) => match[1])
}

test("production environment guidance matches workflow variables and immutable OIDC", () => {
  const workflowVariables = uniqueMatches(
    `${deployWorkflow}\n${retentionWorkflow}`,
    /vars\.([A-Z][A-Z0-9_]*)/g,
  )
  assert.deepEqual(workflowVariables, [...expectedVariables].sort())
  for (const variable of expectedVariables) {
    assert.match(operations, new RegExp(`\\b${variable}\\b`))
  }
  assert.match(operations, /sei variabili[^.]*non segrete/is)
  assert.match(operations, /PUBLIC_BASE_URL[^.]*solo[^.]*deploy/is)
  assert.match(operations, /environment `production`/i)
  assert.match(operations, /selected[^.]*branch[^.]*`main`/is)
  assert.match(operations, /branch protection[^.]*status check/is)
  assert.match(operations, /inesistente[^.]*auto-cre[^.]*senza protezion/is)
  assert.match(operations, /required reviewer/is)
  assert.match(operations, /singolo operatore[^.]*self-review[^.]*deadlock/is)
  assert.match(operations, /retention[^.]*attendere[^.]*approvazione/is)
  assert.match(operations, /production-deploy/)

  const subject =
    "repo:francescostumpo@55147498/legal-callegarin@1365534753:environment:production"
  assert.match(oidcBootstrap, new RegExp(subject))
  assert.match(operations, new RegExp(subject))
  assert.match(operations, /api:\/\/AzureADTokenExchange/)
  assert.match(operations, /358470bc-b998-42bd-ab17-a7e34c199c0f/)
  assert.match(operations, /15 luglio 2026/i)
  assert.match(operations, /verifica[^.]*non configura[^.]*OIDC/is)
  assert.match(operations, /bootstrap-github-oidc\.sh[\s\S]*--dry-run/)
  assert.ok(
    operations.match(/bootstrap-github-oidc\.sh/g)?.length >= 2,
    "document dry-run and separately labelled mutating invocation",
  )
  assert.match(operations, /AZURE_CLIENT_ID[^.]*client_id/is)
  assert.match(operations, /nessun[^.]*password[^.]*service principal/is)
  assert.match(operations, /resource group[^.]*Container Apps Contributor/is)
  assert.match(
    operations,
    /GITHUB_API_TOKEN[^.]*fine-grained[^.]*francescostumpo\/legal-callegarin[^.]*Actions:\s*read/is,
  )
  assert.match(operations, /metadata[^.]*implicit/is)
  assert.match(
    operations,
    /GITHUB_API_TOKEN[^.]*non[^.]*(?:environment secret|secret[^.]*environment)/is,
  )
  assert.match(
    operations,
    /GITHUB_API_TOKEN[^.]*non è un GitHub environment secret[\s\S]{0,160}GITHUB_TOKEN[\s\S]{0,160}PAT classic runtime[\s\S]{0,160}Azure client secret/i,
  )
  assert.match(
    operations,
    /set \+x[\s\S]*read -r -s -p[\s\S]*export GITHUB_API_TOKEN/,
  )
  assert.match(operations, /trap 'unset GITHUB_API_TOKEN' EXIT HUP INT TERM/)
  assert.match(operations, /\)\s*\nunset GITHUB_API_TOKEN/)
  assert.match(oidcBootstrap, /set \+x[\s\S]*set \+a/)
  assert.match(
    oidcBootstrap,
    /github_api_token=\$\{GITHUB_API_TOKEN-}[\s\S]*unset GITHUB_API_TOKEN/,
  )
  assert.match(
    oidcBootstrap,
    /printf 'Authorization: Bearer %s\\n'[\s\S]*curl[\s\S]*--header @-/,
  )
  assert.match(
    operations,
    /script[^.]*copia[^.]*unset[^.]*curl[^.]*stdin[^.]*prima[^.]*Node[^.]*Azure/is,
  )
  assert.doesNotMatch(
    operations,
    /ruolo[^.]*Owner[^.]*subscription|subscription[^.]*ruolo[^.]*Owner/is,
  )
})

test("every full Bicep deployment requires named traffic normalization", () => {
  assert.match(containerAppBicep, /latestRevision:\s*true/)
  assert.match(
    operations,
    /dopo ogni[^.]*deployment completo[^.]*(?:main\.bicep|Bicep)[^.]*custom-domain\.bicep/is,
  )
  assert.match(
    operations,
    /revision list[\s\S]*--all[\s\S]*active[\s\S]*health[\s\S]*image[\s\S]*mode/,
  )
  assert.match(
    operations,
    /latestRevision:\s*true[\s\S]{0,1200}nome[^.]*opaco/i,
  )
  assert.match(
    operations,
    /--revision-weight\s+"latest=0"\s+"\$NORMALIZED_REVISION=100"/,
  )
  assert.match(
    operations,
    /rileggere[^.]*traffico[^.]*unica[^.]*revisione[^.]*100/is,
  )
  assert.match(operations, /prima[^.]*abilitare[^.]*deploy[^.]*retention/is)
})

test("runtime pull, Actions package access, and retention are separated", () => {
  assert.match(operations, /personal access token\s+\(classic\)/i)
  assert.match(operations, /read:packages/)
  assert.match(operations, /Container App[^.]*`ghcr-token`/is)
  assert.match(operations, /GITHUB_TOKEN[^.]*automatic[oa]/is)
  assert.match(operations, /GITHUB_TOKEN[^.]*non[^.]*PAT[^.]*runtime/is)
  assert.match(operations, /package[^.]*`admin`[^.]*public preview/is)
  assert.match(operations, /domenica[^.]*03:17[^.]*UTC/is)
  assert.match(operations, /digest[^.]*instradat[oa][^.]*10[^.]*30 giorni/is)
  assert.match(operations, /nessun[^.]*tag[^.]*pin/is)
  assert.match(operations, /hard cap|limiti? rigidi?/i)
  assert.match(operations, /partial[^.]*delete|eliminazione parziale/is)
  assert.match(operations, /30 giorni[^.]*ripristin/is)
  assert.match(operations, /Operatore Pacchetti/i)
  assert.match(operations, /mensilmente[^.]*GHCR/is)
  assert.match(operations, /90%[^.]*100%/is)
  assert.match(operations, /avvis[io][^.]*non[^.]*ferma[^.]*spes/is)

  for (const bicep of [mainBicep, customDomainBicep, containerAppBicep]) {
    assert.match(
      bicep,
      /@secure\(\)\s*@description\('Personal access token \(classic\) with only read:packages, used by Container Apps for the private GHCR pull\.'\)\s*param ghcrToken string/,
    )
    assert.doesNotMatch(bicep, /Fine-grained token used only by Container Apps/)
  }
})

test("password recovery is Azure-only, revisioned, and matches session cleanup", () => {
  for (const name of [
    "ADMIN_USERNAME",
    "ADMIN_PASSWORD_HASH",
    "SESSION_KEY_BASE64",
  ]) {
    assert.match(passwordRecovery, new RegExp(`\\b${name}\\b`))
  }
  assert.match(passwordRecovery, /ADMIN_USERNAME[^.]*non\s+segret[oa]/is)
  assert.match(
    passwordRecovery,
    /ADMIN_PASSWORD_HASH[^.]*Argon2id[^.]*PHC[^.]*segret/is,
  )
  assert.match(
    passwordRecovery,
    /SESSION_KEY_BASE64[^.]*32[^.]*byte[^.]*segret/is,
  )
  assert.match(
    passwordRecovery,
    /go run \.\/cmd\/adminhash`?[^\n]*senza argomenti/i,
  )
  assert.match(passwordRecovery, /`admin-password-hash`/)
  assert.match(passwordRecovery, /Portal[e]? Azure[^.]*input[^.]*protett/is)
  assert.match(passwordRecovery, /modifica[^.]*secret[^.]*non[^.]*sufficient/is)
  assert.match(passwordRecovery, /scripts\/deploy-container-app\.sh/)
  assert.match(passwordRecovery, /PHC[^.]*non[^.]*argoment/is)
  assert.match(passwordRecovery, /digest[^.]*immutabil[^.]*instradat[oa]/is)
  assert.match(
    passwordRecovery,
    /traffic[^.]*(?:unica|una sola)[^.]*positiv[^.]*revisionName/is,
  )
  assert.match(
    passwordRecovery,
    /revisione[^.]*esatta[\s\S]{0,300}active[^.]*health[^.]*schema[^.]*image/i,
  )
  assert.match(
    passwordRecovery,
    /revision show[\s\S]*--revision "\$ROUTED_REVISION"[\s\S]*properties\.template\.containers\[0\]\.image/,
  )
  const appShowBlocks = shellBlocks(passwordRecovery).filter((block) =>
    block.includes("az containerapp show"),
  )
  assert.ok(appShowBlocks.length > 0)
  for (const block of appShowBlocks) {
    assert.doesNotMatch(block, /properties\.template\.containers/)
  }
  assert.match(passwordRecovery, /--image-digest "\$ROUTED_IMAGE"/)
  assert.match(passwordRecovery, /suffix[^.]*recovery[^.]*unic/is)
  assert.match(passwordRecovery, /sessione precedente[^.]*rifiutat/is)
  assert.match(passwordRecovery, /nuov[oa] login[^.]*logout/is)
  assert.match(passwordRecovery, /compromissione[^.]*disattiv/is)
  assert.match(
    passwordRecovery,
    /session-key[^.]*invalida[^.]*tutte le sessioni/is,
  )

  const createBody = sessionService.match(
    /func \(service \*SessionService\) Create[\s\S]*?\n}/,
  )?.[0]
  assert.ok(createBody, "SessionService.Create must exist")
  assert.ok(
    createBody.indexOf("DeleteExpired(ctx, now)") <
      createBody.indexOf("io.ReadFull"),
    "expired-session cleanup must precede randomness",
  )
  assert.match(passwordRecovery, /login\s+riuscit[oa][^.]*elimina[^.]*scadut/is)
  assert.doesNotMatch(passwordRecovery, /job[^.]*pulizia sessioni/is)
  assert.doesNotMatch(passwordRecovery, /CI[^.]*secret[^.]*workflow/is)
})

test("deploy, rollback, compatibility boundary, and external gates stay explicit", () => {
  assert.match(operations, /push[^.]*main[^.]*workflow_dispatch/is)
  assert.match(operations, /SHA[^.]*verific[^.]*digest/is)
  assert.match(operations, /smoke[^.]*100%[^.]*revision/is)
  assert.match(operations, /rollback[^.]*automatic[^.]*precedente/is)
  assert.match(operations, /bootstrap[^.]*Bicep[^.]*`compat`/is)
  assert.match(operations, /successiv[oa][^.]*`migrate`/is)
  assert.match(operations, /traffic[^.]*image[^.]*mode[^.]*health/is)
  assert.match(operations, /nome[^.]*opaco[^.]*Azure/is)
  const rollbackSection = operations.match(
    /## Deploy applicativo e rollback([\s\S]*?)## GHCR privato/,
  )?.[1]
  assert.ok(rollbackSection, "manual rollback section must exist")
  const rollbackTrafficBlocks = shellBlocks(rollbackSection).filter((block) =>
    block.includes("az containerapp ingress traffic set"),
  )
  assert.equal(rollbackTrafficBlocks.length, 1)
  assert.match(
    rollbackTrafficBlocks[0],
    /--revision-weight\s+"\$CURRENT_ROUTED_REVISION=0"\s+"\$ROLLBACK_REVISION=100"/,
  )
  assert.doesNotMatch(
    operations,
    /--revision-weight\s+'<azure-returned-verified-revision-name>=100'/,
  )
  assert.match(
    operations,
    /dopo[^.]*rollback[^.]*rileggere[^.]*app[^.]*revisione[^.]*smoke/is,
  )
  assert.match(operations, /rollback[^.]*non[^.]*annulla[^.]*scritt/is)
  assert.match(operations, /Set-Cookie/)

  assert.doesNotMatch(articleRollout, /rollback floor/i)
  assert.doesNotMatch(
    articleRollout,
    /permanently (?:pin|retain)|permanent rollback/i,
  )
  assert.match(articleRollout, /logical compatibility boundary/i)
  assert.match(articleRollout, /actually retained digest/i)
  assert.match(articleRollout, /newer retained compatible digest/i)
  assert.match(articleRollout, /never rely on a SHA tag/i)
  assert.match(articleRollout, /never use a pre-boundary image/i)
  for (const command of [
    "az containerapp update",
    "az containerapp ingress traffic set",
    'sh "$ROLLOUT_SCRIPT" assert-no-legacy',
  ]) {
    assert.match(articleRollout, new RegExp(command.replaceAll("$", "\\$")))
  }

  for (const gate of [
    "GitHub plan",
    "package `admin`",
    "Actions",
    "OIDC",
    "private image pull",
    "what-if",
    "rollback drill",
    "DNS/TLS",
    "alert delivery",
    "cost",
    "storage recovery",
    "contenuti approvati dall'avvocato",
  ]) {
    assert.match(operations, new RegExp(gate, "i"))
  }
  assert.match(operations, /non[^.]*eseguit[oa][^.]*Codex/is)
})

test("official references and examples do not disclose or pass secrets", () => {
  assert.match(operations, /https:\/\/docs\.github\.com\//)
  assert.match(operations, /https:\/\/learn\.microsoft\.com\//)
  const documentation = `${operations}\n${passwordRecovery}\n${articleRollout}`
  assert.doesNotMatch(documentation, /\bghp_[A-Za-z0-9]{20,}\b/)
  assert.doesNotMatch(documentation, /\bgithub_pat_[A-Za-z0-9_]{20,}\b/)
  assert.doesNotMatch(documentation, /\$argon2id\$/)
  assert.doesNotMatch(
    documentation,
    /(?:GHCR_TOKEN|ADMIN_PASSWORD_HASH|SESSION_KEY_BASE64)=['"][^<'"$][^'"]*['"]/,
  )
  assert.doesNotMatch(
    documentation,
    /GITHUB_API_TOKEN=['"][^<'"$][^'"]*['"]|--github-api-token\b|(?:echo|printf)[^\n]*GITHUB_API_TOKEN/i,
  )
  assert.doesNotMatch(passwordRecovery, /go run \.\/cmd\/adminhash\s+--?\S+/)
  assert.doesNotMatch(
    passwordRecovery,
    /deploy-container-app\.sh[^\n]*(?:PHC|PASSWORD_HASH|admin-password-hash)/,
  )
  assert.match(operations, /## Governance del budget Azure/)
  assert.match(operations, /## Dominio personalizzato Azure Container Apps/)
  assert.match(operations, /az deployment group what-if/)
  assert.match(operations, /custom-domain\.bicep/)
})
