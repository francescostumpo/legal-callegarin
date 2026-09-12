#!/usr/bin/env bash
set -euo pipefail

export AZURE_CORE_OUTPUT=none

readonly REPOSITORY="francescostumpo/legal-callegarin"
readonly APPROVED_OWNER_ID="55147498"
readonly APPROVED_REPOSITORY_ID="1365534753"
readonly ISSUER="https://token.actions.githubusercontent.com"
readonly AUDIENCE="api://AzureADTokenExchange"
readonly SUBJECT="repo:francescostumpo@55147498/legal-callegarin@1365534753:environment:production"
readonly ROLE_DEFINITION_ID="358470bc-b998-42bd-ab17-a7e34c199c0f"
readonly API_VERSION="2026-03-10"

usage() {
  cat <<'EOF'
Usage: bootstrap-github-oidc.sh --subscription-id <uuid> --tenant-id <uuid> --resource-group <name> --github-owner-id 55147498 --github-repository-id 1365534753 --application-display-name <name> --federated-credential-name <name> [--dry-run]
EOF
}

die() {
  printf 'error: %s\n' "$*" >&2
  exit 1
}

subscription_id=""
tenant_id=""
resource_group=""
github_owner_id=""
github_repository_id=""
application_display_name=""
federated_credential_name=""
dry_run=false

if [[ $# -eq 1 && $1 == "--help" ]]; then
  usage
  exit 0
fi

declare -A seen=()
while (($#)); do
  option=$1
  shift
  case "$option" in
    --subscription-id|--tenant-id|--resource-group|--github-owner-id|--github-repository-id|--application-display-name|--federated-credential-name)
      [[ -z ${seen[$option]+x} ]] || die "repeated argument: $option"
      seen[$option]=1
      (($#)) || die "missing value for $option"
      [[ -n $1 && $1 != --* ]] || die "empty or missing value for $option"
      value=$1
      shift
      case "$option" in
        --subscription-id) subscription_id=$value ;;
        --tenant-id) tenant_id=$value ;;
        --resource-group) resource_group=$value ;;
        --github-owner-id) github_owner_id=$value ;;
        --github-repository-id) github_repository_id=$value ;;
        --application-display-name) application_display_name=$value ;;
        --federated-credential-name) federated_credential_name=$value ;;
      esac
      ;;
    --dry-run)
      [[ -z ${seen[$option]+x} ]] || die "repeated argument: $option"
      seen[$option]=1
      dry_run=true
      ;;
    --help) die "--help cannot be combined with other arguments" ;;
    --*) die "unknown argument: $option" ;;
    *) die "positional argument is not allowed: $option" ;;
  esac
done

for required in subscription_id tenant_id resource_group github_owner_id github_repository_id application_display_name federated_credential_name; do
  [[ -n ${!required} ]] || die "missing required argument: ${required//_/-}"
done

