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

module storage './modules/storage.bicep' = {
  name: 'storage'
  params: {
    location: location
    storageAccountName: storageAccountName
    tags: commonTags
    priorVersionRetentionDays: priorVersionRetentionDays
  }
}

output storageAccountId string = storage.outputs.storageAccountId
output storageAccountName string = storage.outputs.storageAccountName
output blobOrigin string = storage.outputs.blobOrigin
output articlesTableName string = storage.outputs.articlesTableName
output contactsTableName string = storage.outputs.contactsTableName
output sessionsTableName string = storage.outputs.sessionsTableName
output articleBodiesContainerName string = storage.outputs.articleBodiesContainerName
