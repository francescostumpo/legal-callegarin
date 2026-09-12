@description('Azure region for the Container Apps managed environment.')
param location string

@minLength(2)
@maxLength(60)
@description('Deterministic Container Apps managed environment name.')
param managedEnvironmentName string

@description('Resource tags inherited from the resource-group entry point.')
param tags object

resource managedEnvironment 'Microsoft.App/managedEnvironments@2026-01-01' = {
  name: managedEnvironmentName
  location: location
  tags: tags
  properties: {}
}

output managedEnvironmentId string = managedEnvironment.id
output managedEnvironmentName string = managedEnvironment.name
output defaultDomain string = managedEnvironment.properties.defaultDomain
output staticIp string = managedEnvironment.properties.staticIp
output customDomainVerificationId string = managedEnvironment.properties.customDomainConfiguration.customDomainVerificationId
