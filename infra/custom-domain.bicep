targetScope = 'resourceGroup'

@allowed([
  'italynorth'
])
@description('Azure region for the existing Container Apps resources. Italy North only.')
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

@description('Canonical public HTTPS origin for either the apex or www host, without a trailing slash.')
param publicBaseUrl string

@description('Required apex custom domain. The www host is derived automatically.')
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
var managedEnvironmentName = '${validatedProjectPrefix}-${environment}-env'
var containerAppName = '${validatedProjectPrefix}-${environment}-app'
var apexCertificateName = '${validatedProjectPrefix}-${environment}-apex-cert'
var wwwCertificateName = '${validatedProjectPrefix}-${environment}-www-cert'
var blobOrigin = 'https://${storageAccountName}.blob.${az.environment().suffixes.storage}'

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
var customDomainConfigurationIsValid = customDomainApexIsValid && publicBaseUrlMatchesCustomDomain
var validatedCustomDomainApex = customDomainConfigurationIsValid
  ? customDomainApex
  : fail('customDomainApex must be a nonempty valid apex whose canonical publicBaseUrl is the apex or www HTTPS origin.')
var wwwDomain = 'www.${validatedCustomDomainApex}'
var validationCustomDomains = [
  {
    name: validatedCustomDomainApex
    bindingType: 'Disabled'
  }
  {
    name: wwwDomain
    bindingType: 'Disabled'
  }
]

resource managedEnvironment 'Microsoft.App/managedEnvironments@2026-01-01' existing = {
  name: managedEnvironmentName
}

module domainValidationApp './modules/container-app.bicep' = {
  name: 'domain-validation-app'
  params: {
    location: location
    containerAppName: containerAppName
    managedEnvironmentId: managedEnvironment.id
    tags: commonTags
    imageReference: imageReference
    ghcrUsername: ghcrUsername
    ghcrToken: ghcrToken
    adminUsername: adminUsername
    adminPasswordHash: adminPasswordHash
    sessionKeyBase64: sessionKeyBase64
    publicBaseUrl: publicBaseUrl
    azureStorageAccountUrl: blobOrigin
    customDomains: validationCustomDomains
  }
}

resource apexCertificate 'Microsoft.App/managedEnvironments/managedCertificates@2026-01-01' = {
  parent: managedEnvironment
  name: apexCertificateName
  location: location
  tags: commonTags
  properties: {
    subjectName: validatedCustomDomainApex
    domainControlValidation: 'HTTP'
  }
  dependsOn: [
    domainValidationApp
  ]
}

resource wwwCertificate 'Microsoft.App/managedEnvironments/managedCertificates@2026-01-01' = {
  parent: managedEnvironment
  name: wwwCertificateName
  location: location
  tags: commonTags
  properties: {
    subjectName: wwwDomain
    domainControlValidation: 'CNAME'
  }
  dependsOn: [
    domainValidationApp
  ]
}

var securedCustomDomains = [
  {
    name: validatedCustomDomainApex
    bindingType: 'SniEnabled'
    certificateId: apexCertificate.id
  }
  {
    name: wwwDomain
    bindingType: 'SniEnabled'
    certificateId: wwwCertificate.id
  }
]

module domainBoundApp './modules/container-app.bicep' = {
  name: 'domain-bound-app'
  params: {
    location: location
    containerAppName: containerAppName
    managedEnvironmentId: managedEnvironment.id
    tags: commonTags
    imageReference: imageReference
    ghcrUsername: ghcrUsername
    ghcrToken: ghcrToken
    adminUsername: adminUsername
    adminPasswordHash: adminPasswordHash
    sessionKeyBase64: sessionKeyBase64
    publicBaseUrl: publicBaseUrl
    azureStorageAccountUrl: blobOrigin
    customDomains: securedCustomDomains
  }
  dependsOn: [
    #disable-next-line no-unnecessary-dependson
    apexCertificate
    #disable-next-line no-unnecessary-dependson
    wwwCertificate
  ]
}

output customDomainApex string = validatedCustomDomainApex
output wwwDomain string = wwwDomain
output apexCertificateId string = apexCertificate.id
output apexCertificateName string = apexCertificate.name
output wwwCertificateId string = wwwCertificate.id
output wwwCertificateName string = wwwCertificate.name
output containerAppName string = domainBoundApp.outputs.containerAppName
output containerAppFqdn string = domainBoundApp.outputs.containerAppFqdn
output dnsApexAHost string = '@'
output dnsApexAValue string = managedEnvironment.properties.staticIp
output dnsApexTxtHost string = 'asuid'
output dnsApexTxtValue string = managedEnvironment.properties.customDomainConfiguration.customDomainVerificationId
output dnsWwwCnameHost string = 'www'
output dnsWwwCnameValue string = domainBoundApp.outputs.containerAppFqdn
output dnsWwwTxtHost string = 'asuid.www'
output dnsWwwTxtValue string = managedEnvironment.properties.customDomainConfiguration.customDomainVerificationId
