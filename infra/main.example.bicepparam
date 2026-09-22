using './main.bicep'

param location = 'italynorth'
param projectPrefix = 'legal'
param environment = 'prod'
param additionalTags = {}
param alertEmailAddress = 'ops@example.com'
param monthlyBudgetAmount = 0
param imageReference = 'ghcr.io/example/legal-callegarin@sha256:0000000000000000000000000000000000000000000000000000000000000000'
param ghcrUsername = 'example'
param ghcrToken = readEnvironmentVariable('GHCR_TOKEN')
param adminUsername = 'admin'
param adminPasswordHash = readEnvironmentVariable('ADMIN_PASSWORD_HASH')
param sessionKeyBase64 = readEnvironmentVariable('SESSION_KEY_BASE64')
param publicBaseUrl = 'https://example.com'
param customDomainApex = ''
