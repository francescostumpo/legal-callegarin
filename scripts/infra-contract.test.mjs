import assert from "node:assert/strict"
import { spawnSync } from "node:child_process"
import test from "node:test"

const repositoryRoot = new URL("../", import.meta.url)

function compileBicep(path) {
  const result = spawnSync(
    "az",
    ["bicep", "build", "--file", path, "--stdout"],
    {
      cwd: repositoryRoot,
      encoding: "utf8",
      env: process.env,
    },
  )

  assert.equal(
    result.status,
    0,
    `az bicep build failed for ${path}:\n${result.stderr || result.stdout}`,
  )
  assert.equal(result.stderr, "", `Bicep emitted warnings for ${path}`)
  return JSON.parse(result.stdout)
}

function resourcesOf(template) {
  return template.resources ?? []
}

function oneResource(template, type) {
  const resources = resourcesOf(template).filter(
    (resource) => resource.type === type,
  )
  assert.equal(resources.length, 1, `expected one ${type}`)
  return resources[0]
}

function outputNames(template) {
  return Object.keys(template.outputs ?? {}).sort()
}

test("the resource-group entry point has bounded deterministic naming and safe outputs", () => {
  const template = compileBicep("infra/main.bicep")

  assert.equal(
    template.$schema,
    "https://schema.management.azure.com/schemas/2019-04-01/deploymentTemplate.json#",
  )
  assert.deepEqual(Object.keys(template.parameters).sort(), [
    "additionalTags",
    "environment",
    "location",
    "priorVersionRetentionDays",
    "projectPrefix",
  ])
  assert.equal(template.parameters.location.defaultValue, "italynorth")
  assert.deepEqual(template.parameters.location.allowedValues, ["italynorth"])
  assert.equal(template.parameters.projectPrefix.minLength, 2)
  assert.equal(template.parameters.projectPrefix.maxLength, 6)
  assert.deepEqual(template.parameters.environment.allowedValues, [
    "dev",
    "test",
    "stage",
    "prod",
  ])
  assert.deepEqual(template.parameters.additionalTags.defaultValue, {})
  assert.equal(template.parameters.priorVersionRetentionDays.defaultValue, 30)
  assert.equal(template.parameters.priorVersionRetentionDays.minValue, 1)
  assert.equal(template.parameters.priorVersionRetentionDays.maxValue, 365)

  const deployment = oneResource(template, "Microsoft.Resources/deployments")
  assert.equal(deployment.properties.mode, "Incremental")
  assert.equal(
    deployment.properties.parameters.storageAccountName.value,
    "[variables('storageAccountName')]",
  )
  assert.equal(
    template.variables.allowedProjectPrefixCharacters,
    "abcdefghijklmnopqrstuvwxyz0123456789",
  )
  assert.match(
    template.variables.invalidProjectPrefixCharacters,
    /filter\(map\(range\(0, length\(parameters\('projectPrefix'\)\)\).*substring\(parameters\('projectPrefix'\).*not\(contains\(variables\('allowedProjectPrefixCharacters'\)/,
  )
  assert.match(
    template.variables.validatedProjectPrefix,
    /if\(empty\(variables\('invalidProjectPrefixCharacters'\)\), parameters\('projectPrefix'\), fail\('projectPrefix must contain only lowercase letters and numbers\.'\)\)/,
  )
  assert.match(
    template.variables.storageAccountName,
    /variables\('validatedProjectPrefix'\).*parameters\('environment'\).*uniqueString\(resourceGroup\(\)\.id\)/,
  )
  assert.equal(
    deployment.properties.parameters.tags.value,
    "[variables('commonTags')]",
  )
  assert.match(
    template.variables.commonTags,
    /^\[union\(parameters\('additionalTags'\), createObject\('project', parameters\('projectPrefix'\), 'environment', parameters\('environment'\), 'managed-by', 'bicep'\)\)\]$/,
  )

  assert.deepEqual(outputNames(template), [
    "articleBodiesContainerName",
    "articlesTableName",
    "blobOrigin",
    "contactsTableName",
    "sessionsTableName",
    "storageAccountId",
    "storageAccountName",
  ])
  const serializedOutputs = JSON.stringify(template.outputs)
  assert.doesNotMatch(
    serializedOutputs,
    /listKeys|connectionString|sharedAccessSignature|sasToken|password|secret/i,
  )
})

test("the storage module compiles the exact protected data plane", () => {
  const template = compileBicep("infra/modules/storage.bicep")
  const resources = resourcesOf(template)

  assert.deepEqual(
    resources.map(({ type, apiVersion }) => `${type}@${apiVersion}`).sort(),
    [
      "Microsoft.Storage/storageAccounts@2026-04-01",
      "Microsoft.Storage/storageAccounts/blobServices@2026-04-01",
      "Microsoft.Storage/storageAccounts/blobServices/containers@2026-04-01",
      "Microsoft.Storage/storageAccounts/managementPolicies@2026-04-01",
      "Microsoft.Storage/storageAccounts/tableServices@2026-04-01",
      "Microsoft.Storage/storageAccounts/tableServices/tables@2026-04-01",
      "Microsoft.Storage/storageAccounts/tableServices/tables@2026-04-01",
      "Microsoft.Storage/storageAccounts/tableServices/tables@2026-04-01",
    ].sort(),
  )

  const account = oneResource(template, "Microsoft.Storage/storageAccounts")
  assert.equal(account.location, "[parameters('location')]")
  assert.equal(account.tags, "[parameters('tags')]")
  assert.deepEqual(account.sku, { name: "Standard_ZRS" })
  assert.equal(account.kind, "StorageV2")
  assert.deepEqual(account.properties, {
    accessTier: "Hot",
    allowBlobPublicAccess: false,
    allowCrossTenantReplication: false,
    allowSharedKeyAccess: false,
    defaultToOAuthAuthentication: true,
    encryption: {
      keySource: "Microsoft.Storage",
      services: {
        blob: { enabled: true, keyType: "Account" },
        table: { enabled: true, keyType: "Account" },
      },
    },
    minimumTlsVersion: "TLS1_2",
    publicNetworkAccess: "Enabled",
    supportsHttpsTrafficOnly: true,
  })

  const blobService = oneResource(
    template,
    "Microsoft.Storage/storageAccounts/blobServices",
  )
  assert.deepEqual(blobService.properties.cors, { corsRules: [] })
  assert.deepEqual(blobService.properties.deleteRetentionPolicy, {
    allowPermanentDelete: false,
    days: 30,
    enabled: true,
  })
  assert.deepEqual(blobService.properties.containerDeleteRetentionPolicy, {
    allowPermanentDelete: false,
    days: 30,
    enabled: true,
  })
  assert.equal(blobService.properties.isVersioningEnabled, true)

  const container = oneResource(
    template,
    "Microsoft.Storage/storageAccounts/blobServices/containers",
  )
  assert.equal(template.variables.articleBodiesContainerName, "article-bodies")
  assert.match(container.name, /variables\('articleBodiesContainerName'\)/)
  assert.deepEqual(container.properties, {
    defaultEncryptionScope: "$account-encryption-key",
    denyEncryptionScopeOverride: true,
    publicAccess: "None",
  })

  const tableService = oneResource(
    template,
    "Microsoft.Storage/storageAccounts/tableServices",
  )
  assert.deepEqual(tableService.properties.cors, { corsRules: [] })

  const tables = resources.filter(
    (resource) =>
      resource.type ===
      "Microsoft.Storage/storageAccounts/tableServices/tables",
  )
  assert.deepEqual(
    tables
      .map(({ name }) => name.match(/variables\('([^']+)'\)/)?.[1])
      .map((variable) => template.variables[variable])
      .sort(),
    ["articles", "contacts", "sessions"],
  )
  for (const table of tables) {
    assert.deepEqual(table.properties, { signedIdentifiers: [] })
  }

  const policy = oneResource(
    template,
    "Microsoft.Storage/storageAccounts/managementPolicies",
  )
  assert.equal(policy.properties.policy.rules.length, 1)
  const rule = policy.properties.policy.rules[0]
  assert.equal(rule.enabled, true)
  assert.equal(rule.name, "expire-prior-article-body-versions")
  assert.equal(rule.type, "Lifecycle")
  assert.deepEqual(rule.definition.filters.blobTypes, ["blockBlob"])
  assert.deepEqual(rule.definition.filters.prefixMatch, [
    "[format('{0}/articles/', variables('articleBodiesContainerName'))]",
  ])
  assert.deepEqual(Object.keys(rule.definition.actions).sort(), [
    "snapshot",
    "version",
  ])
  assert.equal(
    rule.definition.actions.snapshot.delete.daysAfterCreationGreaterThan,
    "[parameters('priorVersionRetentionDays')]",
  )
  assert.equal(
    rule.definition.actions.version.delete.daysAfterCreationGreaterThan,
    "[parameters('priorVersionRetentionDays')]",
  )
  assert.equal("baseBlob" in rule.definition.actions, false)

  assert.deepEqual(outputNames(template), [
    "articleBodiesContainerName",
    "articlesTableName",
    "blobOrigin",
    "contactsTableName",
    "sessionsTableName",
    "storageAccountId",
    "storageAccountName",
  ])
  assert.equal(
    template.outputs.blobOrigin.value,
    "[format('https://{0}.blob.{1}', parameters('storageAccountName'), environment().suffixes.storage)]",
  )
  const serialized = JSON.stringify(template)
  assert.doesNotMatch(serialized, /listKeys|connectionString|sasToken|secret/i)
  assert.doesNotMatch(serialized, /@(?:\d{4}-\d{2}-\d{2}-preview|preview)/i)
})
