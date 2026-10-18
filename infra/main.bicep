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

@description('Email receiver for operational Azure Monitor alerts.')
param alertEmailAddress string

@minValue(30)
@maxValue(730)
@description('Log Analytics data retention in days.')
param logRetentionDays int = 30

@minValue(1)
@maxValue(1024)
@description('Rolling 24-hour billable log-usage alert threshold in MB.')
param logUsageAlertThresholdMb int = 100

@minValue(1)
@description('Storage used-capacity alert threshold in bytes.')
param storageUsedCapacityAlertThresholdBytes int = 5368709120

@minValue(0)
@description('Optional monthly budget amount in the subscription billing currency. Zero disables creation.')
param monthlyBudgetAmount int = 0

@description('First UTC day of the current month used as the Azure budget start date.')
param budgetStartDate string = '${utcNow('yyyy-MM')}-01T00:00:00Z'

@description('Immutable lowercase private GHCR image digest.')
param imageReference string

@minLength(1)
@description('Username used to pull the private GHCR image.')
param ghcrUsername string

@secure()
@description('Personal access token (classic) with only read:packages, used by Container Apps for the private GHCR pull.')
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

@description('Apex custom domain. Use an empty string only for the initial pre-DNS bootstrap deployment.')
param customDomainApex string

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
var logAnalyticsWorkspaceName = '${validatedProjectPrefix}-${environment}-logs'
var actionGroupName = '${validatedProjectPrefix}-${environment}-ops'
var actionGroupShortName = '${take(validatedProjectPrefix, 5)}${take(environment, 3)}ag'
var storageUsedCapacityAlertName = '${validatedProjectPrefix}-${environment}-storage-capacity'
var logUsageAlertName = '${validatedProjectPrefix}-${environment}-log-usage'
var budgetName = '${validatedProjectPrefix}-${environment}-monthly-budget'
var managedEnvironmentName = '${validatedProjectPrefix}-${environment}-env'
var containerAppName = '${validatedProjectPrefix}-${environment}-app'
var apexCertificateName = '${validatedProjectPrefix}-${environment}-apex-cert'
var wwwCertificateName = '${validatedProjectPrefix}-${environment}-www-cert'
var allowedDomainCharacters = 'abcdefghijklmnopqrstuvwxyz0123456789.-'
var domainAlphaNumericCharacters = 'abcdefghijklmnopqrstuvwxyz0123456789'
var malformedDomainBoundaryPairs = [
  '..'
  '.-'
  '-.'
]
var customDomainLabels = split(customDomainApex, '.')
var customDomainLabelsAreValid = empty(filter(
  customDomainLabels,
  label => empty(label) || length(label) > 63
))
var invalidCustomDomainCharacters = filter(
  map(range(0, length(customDomainApex)), index => substring(customDomainApex, index, 1)),
  character => !contains(allowedDomainCharacters, character)
)
var customDomainBoundaryIsValid = empty(customDomainApex) ? false : contains(domainAlphaNumericCharacters, substring(customDomainApex, 0, 1)) && contains(domainAlphaNumericCharacters, substring(customDomainApex, max(0, length(customDomainApex) - 1), 1))
var customDomainSeparatorsAreValid = empty(filter(
  malformedDomainBoundaryPairs,
  pair => contains(customDomainApex, pair)
))
var customDomainApexIsValid = !empty(customDomainApex) && length(customDomainApex) <= 253 && customDomainApex == trim(customDomainApex) && customDomainApex == toLower(customDomainApex) && empty(invalidCustomDomainCharacters) && contains(customDomainApex, '.') && customDomainBoundaryIsValid && customDomainSeparatorsAreValid && customDomainLabelsAreValid && !startsWith(customDomainApex, 'www.')
var publicBaseUrlMatchesCustomDomain = publicBaseUrl == 'https://${customDomainApex}' || publicBaseUrl == 'https://www.${customDomainApex}'
var customDomainConfigurationIsValid = empty(customDomainApex) || (customDomainApexIsValid && publicBaseUrlMatchesCustomDomain)
var validatedCustomDomainApex = customDomainConfigurationIsValid
  ? customDomainApex
  : fail('customDomainApex must be empty for bootstrap or a valid apex whose canonical publicBaseUrl is the apex or www HTTPS origin.')