uuid_pattern='^[0-9A-Fa-f]{8}-[0-9A-Fa-f]{4}-[1-5][0-9A-Fa-f]{3}-[89AaBb][0-9A-Fa-f]{3}-[0-9A-Fa-f]{12}$'
[[ $subscription_id =~ $uuid_pattern ]] || die "invalid subscription UUID"
[[ $tenant_id =~ $uuid_pattern ]] || die "invalid tenant UUID"
subscription_id=${subscription_id,,}
tenant_id=${tenant_id,,}
[[ $github_owner_id =~ ^[1-9][0-9]*$ ]] || die "invalid GitHub owner ID"
[[ $github_repository_id =~ ^[1-9][0-9]*$ ]] || die "invalid GitHub repository ID"
[[ $github_owner_id == "$APPROVED_OWNER_ID" ]] || die "GitHub owner ID is not approved"
[[ $github_repository_id == "$APPROVED_REPOSITORY_ID" ]] || die "GitHub repository ID is not approved"
[[ ${#resource_group} -le 90 && $resource_group =~ ^[A-Za-z0-9._()-]+$ && $resource_group != *. ]] || die "invalid resource group name"
[[ ${#application_display_name} -le 256 && $application_display_name =~ ^[A-Za-z0-9][A-Za-z0-9._()/\ -]*$ && $application_display_name != *[[:space:]] ]] || die "invalid application display name"
[[ ${#federated_credential_name} -le 120 && $federated_credential_name =~ ^[A-Za-z0-9._~-]+$ ]] || die "invalid federated credential name"

resource_group_scope="/subscriptions/$subscription_id/resourceGroups/$resource_group"

print_intent() {
  printf '%s\n' \
    "repository=$REPOSITORY" \
    "github_owner_id=$APPROVED_OWNER_ID" \
    "github_repository_id=$APPROVED_REPOSITORY_ID" \
    "application_display_name=$application_display_name" \
    "federated_credential_name=$federated_credential_name" \
    "issuer=$ISSUER" \
    "subject=$SUBJECT" \
    "audience=$AUDIENCE" \
    "role_definition_id=$ROLE_DEFINITION_ID" \
    "AZURE_TENANT_ID=$tenant_id" \
    "AZURE_SUBSCRIPTION_ID=$subscription_id" \
    "AZURE_RESOURCE_GROUP=$resource_group" \
    "resource_group_scope=$resource_group_scope"
}

if [[ $dry_run == true ]]; then
  print_intent
  printf 'changes_applied=false\n'
  exit 0
fi

command -v az >/dev/null || die "az is required"
command -v curl >/dev/null || die "curl is required"
command -v node >/dev/null || die "node is required"

github_get() {
  curl --fail --silent --show-error --location \
    --header "Accept: application/vnd.github+json" \
    --header "X-GitHub-Api-Version: $API_VERSION" \
    --user-agent "legal-callegarin-oidc-bootstrap/1.0" "$1"
}

repo_json=$(github_get "https://api.github.com/repos/$REPOSITORY")
oidc_json=$(github_get "https://api.github.com/repos/$REPOSITORY/actions/oidc/customization/sub")
REPO_JSON=$repo_json OIDC_JSON=$oidc_json node -e '
const fail = message => { console.error(`error: ${message}`); process.exit(1) }
let repo, oidc
try { repo = JSON.parse(process.env.REPO_JSON); oidc = JSON.parse(process.env.OIDC_JSON) } catch { fail("invalid GitHub JSON") }
if (repo.full_name !== "francescostumpo/legal-callegarin" || repo.owner?.id !== 55147498 || repo.id !== 1365534753) fail("GitHub repository identity mismatch")
const created=typeof repo.created_at === "string" ? Date.parse(repo.created_at) : Number.NaN
if (!Number.isFinite(created) || created < Date.parse("2026-07-15T00:00:00Z")) fail("GitHub repository creation is outside the approved window")
if (repo.archived !== false || repo.disabled !== false) fail("GitHub repository is archived or disabled")
if (oidc.use_default !== true || oidc.use_immutable_subject !== true || oidc.sub_claim_prefix !== "repo:francescostumpo@55147498/legal-callegarin@1365534753") fail("GitHub immutable OIDC customization mismatch")
'

account_tsv=$(az account show --query '[id,tenantId,state]' --output tsv)
[[ $account_tsv != *$'\n'* ]] || die "selected Azure account returned unexpected rows"
IFS=$'\t' read -r actual_subscription actual_tenant actual_state extra <<<"$account_tsv"
[[ -z ${extra:-} && ${actual_subscription,,} == "$subscription_id" && ${actual_tenant,,} == "$tenant_id" && $actual_state == "Enabled" ]] || die "selected Azure account does not match"

actual_scope=$(az group show --subscription "$subscription_id" --name "$resource_group" --query id --output tsv)
[[ ${actual_scope,,} == "${resource_group_scope,,}" ]] || die "resource group scope does not match"
resource_group_scope=$actual_scope

json_empty_array() {
  JSON_INPUT=$1 node -e 'let v; try { v=JSON.parse(process.env.JSON_INPUT) } catch { process.exit(2) }; if (!Array.isArray(v) || v.length !== 0) process.exit(1)'
}

audit_app_list() {
  JSON_INPUT=$1 APP_NAME=$application_display_name node -e '
const uuid=/^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i
const empty=v => Array.isArray(v) && v.length===0
let all; try { all=JSON.parse(process.env.JSON_INPUT) } catch { process.exit(2) }
if (!Array.isArray(all)) process.exit(2)
const exact=all.filter(v => v?.displayName===process.env.APP_NAME)
if (exact.length===0) process.exit(3)
if (exact.length!==1) process.exit(4)
const a=exact[0]
if (!uuid.test(a.id) || !uuid.test(a.appId) || a.signInAudience!=="AzureADMyOrg" || !empty(a.passwordCredentials) || !empty(a.keyCredentials) || !empty(a.requiredResourceAccess) || !empty(a.identifierUris) || !empty(a.web?.redirectUris ?? []) || !empty(a.spa?.redirectUris ?? []) || !empty(a.publicClient?.redirectUris ?? [])) process.exit(5)
process.stdout.write(`${a.id}\t${a.appId}`)
'
}

list_apps() {
  az ad app list --display-name "$application_display_name" --all --output json
}

changes_applied=false
apps_json=$(list_apps)
set +e
app_identity=$(audit_app_list "$apps_json")
app_status=$?
set -e
if [[ $app_status -eq 3 ]]; then
  az ad app create --display-name "$application_display_name" --sign-in-audience AzureADMyOrg --query '{id:id,appId:appId}' --output json >/dev/null
  changes_applied=true
  apps_json=$(list_apps)
  app_identity=$(audit_app_list "$apps_json") || die "created application failed dedicated audit or became ambiguous"
elif [[ $app_status -ne 0 ]]; then
  die "application lookup is ambiguous or incompatible"
fi
IFS=$'\t' read -r app_object_id client_id <<<"$app_identity"
app_credentials=$(az ad app credential list --id "$app_object_id" --output json)
json_empty_array "$app_credentials" || die "application credentials are forbidden"

audit_sp_list() {
  JSON_INPUT=$1 CLIENT_ID=$client_id node -e '
const uuid=/^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i
const empty=v => Array.isArray(v) && v.length===0
let all; try { all=JSON.parse(process.env.JSON_INPUT) } catch { process.exit(2) }
if (!Array.isArray(all)) process.exit(2)
const exact=all.filter(v => String(v?.appId).toLowerCase()===process.env.CLIENT_ID.toLowerCase())
if (exact.length===0) process.exit(3)
if (exact.length!==1) process.exit(4)
const s=exact[0]
if (!uuid.test(s.id) || !uuid.test(s.appId) || s.accountEnabled!==true || s.servicePrincipalType!=="Application" || !empty(s.passwordCredentials) || !empty(s.keyCredentials)) process.exit(5)
process.stdout.write(s.id)
'
}

list_sps() {
  az ad sp list --all --filter "appId eq '$client_id'" --output json
}

sps_json=$(list_sps)
set +e
sp_object_id=$(audit_sp_list "$sps_json")
sp_status=$?
set -e
if [[ $sp_status -eq 3 ]]; then
  az ad sp create --id "$client_id" --output none
  changes_applied=true
  sp_object_id=""
  for ((attempt=1; attempt<=12; attempt++)); do
    sps_json=$(list_sps)
    if sp_object_id=$(audit_sp_list "$sps_json"); then break; fi
    sp_object_id=""
    sleep 5
  done
  [[ -n $sp_object_id ]] || die "service principal did not become visible"
elif [[ $sp_status -ne 0 ]]; then
  die "service principal lookup is ambiguous or incompatible"
fi
sp_credentials=$(az ad sp credential list --id "$sp_object_id" --output json)
json_empty_array "$sp_credentials" || die "service principal credentials are forbidden"

audit_fics() {
  JSON_INPUT=$1 FIC_NAME=$federated_credential_name node -e '
let all; try { all=JSON.parse(process.env.JSON_INPUT) } catch { process.exit(2) }
if (!Array.isArray(all)) process.exit(2)
if (all.length===0) process.exit(3)
if (all.length!==1) process.exit(4)
const f=all[0]
const claims=f.claimsMatchingExpression
if (f.name!==process.env.FIC_NAME || f.issuer!=="https://token.actions.githubusercontent.com" || f.subject!=="repo:francescostumpo@55147498/legal-callegarin@1365534753:environment:production" || !Array.isArray(f.audiences) || f.audiences.length!==1 || f.audiences[0]!=="api://AzureADTokenExchange" || !(claims===null || claims===undefined || claims==="")) process.exit(5)
process.stdout.write("exact")
'
}

list_fics() {
  az ad app federated-credential list --id "$app_object_id" --output json
}

fics_json=$(list_fics)
set +e
fic_state=$(audit_fics "$fics_json")
fic_status=$?
set -e
if [[ $fic_status -eq 3 ]]; then
  fic_file=$(mktemp)
  cleanup_fic() { rm -f "${fic_file:-}"; }
  trap cleanup_fic EXIT
  trap 'cleanup_fic; exit 130' INT
  trap 'cleanup_fic; exit 143' TERM
  chmod 600 "$fic_file"
  FIC_NAME=$federated_credential_name FIC_ISSUER=$ISSUER FIC_SUBJECT=$SUBJECT FIC_AUDIENCE=$AUDIENCE node -e '
const fs=require("node:fs")
fs.writeFileSync(process.argv[1], JSON.stringify({name:process.env.FIC_NAME,issuer:process.env.FIC_ISSUER,subject:process.env.FIC_SUBJECT,audiences:[process.env.FIC_AUDIENCE]}))
' "$fic_file"
  if az ad app federated-credential create --id "$app_object_id" --parameters "@$fic_file" --output none; then :; fi
  changes_applied=true
  fic_state=""
  for ((attempt=1; attempt<=12; attempt++)); do
    fics_json=$(list_fics)
    if fic_state=$(audit_fics "$fics_json"); then break; fi
    fic_state=""
    sleep 5
  done
  [[ $fic_state == exact ]] || die "federated credential creation did not converge to exact state"
  cleanup_fic
  trap - EXIT INT TERM
elif [[ $fic_status -ne 0 ]]; then
  die "federated credential is ambiguous or incompatible"
fi

list_direct_rbac() {
  az role assignment list --subscription "$subscription_id" --assignee-object-id "$sp_object_id" --all --fill-principal-name false --fill-role-definition-name false --output json
}

list_visible_rbac() {
  az role assignment list --subscription "$subscription_id" --assignee-object-id "$sp_object_id" --scope "$resource_group_scope" --include-inherited --fill-principal-name false --fill-role-definition-name false --output json
}

audit_rbac() {
  DIRECT_JSON=$1 VISIBLE_JSON=$2 SP_ID=$sp_object_id RG_SCOPE=$resource_group_scope ROLE_ID=$ROLE_DEFINITION_ID node -e '
let direct, visible; try { direct=JSON.parse(process.env.DIRECT_JSON); visible=JSON.parse(process.env.VISIBLE_JSON) } catch { process.exit(2) }
if (!Array.isArray(direct) || !Array.isArray(visible)) process.exit(2)
const byId=new Map()
for (const a of [...direct,...visible]) {
  const key=a?.id ?? JSON.stringify(a)
  if (!byId.has(key)) byId.set(key,a)
}
const all=[...byId.values()]
if (all.length===0) process.exit(3)
if (all.length!==1) process.exit(4)
const a=all[0]
const directById=new Map()
for (const item of direct) {
  const key=item?.id ?? JSON.stringify(item)
  if (!directById.has(key)) directById.set(key,item)
}
if (directById.size!==1) process.exit(5)
const role=String(a.roleDefinitionId ?? "").split("/").filter(Boolean).at(-1)
const empty=v => v===null || v===undefined || v===""
if (String(a.principalId).toLowerCase()!==process.env.SP_ID.toLowerCase() || a.principalType!=="ServicePrincipal" || String(a.scope).toLowerCase()!==process.env.RG_SCOPE.toLowerCase() || role.toLowerCase()!==process.env.ROLE_ID.toLowerCase() || !empty(a.condition) || !empty(a.conditionVersion)) process.exit(5)
process.stdout.write("exact")
'
}

direct_json=$(list_direct_rbac)
visible_json=$(list_visible_rbac)
set +e
rbac_state=$(audit_rbac "$direct_json" "$visible_json")
rbac_status=$?
set -e
if [[ $rbac_status -eq 3 ]]; then
  if az role assignment create --subscription "$subscription_id" --role "$ROLE_DEFINITION_ID" --scope "$resource_group_scope" --assignee-object-id "$sp_object_id" --assignee-principal-type ServicePrincipal --output none; then :; fi
  changes_applied=true
  rbac_state=""
  for ((attempt=1; attempt<=12; attempt++)); do
    direct_json=$(list_direct_rbac)
    visible_json=$(list_visible_rbac)
    if rbac_state=$(audit_rbac "$direct_json" "$visible_json"); then break; fi
    rbac_state=""
    sleep 5
  done
  [[ $rbac_state == exact ]] || die "role assignment creation did not converge to exact state"
elif [[ $rbac_status -ne 0 ]]; then
  die "visible role assignment is broader, duplicate, conditional, or incompatible"
fi

# Authoritative final re-audit of every reusable object and credential surface.
app_identity=$(audit_app_list "$(list_apps)") || die "final application audit failed"
IFS=$'\t' read -r final_app_object final_client <<<"$app_identity"
[[ ${final_app_object,,} == ${app_object_id,,} && ${final_client,,} == ${client_id,,} ]] || die "application identity changed during bootstrap"
json_empty_array "$(az ad app credential list --id "$app_object_id" --output json)" || die "final application credential audit failed"
final_sp=$(audit_sp_list "$(list_sps)") || die "final service principal audit failed"
[[ ${final_sp,,} == ${sp_object_id,,} ]] || die "service principal identity changed during bootstrap"
json_empty_array "$(az ad sp credential list --id "$sp_object_id" --output json)" || die "final service principal credential audit failed"
[[ $(audit_fics "$(list_fics)") == exact ]] || die "final federated credential audit failed"
[[ $(audit_rbac "$(list_direct_rbac)" "$(list_visible_rbac)") == exact ]] || die "final role assignment audit failed"

print_intent
printf '%s\n' "AZURE_CLIENT_ID=$client_id" "changes_applied=$changes_applied"
