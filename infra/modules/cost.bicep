@minValue(0)
@description('Optional monthly budget amount in the subscription billing currency. Zero disables the budget.')
param monthlyBudgetAmount int

@description('Deterministic resource-group budget name.')
param budgetName string

@description('First UTC day of the month from which Azure evaluates the budget.')
param budgetStartDate string

@description('Existing Azure Monitor action-group resource ID for budget notifications.')
param actionGroupId string

resource monthlyBudget 'Microsoft.Consumption/budgets@2024-08-01' = if (monthlyBudgetAmount > 0) {
  name: budgetName
  properties: {
    amount: monthlyBudgetAmount
    category: 'Cost'
    notifications: {
      Actual_GreaterThanOrEqualTo_80_Percent: {
        contactEmails: []
        contactGroups: [
          actionGroupId
        ]
        contactRoles: []
        enabled: true
        locale: 'it-it'
        operator: 'GreaterThanOrEqualTo'
        threshold: 80
        thresholdType: 'Actual'
      }
    }
    timeGrain: 'Monthly'
    timePeriod: {
      startDate: budgetStartDate
    }
  }
}

output budgetEnabled bool = monthlyBudgetAmount > 0
