targetScope = 'resourceGroup'

@allowed([
  'italynorth'
])
@description('Azure region for all resources. Task 11A supports Italy North only.')
param location string = 'italynorth'

@minLength(2)
@maxLength(6)
@description('Short project prefix containing only lowercase letters and numbers.')
param projectPrefix string

@allowed([
  'dev'
  'test'
  'stage'
  'prod'
])
@description('Short lowercase deployment environment name.')
param environment string

@description('Optional extra tags. Reserved project, environment, and managed-by tags always win.')
param additionalTags object = {}

@minValue(1)
@maxValue(365)
@description('Days to retain prior Blob versions and snapshots. Current blobs are never age-deleted.')
param priorVersionRetentionDays int = 30

@description('Immutable lowercase private GHCR image digest.')
param imageReference string

@minLength(1)
@description('Username used to pull the private GHCR image.')
param ghcrUsername string

@secure()
@description('Fine-grained token used only by Container Apps to pull from GHCR.')
param ghcrToken string

@minLength(1)
@description('Single internal administrator username.')
param adminUsername string

@secure()
@description('Argon2id password hash for the single internal administrator.')
param adminPasswordHash string

@secure()
@description('Base64-encoded session authentication key.')
param sessionKeyBase64 string

@description('Canonical public HTTPS origin without a trailing slash.')
param publicBaseUrl string

var commonTags = union(additionalTags, {
  project: projectPrefix
  environment: environment
  'managed-by': 'bicep'
})
var allowedProjectPrefixCharacters = 'abcdefghijklmnopqrstuvwxyz0123456789'
var invalidProjectPrefixCharacters = filter(
  map(range(0, length(projectPrefix)), index => substring(projectPrefix, index, 1)),
  character => !contains(allowedProjectPrefixCharacters, character)
)
var validatedProjectPrefix = empty(invalidProjectPrefixCharacters)
  ? projectPrefix
  : fail('projectPrefix must contain only lowercase letters and numbers.')
var storageAccountName = '${validatedProjectPrefix}${environment}${uniqueString(resourceGroup().id)}'
var managedEnvironmentName = '${validatedProjectPrefix}-${environment}-env'
var containerAppName = '${validatedProjectPrefix}-${environment}-app'

module storage './modules/storage.bicep' = {
  name: 'storage'
  params: {
    location: location
    storageAccountName: storageAccountName
    tags: commonTags
    priorVersionRetentionDays: priorVersionRetentionDays
  }
}

module platform './modules/platform.bicep' = {
  name: 'platform'
  params: {
    location: location
    managedEnvironmentName: managedEnvironmentName
    tags: commonTags
  }
}

module containerApp './modules/container-app.bicep' = {
  name: 'container-app'
  params: {
    location: location
    containerAppName: containerAppName
    managedEnvironmentId: platform.outputs.managedEnvironmentId
    tags: commonTags
    imageReference: imageReference
    ghcrUsername: ghcrUsername
    ghcrToken: ghcrToken
    adminUsername: adminUsername
    adminPasswordHash: adminPasswordHash
    sessionKeyBase64: sessionKeyBase64
    publicBaseUrl: publicBaseUrl
    azureStorageAccountUrl: storage.outputs.blobOrigin
    customDomains: []
  }
}

module storageIdentity './modules/identity.bicep' = {
  name: 'storage-identity'
  params: {
    storageAccountName: storage.outputs.storageAccountName
    principalId: containerApp.outputs.principalId
  }
}

output storageAccountId string = storage.outputs.storageAccountId
output storageAccountName string = storage.outputs.storageAccountName
output blobOrigin string = storage.outputs.blobOrigin
output articlesTableName string = storage.outputs.articlesTableName
output contactsTableName string = storage.outputs.contactsTableName
output sessionsTableName string = storage.outputs.sessionsTableName
output articleBodiesContainerName string = storage.outputs.articleBodiesContainerName
output managedEnvironmentId string = platform.outputs.managedEnvironmentId
output managedEnvironmentName string = platform.outputs.managedEnvironmentName
output managedEnvironmentDefaultDomain string = platform.outputs.defaultDomain
output managedEnvironmentStaticIp string = platform.outputs.staticIp
output managedEnvironmentCustomDomainVerificationId string = platform.outputs.customDomainVerificationId
output containerAppId string = containerApp.outputs.containerAppId
output containerAppName string = containerApp.outputs.containerAppName
output containerAppFqdn string = containerApp.outputs.containerAppFqdn
output containerAppPrincipalId string = containerApp.outputs.principalId
