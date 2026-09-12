@description('Azure region for the Container App.')
param location string

@minLength(2)
@maxLength(31)
@description('Deterministic Container App name, kept below the rollout limit.')
param containerAppName string

@description('Resource ID of the managed Container Apps environment.')
param managedEnvironmentId string

@description('Resource tags inherited from the resource-group entry point.')
param tags object

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

@description('Canonical Blob service origin used by Azure Identity clients.')
param azureStorageAccountUrl string

@description('Complete custom-domain bindings. Kept in this full PUT surface for Task 11D.')
param customDomains array = []

var ghcrPrefix = 'ghcr.io/'
var imageParts = split(imageReference, '@sha256:')
var imageRepository = first(imageParts)
var imageDigest = last(imageParts)
var repositoryPath = startsWith(imageRepository, ghcrPrefix)
  ? substring(imageRepository, length(ghcrPrefix))
  : ''
var repositoryParts = split(repositoryPath, '/')
var repositoryOwner = first(repositoryParts)
var repositoryPackage = last(repositoryParts)
var allowedRepositoryCharacters = 'abcdefghijklmnopqrstuvwxyz0123456789._-'
var invalidOwnerCharacters = filter(
  map(range(0, length(repositoryOwner)), index => substring(repositoryOwner, index, 1)),
  character => !contains(allowedRepositoryCharacters, character)
)
var invalidPackageCharacters = filter(
  map(range(0, length(repositoryPackage)), index => substring(repositoryPackage, index, 1)),
  character => !contains(allowedRepositoryCharacters, character)
)
var allowedDigestCharacters = '0123456789abcdef'
var invalidDigestCharacters = filter(
  map(range(0, length(imageDigest)), index => substring(imageDigest, index, 1)),
  character => !contains(allowedDigestCharacters, character)
)
var imageReferenceIsValid = length(imageParts) == 2 && startsWith(imageRepository, ghcrPrefix) && length(repositoryParts) == 2 && !empty(repositoryOwner) && !empty(repositoryPackage) && empty(invalidOwnerCharacters) && empty(invalidPackageCharacters) && length(imageDigest) == 64 && empty(invalidDigestCharacters) && imageReference == toLower(imageReference)
var validatedImageReference = imageReferenceIsValid
  ? imageReference
  : fail('imageReference must be an exact lowercase ghcr.io/<owner>/<package>@sha256:<64 lowercase hex> digest.')

var httpsPrefix = 'https://'
var publicAuthority = startsWith(publicBaseUrl, httpsPrefix)
  ? substring(publicBaseUrl, length(httpsPrefix))
  : ''
var publicBaseUrlIsValid = startsWith(publicBaseUrl, httpsPrefix) && publicBaseUrl == toLower(publicBaseUrl) && publicBaseUrl == trim(publicBaseUrl) && !empty(publicAuthority) && !contains(publicAuthority, '/') && !contains(publicAuthority, '@') && !contains(publicAuthority, '?') && !contains(publicAuthority, '#') && !contains(publicAuthority, ' ') && !contains(publicAuthority, '\\')
var validatedPublicBaseUrl = publicBaseUrlIsValid
  ? publicBaseUrl
  : fail('publicBaseUrl must be a canonical HTTPS origin without credentials, query, fragment, path, or trailing slash.')

resource containerApp 'Microsoft.App/containerApps@2026-01-01' = {
  name: containerAppName
  location: location
  tags: tags
  identity: {
    type: 'SystemAssigned'
  }
  properties: {
    environmentId: managedEnvironmentId
    configuration: {
      activeRevisionsMode: 'Multiple'
      registries: [
        {
          server: 'ghcr.io'
          username: ghcrUsername
          passwordSecretRef: 'ghcr-token'
        }
      ]
      secrets: [
        {
          name: 'ghcr-token'
          value: ghcrToken
        }
        {
          name: 'admin-password-hash'
          value: adminPasswordHash
        }
        {
          name: 'session-key-base64'
          value: sessionKeyBase64
        }
      ]
      ingress: {
        external: true
        allowInsecure: false
        targetPort: 8080
        transport: 'auto'
        customDomains: customDomains
        traffic: [
          {
            latestRevision: true
            weight: 100
          }
        ]
      }
    }
    template: {
      terminationGracePeriodSeconds: 30
      containers: [
        {
          name: containerAppName
          image: validatedImageReference
          env: [
            {
              name: 'APP_ENV'
              value: 'production'
            }
            {
              name: 'HTTP_ADDRESS'
              value: ':8080'
            }
            {
              name: 'PUBLIC_BASE_URL'
              value: validatedPublicBaseUrl
            }
            {
              name: 'STORAGE_MODE'
              value: 'azure'
            }
            {
              name: 'ARTICLE_STORAGE_SCHEMA_MODE'
              value: 'compat'
            }
            {
              name: 'AZURE_STORAGE_ACCOUNT_URL'
              value: azureStorageAccountUrl
            }
            {
              name: 'ADMIN_USERNAME'
              value: adminUsername
            }
            {
              name: 'ADMIN_PASSWORD_HASH'
              secretRef: 'admin-password-hash'
            }
            {
              name: 'SESSION_KEY_BASE64'
              secretRef: 'session-key-base64'
            }
            {
              name: 'TRUSTED_PROXY_HOPS'
              value: '1'
            }
          ]
          resources: {
            cpu: json('0.25')
            memory: '0.5Gi'
          }
          probes: [
            {
              type: 'Startup'
              httpGet: {
                path: '/health/live'
                port: 8080
                scheme: 'HTTP'
              }
              initialDelaySeconds: 5
              periodSeconds: 15
              timeoutSeconds: 3
              failureThreshold: 10
              successThreshold: 1
            }
            {
              type: 'Liveness'
              httpGet: {
                path: '/health/live'
                port: 8080
                scheme: 'HTTP'
              }
              initialDelaySeconds: 10
              periodSeconds: 30
              timeoutSeconds: 3
              failureThreshold: 3
              successThreshold: 1
            }
            {
              type: 'Readiness'
              httpGet: {
                path: '/health/ready'
                port: 8080
                scheme: 'HTTP'
              }
              initialDelaySeconds: 5
              periodSeconds: 10
              timeoutSeconds: 3
              failureThreshold: 3
              successThreshold: 1
            }
          ]
        }
      ]
      scale: {
        minReplicas: 0
        maxReplicas: 1
        rules: [
          {
            name: 'http-concurrency'
            http: {
              metadata: {
                concurrentRequests: '10'
              }
            }
          }
        ]
      }
    }
  }
}

output containerAppId string = containerApp.id
output containerAppName string = containerApp.name
output containerAppFqdn string = containerApp.properties.configuration.ingress.fqdn
output principalId string = containerApp.identity.principalId
