@allowed([
  'italynorth'
])
param location string

@minLength(3)
@maxLength(24)
@description('Globally unique lowercase alphanumeric Storage account name.')
param storageAccountName string

param tags object

@minValue(1)
@maxValue(365)
param priorVersionRetentionDays int

var articlesTableName = 'articles'
var contactsTableName = 'contacts'
var sessionsTableName = 'sessions'
var articleBodiesContainerName = 'article-bodies'

resource storageAccount 'Microsoft.Storage/storageAccounts@2026-04-01' = {
  name: storageAccountName
  location: location
  tags: tags
  sku: {
    name: 'Standard_ZRS'
  }
  kind: 'StorageV2'
  properties: {
    accessTier: 'Hot'
    allowBlobPublicAccess: false
    allowCrossTenantReplication: false
    allowSharedKeyAccess: false
    defaultToOAuthAuthentication: true
    encryption: {
      keySource: 'Microsoft.Storage'
      services: {
        blob: {
          enabled: true
          keyType: 'Account'
        }
        table: {
          enabled: true
          keyType: 'Account'
        }
      }
    }
    minimumTlsVersion: 'TLS1_2'
    publicNetworkAccess: 'Enabled'
    supportsHttpsTrafficOnly: true
  }
}

resource blobService 'Microsoft.Storage/storageAccounts/blobServices@2026-04-01' = {
  name: 'default'
  parent: storageAccount
  properties: {
    cors: {
      corsRules: []
    }
    deleteRetentionPolicy: {
      allowPermanentDelete: false
      days: 30
      enabled: true
    }
    containerDeleteRetentionPolicy: {
      allowPermanentDelete: false
      days: 30
      enabled: true
    }
    isVersioningEnabled: true
  }
}

resource articleBodies 'Microsoft.Storage/storageAccounts/blobServices/containers@2026-04-01' = {
  name: articleBodiesContainerName
  parent: blobService
  properties: {
    defaultEncryptionScope: '$account-encryption-key'
    denyEncryptionScopeOverride: true
    publicAccess: 'None'
  }
}

resource tableService 'Microsoft.Storage/storageAccounts/tableServices@2026-04-01' = {
  name: 'default'
  parent: storageAccount
  properties: {
    cors: {
      corsRules: []
    }
  }
}

resource articlesTable 'Microsoft.Storage/storageAccounts/tableServices/tables@2026-04-01' = {
  name: articlesTableName
  parent: tableService
  properties: {
    signedIdentifiers: []
  }
}

resource contactsTable 'Microsoft.Storage/storageAccounts/tableServices/tables@2026-04-01' = {
  name: contactsTableName
  parent: tableService
  properties: {
    signedIdentifiers: []
  }
}

resource sessionsTable 'Microsoft.Storage/storageAccounts/tableServices/tables@2026-04-01' = {
  name: sessionsTableName
  parent: tableService
  properties: {
    signedIdentifiers: []
  }
}

// Current blobs carry no reliable orphan marker. Inferring current-blob deletion
// from age in infrastructure would risk deleting a still-referenced article body;
// orphan collection therefore remains an explicit application/operator task.
resource lifecyclePolicy 'Microsoft.Storage/storageAccounts/managementPolicies@2026-04-01' = {
  name: 'default'
  parent: storageAccount
  properties: {
    policy: {
      rules: [
        {
          enabled: true
          name: 'expire-prior-article-body-versions'
          type: 'Lifecycle'
          definition: {
            actions: {
              snapshot: {
                delete: {
                  daysAfterCreationGreaterThan: priorVersionRetentionDays
                }
              }
              version: {
                delete: {
                  daysAfterCreationGreaterThan: priorVersionRetentionDays
                }
              }
            }
            filters: {
              blobTypes: [
                'blockBlob'
              ]
              prefixMatch: [
                '${articleBodiesContainerName}/articles/'
              ]
            }
          }
        }
      ]
    }
  }
  dependsOn: [
    blobService
  ]
}

output storageAccountId string = storageAccount.id
output storageAccountName string = storageAccount.name
output blobOrigin string = 'https://${storageAccount.name}.blob.${environment().suffixes.storage}'
output articlesTableName string = articlesTable.name
output contactsTableName string = contactsTable.name
output sessionsTableName string = sessionsTable.name
output articleBodiesContainerName string = articleBodies.name
