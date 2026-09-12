@description('Azure region for the Log Analytics workspace and scheduled query rule.')
param location string

@minLength(4)
@maxLength(63)
@description('Deterministic Log Analytics workspace name.')
param logAnalyticsWorkspaceName string

@description('Deterministic Azure Monitor action group name.')
param actionGroupName string

@minLength(1)
@maxLength(12)
@description('Deterministic short name used by the Azure Monitor action group.')
param actionGroupShortName string

@description('Email receiver for operational Azure Monitor alerts.')
param alertEmailAddress string

@description('Resource ID of the Storage account monitored for used capacity.')
param storageAccountId string

@description('Deterministic Storage used-capacity alert name.')
param storageUsedCapacityAlertName string

@description('Deterministic rolling log-usage alert name.')
param logUsageAlertName string

@minValue(30)
@maxValue(730)
@description('Log Analytics data retention in days.')
param logRetentionDays int

@minValue(1)
@maxValue(1024)
@description('Rolling 24-hour billable log-usage threshold in MB.')
param logUsageAlertThresholdMb int

@minValue(1)
@description('Storage used-capacity threshold in bytes.')
param storageUsedCapacityAlertThresholdBytes int

@description('Resource tags inherited from the resource-group entry point.')
param tags object

resource logAnalyticsWorkspace 'Microsoft.OperationalInsights/workspaces@2025-07-01' = {
  name: logAnalyticsWorkspaceName
  location: location
  tags: tags
  properties: {
    features: {
      disableLocalAuth: false
      enableLogAccessUsingOnlyResourcePermissions: true
    }
    publicNetworkAccessForIngestion: 'Enabled'
    publicNetworkAccessForQuery: 'Enabled'
    retentionInDays: logRetentionDays
    sku: {
      name: 'PerGB2018'
    }
    // This fixed daily cap is an emergency cost guard, not precise spend enforcement.
    workspaceCapping: {
      dailyQuotaGb: 1
    }
  }
}

resource actionGroup 'Microsoft.Insights/actionGroups@2023-01-01' = {
  name: actionGroupName
  location: 'global'
  tags: tags
  properties: {
    emailReceivers: [
      {
        emailAddress: alertEmailAddress
        name: 'operational-alerts'
        useCommonAlertSchema: true
      }
    ]
    enabled: true
    groupShortName: actionGroupShortName
  }
}

resource storageUsedCapacityAlert 'Microsoft.Insights/metricAlerts@2026-01-01' = {
  name: storageUsedCapacityAlertName
  location: 'global'
  tags: tags
  properties: {
    actions: [
      {
        actionGroupId: actionGroup.id
      }
    ]
    autoMitigate: true
    criteria: {
      'odata.type': 'Microsoft.Azure.Monitor.SingleResourceMultipleMetricCriteria'
      allOf: [
        {
          criterionType: 'StaticThresholdCriterion'
          dimensions: []
          metricName: 'UsedCapacity'
          metricNamespace: 'Microsoft.Storage/storageAccounts'
          name: 'UsedCapacity'
          operator: 'GreaterThan'
          skipMetricValidation: false
          threshold: storageUsedCapacityAlertThresholdBytes
          timeAggregation: 'Average'
        }
      ]
    }
    description: 'Alerts when the Storage account used capacity exceeds the configured threshold.'
    enabled: true
    evaluationFrequency: 'PT1H'
    scopes: [
      storageAccountId
    ]
    severity: 2
    targetResourceRegion: location
    targetResourceType: 'Microsoft.Storage/storageAccounts'
    windowSize: 'PT1H'
  }
}

resource logUsageAlert 'Microsoft.Insights/scheduledQueryRules@2026-03-01' = {
  name: logUsageAlertName
  location: location
  tags: tags
  kind: 'LogAlert'
  properties: {
    actions: {
      actionGroups: [
        actionGroup.id
      ]
    }
    autoMitigate: true
    checkWorkspaceAlertsStorageConfigured: false
    criteria: {
      allOf: [
        {
          criterionType: 'StaticThresholdCriterion'
          dimensions: []
          failingPeriods: {
            minFailingPeriodsToAlert: 1
            numberOfEvaluationPeriods: 1
          }
          metricMeasureColumn: 'BillableMb'
          operator: 'GreaterThan'
          query: '''
            Usage
            | where IsBillable == true
            | summarize BillableMb = sum(Quantity)
          '''
          threshold: logUsageAlertThresholdMb
          timeAggregation: 'Total'
        }
      ]
    }
    description: 'Alerts when rolling 24-hour billable Log Analytics usage exceeds the configured threshold.'
    displayName: logUsageAlertName
    enabled: true
    evaluationFrequency: 'PT1H'
    scopes: [
      logAnalyticsWorkspace.id
    ]
    severity: 2
    skipQueryValidation: false
    windowSize: 'P1D'
  }
}

output logAnalyticsWorkspaceId string = logAnalyticsWorkspace.id
output logAnalyticsWorkspaceName string = logAnalyticsWorkspace.name
output actionGroupId string = actionGroup.id
output actionGroupName string = actionGroup.name
output storageUsedCapacityAlertId string = storageUsedCapacityAlert.id
output storageUsedCapacityAlertName string = storageUsedCapacityAlert.name
output logUsageAlertId string = logUsageAlert.id
output logUsageAlertName string = logUsageAlert.name
