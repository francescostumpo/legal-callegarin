import assert from "node:assert/strict"
import { spawnSync } from "node:child_process"
import test from "node:test"

const repositoryRoot = new URL("../", import.meta.url)
const compiledTemplates = new Map()

function compileBicep(path) {
  if (compiledTemplates.has(path)) {
    return compiledTemplates.get(path)
  }
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
  const template = JSON.parse(result.stdout)
  compiledTemplates.set(path, template)
  return template
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

function deploymentByName(template, name) {
  const deployment = resourcesOf(template).find(
    (resource) =>
      resource.type === "Microsoft.Resources/deployments" &&
      resource.name === name,
  )
  assert.ok(deployment, `expected deployment ${name}`)
  return deployment
}

function outputNames(template) {
  return Object.keys(template.outputs ?? {}).sort()
}

function allResources(template) {
  return resourcesOf(template).flatMap((resource) => [
    resource,
    ...(resource.type === "Microsoft.Resources/deployments"
      ? allResources(resource.properties.template)
      : []),
  ])
}

test("the resource-group entry point has bounded deterministic naming and safe outputs", () => {
  const template = compileBicep("infra/main.bicep")

  assert.equal(
    template.$schema,
    "https://schema.management.azure.com/schemas/2019-04-01/deploymentTemplate.json#",
  )
  assert.deepEqual(
    Object.keys(template.parameters).sort(),
    [
      "adminPasswordHash",
      "adminUsername",
      "additionalTags",
      "environment",
      "ghcrToken",
      "ghcrUsername",
      "imageReference",
      "location",
      "priorVersionRetentionDays",
      "projectPrefix",
      "publicBaseUrl",
      "sessionKeyBase64",
    ].sort(),
  )
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

  for (const parameter of [
    "ghcrToken",
    "adminPasswordHash",
    "sessionKeyBase64",
  ]) {
    assert.equal(template.parameters[parameter].type, "securestring")
    assert.equal("defaultValue" in template.parameters[parameter], false)
  }

  const deployment = deploymentByName(template, "storage")
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

  assert.deepEqual(
    outputNames(template),
    [
      "containerAppFqdn",
      "containerAppId",
      "containerAppName",
      "containerAppPrincipalId",
      "articleBodiesContainerName",
      "articlesTableName",
      "blobOrigin",
      "contactsTableName",
      "managedEnvironmentCustomDomainVerificationId",
      "managedEnvironmentDefaultDomain",
      "managedEnvironmentId",
      "managedEnvironmentName",
      "managedEnvironmentStaticIp",
      "sessionsTableName",
      "storageAccountId",
      "storageAccountName",
    ].sort(),
  )
  const serializedOutputs = JSON.stringify(template.outputs)
  assert.doesNotMatch(
    serializedOutputs,
    /listKeys|connectionString|sharedAccessSignature|sasToken|password|secret/i,
  )
})

test("the platform module is a minimal stable Consumption environment", () => {
  const template = compileBicep("infra/modules/platform.bicep")

  assert.deepEqual(Object.keys(template.parameters).sort(), [
    "location",
    "managedEnvironmentName",
    "tags",
  ])
  const environment = oneResource(template, "Microsoft.App/managedEnvironments")
  assert.equal(environment.apiVersion, "2026-01-01")
  assert.equal(environment.name, "[parameters('managedEnvironmentName')]")
  assert.equal(environment.location, "[parameters('location')]")
  assert.equal(environment.tags, "[parameters('tags')]")
  assert.deepEqual(environment.properties, {})
  assert.equal(resourcesOf(template).length, 1)
  assert.deepEqual(outputNames(template), [
    "customDomainVerificationId",
    "defaultDomain",
    "managedEnvironmentId",
    "managedEnvironmentName",
    "staticIp",
  ])

  const serialized = JSON.stringify(template)
  assert.doesNotMatch(
    serialized,
    /logAnalytics|appLogsConfiguration|workloadProfiles|vnetConfiguration|dapr|zoneRedundant|privateEndpoint/i,
  )
  assert.doesNotMatch(serialized, /@(?:\d{4}-\d{2}-\d{2}-preview|preview)/i)
})

test("the Container App module has the exact private runtime contract", () => {
  const template = compileBicep("infra/modules/container-app.bicep")

  assert.deepEqual(Object.keys(template.parameters).sort(), [
    "adminPasswordHash",
    "adminUsername",
    "azureStorageAccountUrl",
    "containerAppName",
    "customDomains",
    "ghcrToken",
    "ghcrUsername",
    "imageReference",
    "location",
    "managedEnvironmentId",
    "publicBaseUrl",
    "sessionKeyBase64",
    "tags",
  ])
  for (const parameter of [
    "ghcrToken",
    "adminPasswordHash",
    "sessionKeyBase64",
  ]) {
    assert.equal(template.parameters[parameter].type, "securestring")
    assert.equal("defaultValue" in template.parameters[parameter], false)
  }
  assert.deepEqual(template.parameters.customDomains.defaultValue, [])

  const app = oneResource(template, "Microsoft.App/containerApps")
  assert.equal(resourcesOf(template).length, 1)
  assert.equal(app.apiVersion, "2026-01-01")
  assert.deepEqual(app.identity, { type: "SystemAssigned" })
  assert.equal(
    app.properties.environmentId,
    "[parameters('managedEnvironmentId')]",
  )

  const configuration = app.properties.configuration
  assert.equal(configuration.activeRevisionsMode, "Multiple")
  assert.deepEqual(configuration.registries, [
    {
      passwordSecretRef: "ghcr-token",
      server: "ghcr.io",
      username: "[parameters('ghcrUsername')]",
    },
  ])
  assert.deepEqual(configuration.secrets, [
    { name: "ghcr-token", value: "[parameters('ghcrToken')]" },
    {
      name: "admin-password-hash",
      value: "[parameters('adminPasswordHash')]",
    },
    {
      name: "session-key-base64",
      value: "[parameters('sessionKeyBase64')]",
    },
  ])
  assert.deepEqual(configuration.ingress, {
    allowInsecure: false,
    customDomains: "[parameters('customDomains')]",
    external: true,
    targetPort: 8080,
    traffic: [{ latestRevision: true, weight: 100 }],
    transport: "auto",
  })

  const appTemplate = app.properties.template
  assert.equal(appTemplate.terminationGracePeriodSeconds, 30)
  assert.deepEqual(appTemplate.scale, {
    maxReplicas: 1,
    minReplicas: 0,
    rules: [
      {
        http: { metadata: { concurrentRequests: "10" } },
        name: "http-concurrency",
      },
    ],
  })
  assert.equal(appTemplate.containers.length, 1)
  const container = appTemplate.containers[0]
  assert.equal(container.image, "[variables('validatedImageReference')]")
  assert.deepEqual(container.resources, {
    cpu: "[json('0.25')]",
    memory: "0.5Gi",
  })
  assert.deepEqual(container.env, [
    { name: "APP_ENV", value: "production" },
    { name: "HTTP_ADDRESS", value: ":8080" },
    { name: "PUBLIC_BASE_URL", value: "[variables('validatedPublicBaseUrl')]" },
    { name: "STORAGE_MODE", value: "azure" },
    { name: "ARTICLE_STORAGE_SCHEMA_MODE", value: "compat" },
    {
      name: "AZURE_STORAGE_ACCOUNT_URL",
      value: "[parameters('azureStorageAccountUrl')]",
    },
    { name: "ADMIN_USERNAME", value: "[parameters('adminUsername')]" },
    { name: "ADMIN_PASSWORD_HASH", secretRef: "admin-password-hash" },
    { name: "SESSION_KEY_BASE64", secretRef: "session-key-base64" },
    { name: "TRUSTED_PROXY_HOPS", value: "1" },
  ])
  assert.deepEqual(container.probes, [
    {
      failureThreshold: 10,
      httpGet: { path: "/health/live", port: 8080, scheme: "HTTP" },
      initialDelaySeconds: 5,
      periodSeconds: 15,
      successThreshold: 1,
      timeoutSeconds: 3,
      type: "Startup",
    },
    {
      failureThreshold: 3,
      httpGet: { path: "/health/live", port: 8080, scheme: "HTTP" },
      initialDelaySeconds: 10,
      periodSeconds: 30,
      successThreshold: 1,
      timeoutSeconds: 3,
      type: "Liveness",
    },
    {
      failureThreshold: 3,
      httpGet: { path: "/health/ready", port: 8080, scheme: "HTTP" },
      initialDelaySeconds: 5,
      periodSeconds: 10,
      successThreshold: 1,
      timeoutSeconds: 3,
      type: "Readiness",
    },
  ])

  assert.match(template.variables.validatedImageReference, /fail\(/)
  assert.match(
    template.variables.validatedImageReference,
    /parameters\('imageReference'\)/,
  )
  assert.equal(template.variables.ghcrPrefix, "ghcr.io/")
  assert.match(template.variables.imageParts, /@sha256:/)
  assert.match(template.variables.imageReferenceIsValid, /imageParts'\)\), 2/)
  assert.match(
    template.variables.imageReferenceIsValid,
    /repositoryParts'\)\), 2/,
  )
  assert.match(template.variables.imageReferenceIsValid, /imageDigest'\)\), 64/)
  assert.match(
    template.variables.imageReferenceIsValid,
    /toLower\(parameters\('imageReference'\)\)/,
  )
  assert.equal(template.variables.allowedDigestCharacters, "0123456789abcdef")
  assert.equal(
    template.variables.allowedRepositoryCharacters,
    "abcdefghijklmnopqrstuvwxyz0123456789._-",
  )
  assert.match(template.variables.validatedPublicBaseUrl, /fail\(/)
  assert.equal(template.variables.httpsPrefix, "https://")
  assert.match(template.variables.publicBaseUrlIsValid, /publicAuthority/)
  assert.match(
    template.variables.publicBaseUrlIsValid,
    /toLower\(parameters\('publicBaseUrl'\)\)/,
  )
  for (const forbidden of ["/", "@", "?", "#", " ", "\\\\"]) {
    assert.match(
      template.variables.publicBaseUrlIsValid,
      new RegExp(`contains\\(variables\\('publicAuthority'\\), '${forbidden}'`),
    )
  }
  assert.deepEqual(outputNames(template), [
    "containerAppFqdn",
    "containerAppId",
    "containerAppName",
    "principalId",
  ])

  const serialized = JSON.stringify(template)
  assert.doesNotMatch(serialized, /AZURE_ACCOUNT_URL|TRUSTED_PROXY(?!_HOPS)/)
  assert.doesNotMatch(
    serialized,
    /AZURE_STORAGE_CONNECTION_STRING|listKeys|sharedAccessSignature|sasToken/i,
  )
  assert.doesNotMatch(serialized, /UserAssigned|workloadProfileName|dapr/i)
  assert.doesNotMatch(serialized, /@(?:\d{4}-\d{2}-\d{2}-preview|preview)/i)
  for (const parameter of [
    "ghcrToken",
    "adminPasswordHash",
    "sessionKeyBase64",
  ]) {
    assert.equal(
      serialized.split(`[parameters('${parameter}')]`).length - 1,
      1,
      `${parameter} must flow only to its Container App secret`,
    )
  }
})

test("the identity module grants only the two required Storage data roles", () => {
  const template = compileBicep("infra/modules/identity.bicep")

  assert.deepEqual(Object.keys(template.parameters).sort(), [
    "principalId",
    "storageAccountName",
  ])
  const resources = resourcesOf(template)
  assert.deepEqual(
    resources.map(({ type, apiVersion }) => `${type}@${apiVersion}`).sort(),
    [
      "Microsoft.Authorization/roleAssignments@2022-04-01",
      "Microsoft.Authorization/roleAssignments@2022-04-01",
    ],
  )
  assert.equal(
    template.variables.blobDataContributorRoleId,
    "ba92f5b4-2d11-453d-a403-e96b0029c9fe",
  )
  assert.equal(
    template.variables.tableDataContributorRoleId,
    "0a9a7e1f-b9d0-4cc4-a60d-0319b160aaa3",
  )
  for (const assignment of resources) {
    assert.equal(
      assignment.properties.principalId,
      "[parameters('principalId')]",
    )
    assert.equal(assignment.properties.principalType, "ServicePrincipal")
    assert.match(
      assignment.name,
      /^\[guid\(resourceId\('Microsoft\.Storage\/storageAccounts', parameters\('storageAccountName'\)\), parameters\('principalId'\), variables\('[^']+RoleId'\)\)\]$/,
    )
    assert.match(assignment.scope, /Microsoft\.Storage\/storageAccounts/)
    assert.match(
      assignment.properties.roleDefinitionId,
      /Microsoft\.Authorization\/roleDefinitions/,
    )
  }
  const serialized = JSON.stringify(template)
  assert.doesNotMatch(
    serialized,
    /Owner|Contributor(?!RoleId)|User Access Administrator/i,
  )
  assert.doesNotMatch(serialized, /@(?:\d{4}-\d{2}-\d{2}-preview|preview)/i)
})

test("main composes platform, app, and account-scoped identity without secret outputs", () => {
  const template = compileBicep("infra/main.bicep")
  const platform = deploymentByName(template, "platform")
  const app = deploymentByName(template, "container-app")
  const identity = deploymentByName(template, "storage-identity")

  assert.equal(
    app.properties.parameters.managedEnvironmentId.value,
    "[reference(resourceId('Microsoft.Resources/deployments', 'platform'), '2025-04-01').outputs.managedEnvironmentId.value]",
  )
  assert.equal(
    app.properties.parameters.azureStorageAccountUrl.value,
    "[reference(resourceId('Microsoft.Resources/deployments', 'storage'), '2025-04-01').outputs.blobOrigin.value]",
  )
  assert.equal(
    identity.properties.parameters.principalId.value,
    "[reference(resourceId('Microsoft.Resources/deployments', 'container-app'), '2025-04-01').outputs.principalId.value]",
  )
  assert.equal(
    identity.properties.parameters.storageAccountName.value,
    "[reference(resourceId('Microsoft.Resources/deployments', 'storage'), '2025-04-01').outputs.storageAccountName.value]",
  )
  assert.deepEqual(
    resourcesOf(template)
      .filter(({ type }) => type === "Microsoft.Resources/deployments")
      .map(({ name }) => name)
      .sort(),
    ["container-app", "platform", "storage", "storage-identity"],
  )
  assert.deepEqual(
    platform.properties.template,
    compileBicep("infra/modules/platform.bicep"),
  )
  assert.deepEqual(
    app.properties.template,
    compileBicep("infra/modules/container-app.bicep"),
  )
  assert.deepEqual(
    identity.properties.template,
    compileBicep("infra/modules/identity.bicep"),
  )
  assert.deepEqual(
    deploymentByName(template, "storage").properties.template,
    compileBicep("infra/modules/storage.bicep"),
  )
  assert.deepEqual(
    allResources(template)
      .filter(({ type }) => type !== "Microsoft.Resources/deployments")
      .map(({ type, apiVersion }) => `${type}@${apiVersion}`)
      .sort(),
    [
      "Microsoft.App/containerApps@2026-01-01",
      "Microsoft.App/managedEnvironments@2026-01-01",
      "Microsoft.Authorization/roleAssignments@2022-04-01",
      "Microsoft.Authorization/roleAssignments@2022-04-01",
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
  const serialized = JSON.stringify(template)
  assert.doesNotMatch(
    serialized,
    /Microsoft\.(?:OperationalInsights|Insights|ContainerRegistry|KeyVault|Sql|DocumentDB|Cache|Cdn|Network\/privateEndpoints)/i,
  )
  assert.doesNotMatch(
    serialized,
    /listKeys|connectionString|sharedAccessSignature|sasToken|UserAssigned|workloadProfiles|workloadProfileName|vnetConfiguration|zoneRedundant/i,
  )
  assert.doesNotMatch(
    JSON.stringify(template.outputs),
    /password|secret|token|hash/i,
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
