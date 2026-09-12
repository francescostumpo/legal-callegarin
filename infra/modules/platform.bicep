@description('Azure region for the Container Apps managed environment.')
param location string

@minLength(2)
@maxLength(60)
@description('Deterministic Container Apps managed environment name.')
param managedEnvironmentName string

@minLength(4)
@maxLength(63)
@description('Name of the existing Log Analytics workspace used for application logs.')
param logAnalyticsWorkspaceName string

@description('Resource tags inherited from the resource-group entry point.')
param tags object

resource logAnalyticsWorkspace 'Microsoft.OperationalInsights/workspaces@2025-07-01' existing = {
  name: logAnalyticsWorkspaceName
}

resource managedEnvironment 'Microsoft.App/managedEnvironments@2026-01-01' = {
  name: managedEnvironmentName
  location: location
  tags: tags
  properties: {
    appLogsConfiguration: {
      destination: 'log-analytics'
      logAnalyticsConfiguration: {
        customerId: logAnalyticsWorkspace.properties.customerId
        sharedKey: logAnalyticsWorkspace.listKeys().primarySharedKey
      }
    }
  }
}

output managedEnvironmentId string = managedEnvironment.id
output managedEnvironmentName string = managedEnvironment.name
output defaultDomain string = managedEnvironment.properties.defaultDomain
output staticIp string = managedEnvironment.properties.staticIp
output customDomainVerificationId string = managedEnvironment.properties.customDomainConfiguration.customDomainVerificationId