var wwwDomain = 'www.${validatedCustomDomainApex}'
var apexCertificateId = resourceId(
  'Microsoft.App/managedEnvironments/managedCertificates',
  managedEnvironmentName,
  apexCertificateName
)
var wwwCertificateId = resourceId(
  'Microsoft.App/managedEnvironments/managedCertificates',
  managedEnvironmentName,
  wwwCertificateName
)
var customDomains = empty(validatedCustomDomainApex)
  ? []
  : [
      {
        name: validatedCustomDomainApex
        bindingType: 'SniEnabled'
        certificateId: apexCertificateId
      }
      {
        name: wwwDomain
        bindingType: 'SniEnabled'
        certificateId: wwwCertificateId
      }
    ]

module storage './modules/storage.bicep' = {
  name: 'storage'
  params: {
    location: location
    storageAccountName: storageAccountName
    tags: commonTags
    priorVersionRetentionDays: priorVersionRetentionDays
  }
}

module observability './modules/observability.bicep' = {
  name: 'observability'
  params: {
    location: location
    logAnalyticsWorkspaceName: logAnalyticsWorkspaceName
    actionGroupName: actionGroupName
    actionGroupShortName: actionGroupShortName
    alertEmailAddress: alertEmailAddress
    storageAccountId: storage.outputs.storageAccountId
    storageUsedCapacityAlertName: storageUsedCapacityAlertName
    logUsageAlertName: logUsageAlertName
    logRetentionDays: logRetentionDays
    logUsageAlertThresholdMb: logUsageAlertThresholdMb
    storageUsedCapacityAlertThresholdBytes: storageUsedCapacityAlertThresholdBytes
    tags: commonTags
  }
}

module cost './modules/cost.bicep' = {
  name: 'cost'
  params: {
    monthlyBudgetAmount: monthlyBudgetAmount
    budgetName: budgetName
    budgetStartDate: budgetStartDate
    actionGroupId: observability.outputs.actionGroupId
  }
}

module platform './modules/platform.bicep' = {
  name: 'platform'
  params: {
    location: location
    managedEnvironmentName: managedEnvironmentName
    logAnalyticsWorkspaceName: observability.outputs.logAnalyticsWorkspaceName
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
    customDomains: customDomains
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
output logAnalyticsWorkspaceId string = observability.outputs.logAnalyticsWorkspaceId
output logAnalyticsWorkspaceName string = observability.outputs.logAnalyticsWorkspaceName
output monitorActionGroupId string = observability.outputs.actionGroupId
output monitorActionGroupName string = observability.outputs.actionGroupName
output storageUsedCapacityAlertId string = observability.outputs.storageUsedCapacityAlertId
output storageUsedCapacityAlertName string = observability.outputs.storageUsedCapacityAlertName
output logUsageAlertId string = observability.outputs.logUsageAlertId
output logUsageAlertName string = observability.outputs.logUsageAlertName
output monthlyBudgetEnabled bool = cost.outputs.budgetEnabled
output managedEnvironmentId string = platform.outputs.managedEnvironmentId
output managedEnvironmentName string = platform.outputs.managedEnvironmentName
output managedEnvironmentDefaultDomain string = platform.outputs.defaultDomain
output managedEnvironmentStaticIp string = platform.outputs.staticIp
output managedEnvironmentCustomDomainVerificationId string = platform.outputs.customDomainVerificationId
output containerAppId string = containerApp.outputs.containerAppId
output containerAppName string = containerApp.outputs.containerAppName
output containerAppFqdn string = containerApp.outputs.containerAppFqdn
output containerAppPrincipalId string = containerApp.outputs.principalId
output dnsApexAHost string = '@'
output dnsApexAValue string = platform.outputs.staticIp
output dnsApexTxtHost string = 'asuid'
output dnsApexTxtValue string = platform.outputs.customDomainVerificationId
output dnsWwwCnameHost string = 'www'
output dnsWwwCnameValue string = containerApp.outputs.containerAppFqdn
output dnsWwwTxtHost string = 'asuid.www'
output dnsWwwTxtValue string = platform.outputs.customDomainVerificationId
