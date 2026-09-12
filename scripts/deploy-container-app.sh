#!/usr/bin/env bash
set -euo pipefail
set -f

readonly IMAGE_REPOSITORY='ghcr.io/francescostumpo/legal-callegarin'
readonly ARM_API_VERSION='2026-01-01'
readonly ARM_ORIGIN='https://management.azure.com'
readonly APP_QUERY='{id:id,name:name,location:location,provisioningState:properties.provisioningState,latestRevisionName:properties.latestRevisionName,latestReadyRevisionName:properties.latestReadyRevisionName,activeRevisionsMode:properties.configuration.activeRevisionsMode,external:properties.configuration.ingress.external,allowInsecure:properties.configuration.ingress.allowInsecure,fqdn:properties.configuration.ingress.fqdn,traffic:properties.configuration.ingress.traffic,template:properties.template}'
readonly REVISION_QUERY='{id:id,name:name,active:properties.active,healthState:properties.healthState,provisioningState:properties.provisioningState,runningState:properties.runningState,fqdn:properties.fqdn,trafficWeight:properties.trafficWeight,template:properties.template}'
readonly REVISION_LIST_QUERY='{items:value[].{id:id,name:name,active:properties.active,healthState:properties.healthState,provisioningState:properties.provisioningState,runningState:properties.runningState,fqdn:properties.fqdn,trafficWeight:properties.trafficWeight,template:properties.template},nextLink:nextLink}'

usage() {
  cat <<'EOF'
Usage: deploy-container-app.sh --subscription-id UUID --resource-group NAME \
  --container-app-name NAME --image-digest GHCR_DIGEST \
  --revision-suffix SUFFIX --public-base-url HTTPS_ORIGIN
EOF
}

fail() {
  printf 'deployment failed: %s\n' "$*" >&2
  exit 1
}

subscription_id=''
resource_group=''
container_app_name=''
image_digest=''
revision_suffix=''
public_base_url=''

if [[ $# -eq 1 && $1 == '--help' ]]; then
  usage
  exit 0
fi

declare -A seen_arguments=()
while [[ $# -gt 0 ]]; do
  key=$1
  case "$key" in
    --subscription-id|--resource-group|--container-app-name|--image-digest|--revision-suffix|--public-base-url)
      [[ -z ${seen_arguments[$key]+x} ]] || fail "argument repeated: $key"
      seen_arguments[$key]=1
      [[ $# -ge 2 && -n $2 && $2 != --* ]] || fail "missing value for $key"
      value=$2
      shift 2
      case "$key" in
        --subscription-id) subscription_id=$value ;;
        --resource-group) resource_group=$value ;;
        --container-app-name) container_app_name=$value ;;
        --image-digest) image_digest=$value ;;
        --revision-suffix) revision_suffix=$value ;;
        --public-base-url) public_base_url=$value ;;
      esac
      ;;
    --*) fail "unknown argument: $key" ;;
    *) fail "unexpected positional argument: $key" ;;
  esac
done

for required in subscription_id resource_group container_app_name image_digest revision_suffix public_base_url; do
  [[ -n ${!required} ]] || fail "missing required argument: ${required//_/-}"
done

[[ $subscription_id =~ ^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$ ]] || fail 'subscription ID must be a canonical lowercase UUID'
[[ ${#resource_group} -le 90 && $resource_group =~ ^[A-Za-z0-9]$|^[A-Za-z0-9][A-Za-z0-9._-]*[A-Za-z0-9_]$ ]] || fail 'invalid resource group name'
[[ ${#container_app_name} -ge 2 && ${#container_app_name} -le 31 && $container_app_name =~ ^[a-z][a-z0-9-]*[a-z0-9]$ && $container_app_name != *--* ]] || fail 'invalid Container App name'
[[ $image_digest =~ ^${IMAGE_REPOSITORY}@sha256:[0-9a-f]{64}$ ]] || fail "image must be an immutable digest in $IMAGE_REPOSITORY"
[[ ${#revision_suffix} -le 64 && $revision_suffix =~ ^[a-z]$|^[a-z][a-z0-9-]*[a-z0-9]$ && $revision_suffix != *--* ]] || fail 'invalid revision suffix'
[[ $public_base_url == https://* ]] || fail 'public base URL must use HTTPS'
public_authority=${public_base_url#https://}
public_hostname=$public_authority
if [[ $public_authority == *:* ]]; then
  [[ $public_authority != *:*:* ]] || fail 'public base URL authority is malformed'
  public_hostname=${public_authority%:*}
  public_port=${public_authority##*:}
  [[ $public_port =~ ^[1-9][0-9]{0,4}$ && $public_port -le 65535 && $public_port -ne 443 ]] || fail 'public base URL has a non-canonical or invalid port'
fi
[[ -n $public_hostname && ${#public_hostname} -le 253 && $public_hostname != *..* ]] || fail 'public base URL hostname is malformed'
IFS='.' read -r -a public_labels <<<"$public_hostname"
for public_label in "${public_labels[@]}"; do
  [[ ${#public_label} -ge 1 && ${#public_label} -le 63 && ( $public_label =~ ^[a-z0-9]$ || $public_label =~ ^[a-z0-9][a-z0-9-]*[a-z0-9]$ ) ]] || fail 'public base URL must be a lowercase HTTPS hostname origin without path, query, fragment, or userinfo'
done

for tool in az curl node mktemp chmod rm; do
  command -v "$tool" >/dev/null 2>&1 || fail "required tool not found: $tool"
done

export AZURE_CORE_OUTPUT=none
rollback_required=0
prior_revision=''
container_name=''
work_dir=''

on_exit() {
  local status=$?
  trap - EXIT
  set +e
  if (( status != 0 && rollback_required == 1 )); then
    rollback
    if [[ $? -ne 0 ]]; then
      printf 'deployment state ambiguous; manual intervention required\n' >&2
    fi
  fi
  if [[ -n $work_dir ]]; then rm -rf -- "$work_dir"; fi
  exit "$status"
}
trap on_exit EXIT

work_dir=$(mktemp -d "${TMPDIR:-/tmp}/callegarin-deploy.XXXXXX") || fail 'could not create temporary directory'
chmod 700 "$work_dir" || fail 'could not protect temporary directory'

json_tool() {
  node - "$@" <<'NODE'
const fs = require('node:fs');
const assertObject = (value, what) => {
  if (!value || typeof value !== 'object' || Array.isArray(value)) throw new Error(`malformed ${what}`);
  return value;
};
const read = file => {
  let source;
  try {
    source = fs.readFileSync(file, 'utf8');
  } catch {
    throw new Error('could not read JSON response');
  }
  let value;
  try {
    value = JSON.parse(source);
  } catch {
    throw new Error('malformed JSON response');
  }
  return assertObject(value, 'JSON response');
};
const own = (object, key) => Object.prototype.hasOwnProperty.call(object, key);
const fail = message => { console.error(`deployment failed: ${message}`); process.exit(1); };
const exactKeys = (object, keys, what) => {
  const actual = Object.keys(assertObject(object, what)).sort();
  const expected = [...keys].sort();
  if (JSON.stringify(actual) !== JSON.stringify(expected)) throw new Error(`${what} has unexpected or missing fields`);
};
const digestPattern = /^ghcr\.io\/francescostumpo\/legal-callegarin@sha256:[0-9a-f]{64}$/;
const fqdnPattern = /^[a-z0-9](?:[a-z0-9.-]{0,251}[a-z0-9])?$/;
const safeName = value => typeof value === 'string' && /^[a-z0-9][a-z0-9-]{0,63}$/.test(value);
const templateFacts = (template, expectedImage, expectedSuffix, expectedContainerName) => {
  assertObject(template, 'template');
  if (!Array.isArray(template.containers) || template.containers.length !== 1) throw new Error('template must contain exactly one container');
  const container = assertObject(expectedContainerName === undefined ? template.containers[0] : template.containers.find(item => item && item.name === expectedContainerName), 'container');
  if (!safeName(container.name)) throw new Error('container name is missing or unsafe');
  if (!digestPattern.test(container.image)) throw new Error('container image is not a fixed repository digest');
  if (expectedImage !== undefined && container.image !== expectedImage) throw new Error('container image does not match requested digest');
  if (expectedSuffix !== undefined && template.revisionSuffix !== expectedSuffix) throw new Error('revision suffix does not match');
  if (!Array.isArray(container.env)) throw new Error('container env is malformed');
  const modes = container.env.filter(item => item && typeof item === 'object' && item.name === 'ARTICLE_STORAGE_SCHEMA_MODE');
  if (modes.length !== 1 || modes[0].value !== 'migrate' || own(modes[0], 'secretRef')) throw new Error('schema mode must be one literal migrate value');
  return { container, containerName: container.name };
};
const normalizedTemplate = template => {
  const value = structuredClone(template);
  if (!Array.isArray(value.containers) || value.containers.length !== 1) throw new Error('template must contain exactly one container');
  value.revisionSuffix = '__REVISION_SUFFIX__';
  value.containers[0].image = '__IMAGE__';
  return value;
};
const canonical = value => {
  if (Array.isArray(value)) return value.map(canonical);
  if (value && typeof value === 'object') return Object.fromEntries(Object.keys(value).sort().map(key => [key, canonical(value[key])]));
  return value;
};
const assertTemplatesEqual = (left, right) => {
  if (JSON.stringify(canonical(normalizedTemplate(left))) !== JSON.stringify(canonical(normalizedTemplate(right)))) throw new Error('Container App and revision templates differ beyond image and suffix');
};
const assertTrafficRuleShape = rule => {
  assertObject(rule, 'traffic rule');
  if (own(rule, 'latestRevision') || own(rule, 'label')) throw new Error('traffic must not use latestRevision or labels');
  if (typeof rule.revisionName !== 'string' || !rule.revisionName) throw new Error('traffic rule must name a revision');
  if (!Number.isInteger(rule.weight) || rule.weight < 0 || rule.weight > 100) throw new Error('traffic weight is invalid');
};
const assertPriorTraffic = (traffic, prior) => {
  if (!Array.isArray(traffic) || traffic.length !== 1) throw new Error('traffic must contain exactly one rule');
  assertTrafficRuleShape(traffic[0]);
  if (traffic[0].revisionName !== prior || traffic[0].weight !== 100) throw new Error('traffic is not pinned 100% to the prior revision');
};
const assertPromotedTraffic = (traffic, prior, candidate) => {
  if (!Array.isArray(traffic) || traffic.length < 1 || traffic.length > 2) throw new Error('promoted traffic has an unsafe rule count');
  const weights = new Map();
  for (const rule of traffic) {
    assertTrafficRuleShape(rule);
    if (weights.has(rule.revisionName)) throw new Error('promoted traffic has duplicate revisions');
    if (rule.revisionName !== prior && rule.revisionName !== candidate) throw new Error('promoted traffic has an unexpected revision');
    weights.set(rule.revisionName, rule.weight);
  }
  if (weights.get(candidate) !== 100 || (weights.has(prior) && weights.get(prior) !== 0)) throw new Error('promoted traffic weights do not match candidate=100/prior=0');
};
const appKeys = ['id','name','location','provisioningState','latestRevisionName','latestReadyRevisionName','activeRevisionsMode','external','allowInsecure','fqdn','traffic','template'];
const assertAppIdentity = (value, expectedId, expectedName) => {
  exactKeys(value, appKeys, 'app projection');
  if (typeof value.id !== 'string' || value.id.toLowerCase() !== expectedId.toLowerCase() || value.name !== expectedName) throw new Error('Container App identity mismatch');
};
const revisionKeys = ['id','name','active','healthState','provisioningState','runningState','fqdn','trafficWeight','template'];
const assertRevisionIdentity = (revision, expectedId, expectedName) => {
  const keys = Object.keys(assertObject(revision, 'revision projection'));
  const expectedKeys = own(revision, 'trafficWeight') ? revisionKeys : revisionKeys.filter(key => key !== 'trafficWeight');
  if (JSON.stringify(keys.sort()) !== JSON.stringify(expectedKeys.sort())) throw new Error('revision projection has unexpected or missing fields');
  if (typeof revision.id !== 'string' || revision.id.toLowerCase() !== expectedId.toLowerCase() || revision.name !== expectedName) throw new Error('revision identity does not match requested resource');
};
const assertRevisionStatus = (revision, weightMode) => {
  if (revision.active !== true || revision.healthState !== 'Healthy' || revision.provisioningState !== 'Provisioned' || !['Running','ScaledToZero'].includes(revision.runningState)) throw new Error('revision is not active, healthy, provisioned, or runnable');
  if (weightMode === '100' && revision.trafficWeight !== 100) throw new Error('revision traffic weight is not 100');
  if (weightMode === 'zero' && !(revision.trafficWeight === 0 || revision.trafficWeight === null || !own(revision, 'trafficWeight'))) throw new Error('revision traffic weight is not absent or zero');
  if (typeof revision.fqdn !== 'string' || !fqdnPattern.test(revision.fqdn) || revision.fqdn.includes('..')) throw new Error('revision FQDN is missing or malformed');
};
try {
  const [action, ...args] = process.argv.slice(2);
  if (action === 'account') {
    const [file, expected] = args; const value = read(file);
    exactKeys(value, ['id','state'], 'account projection');
    if (typeof value.id !== 'string' || value.id.toLowerCase() !== expected || value.state !== 'Enabled') throw new Error('Azure account is not the requested enabled subscription');
  } else if (action === 'app-preflight') {
    const [file, expectedId, expectedName] = args; const value = read(file);
    assertAppIdentity(value, expectedId, expectedName);
    if (value.provisioningState !== 'Succeeded' || value.activeRevisionsMode !== 'Multiple' || value.external !== true || value.allowInsecure !== false) throw new Error('Container App mode, provisioning, or ingress is unsafe');
    if (typeof value.location !== 'string' || !value.location || typeof value.fqdn !== 'string' || !fqdnPattern.test(value.fqdn)) throw new Error('Container App location or FQDN is malformed');
    if (!Array.isArray(value.traffic) || value.traffic.length !== 1) throw new Error('traffic must contain exactly one rule');
    assertTrafficRuleShape(value.traffic[0]);
    if (value.traffic[0].weight !== 100) throw new Error('prior traffic weight is not 100');
    templateFacts(value.template);
    process.stdout.write(`${value.traffic[0].revisionName}\n${value.template.containers[0].name}\n`);
  } else if (action === 'app-prior-traffic') {
    const [file, expectedId, expectedName, prior] = args; const value = read(file); assertAppIdentity(value, expectedId, expectedName); assertPriorTraffic(value.traffic, prior);
  } else if (action === 'app-promoted-traffic') {
    const [file, expectedId, expectedName, prior, candidate] = args; const value = read(file); assertAppIdentity(value, expectedId, expectedName); assertPromotedTraffic(value.traffic, prior, candidate);
  } else if (action === 'revision-prior') {
    const [file, expectedId, name, containerName] = args; const value = read(file);
    assertRevisionIdentity(value, expectedId, name); assertRevisionStatus(value, '100'); templateFacts(value.template, undefined, undefined, containerName);
  } else if (action === 'revision-candidate') {
    const [file, expectedId, name, image, suffix, priorFile, containerName] = args; const value = read(file);
    assertRevisionIdentity(value, expectedId, name); assertRevisionStatus(value, 'zero'); templateFacts(value.template, image, suffix, containerName);
    const prior = read(priorFile); assertTemplatesEqual(value.template, prior.template);
    process.stdout.write(`${value.fqdn}\n`);
  } else if (action === 'revision-promoted') {
    const [file, expectedId, name, weight, containerName] = args; const value = read(file);
    assertRevisionIdentity(value, expectedId, name); assertRevisionStatus(value, weight); templateFacts(value.template, undefined, undefined, containerName);
  } else if (action === 'templates-equal') {
    const [appFile, revisionFile] = args; assertTemplatesEqual(read(appFile).template, read(revisionFile).template);
  } else if (action === 'page-next') {
    const [file, expectedPath] = args; const value = read(file);
    exactKeys(value, ['items','nextLink'], 'revision list page');
    if (!Array.isArray(value.items)) throw new Error('revision list items is not an array');
    for (const revision of value.items) assertRevisionIdentity(revision, revision.id, revision.name);
    if (value.nextLink === null) process.exit(0);
    if (typeof value.nextLink !== 'string' || !value.nextLink || /[\u0000-\u001f\u007f]/.test(value.nextLink)) throw new Error('revision nextLink is malformed');
    let url;
    let normalizedPath;
    try {
      url = new URL(value.nextLink);
      normalizedPath = decodeURIComponent(url.pathname);
    } catch {
      throw new Error('revision nextLink is malformed');
    }
    if (url.protocol !== 'https:' || url.hostname !== 'management.azure.com' || url.port || url.username || url.password || url.hash || normalizedPath.toLowerCase() !== expectedPath.toLowerCase()) throw new Error('revision nextLink escapes the revisions collection');
    const versions = url.searchParams.getAll('api-version');
    if (versions.length !== 1 || versions[0] !== '2026-01-01') throw new Error('revision nextLink changes the pinned API version');
    const sorted = [...url.searchParams.entries()].sort(([ak,av],[bk,bv]) => ak.localeCompare(bk) || av.localeCompare(bv));
    const canonical = `${url.origin}${url.pathname}?${new URLSearchParams(sorted)}`;
    process.stdout.write(`${value.nextLink}\n${canonical}\n`);
  } else if (action === 'list-preflight' || action === 'list-candidate') {
    const [revisionBaseId, priorName, suffix, ...files] = args; const names = new Set(); let priorCount = 0; const candidates = [];
    for (const file of files) {
      const page = read(file);
      exactKeys(page, ['items','nextLink'], 'revision list page');
      if (!Array.isArray(page.items)) throw new Error('revision list items is not an array');
      for (const revision of page.items) {
        if (!safeName(revision.name)) throw new Error('revision list has an unsafe name');
        assertRevisionIdentity(revision, `${revisionBaseId}/${revision.name}`, revision.name);
        if (!safeName(revision.name) || names.has(revision.name)) throw new Error('revision list has an unsafe or duplicate name');
        names.add(revision.name);
        if (revision.name === priorName) priorCount++;
        const declared = revision.template && revision.template.revisionSuffix;
        const matches = declared === suffix || revision.name === suffix || revision.name.endsWith(`--${suffix}`);
        if (matches) candidates.push(revision);
      }
    }
    if (priorCount !== 1) throw new Error('prior revision does not occur exactly once in the complete list');
    if (action === 'list-preflight') {
      if (candidates.length) throw new Error('requested revision suffix or name already exists');
    } else {
      if (candidates.length > 1) throw new Error('candidate revision is ambiguous');
      if (candidates.length === 1) process.stdout.write(`${candidates[0].name}\n`);
    }
  } else if (action === 'smoke') {
    const [headersFile, bodyFile, status, kind, expected] = args;
    if (!/^\d{3}$/.test(status)) throw new Error('HTTP status is malformed');
    const headers = fs.readFileSync(headersFile, 'latin1');
    if (/^set-cookie\s*:/im.test(headers)) throw new Error('response attempted to set a cookie');
    const body = fs.readFileSync(bodyFile);
    if (kind === 'health') {
      if (status !== '200' || !body.equals(Buffer.from('ok\n'))) throw new Error('health response is not exact');
    } else if (kind === 'candidate-root') {
      const locations = [...headers.matchAll(/^location\s*:\s*([^\r\n]+)\s*$/gim)].map(match => match[1].trim());
      if (status !== '308' || locations.length !== 1 || locations[0] !== `${expected}/`) throw new Error('candidate root redirect is not exact');
    } else if (kind === 'canonical-root') {
      if (status !== '200' || !body.includes(Buffer.from('Alessandro Callegarin'))) throw new Error('canonical root response is invalid');
    } else throw new Error('unknown smoke check');
  } else throw new Error('unknown JSON helper action');
} catch (error) { fail(error.message); }
NODE
}

readonly app_path="/subscriptions/$subscription_id/resourceGroups/$resource_group/providers/Microsoft.App/containerApps/$container_app_name"
readonly revisions_path="$app_path/revisions"
readonly app_url="$ARM_ORIGIN$app_path?api-version=$ARM_API_VERSION"
readonly revisions_url="$ARM_ORIGIN$revisions_path?api-version=$ARM_API_VERSION"

read_app() {
  local output=$1
  az rest --method get --url "$app_url" --query "$APP_QUERY" --output json >"$output"
}

read_revision() {
  local revision=$1 output=$2
  [[ $revision =~ ^[a-z0-9][a-z0-9-]{0,63}$ ]] || return 1
  az rest --method get --url "$ARM_ORIGIN$revisions_path/$revision?api-version=$ARM_API_VERSION" --query "$REVISION_QUERY" --output json >"$output"
}

LIST_FILES=()
fetch_revision_list() {
  local prefix=$1 url=$revisions_url page=1 info next canonical seen=''
  LIST_FILES=()
  while :; do
    local output="$work_dir/${prefix}-${page}.json"
    az rest --method get --url "$url" --query "$REVISION_LIST_QUERY" --output json >"$output" || return 1
    LIST_FILES+=("$output")
    info=$(json_tool page-next "$output" "$revisions_path") || return 1
    [[ -n $info ]] || return 0
    next=${info%%$'\n'*}
    canonical=${info#*$'\n'}
    [[ $canonical != "$info" && -n $next && -n $canonical ]] || return 1
    if [[ $'\n'$seen$'\n' == *$'\n'$canonical$'\n'* ]]; then
      printf 'deployment failed: revision pagination cycle detected\n' >&2
      return 1
    fi
    seen+="${seen:+$'\n'}$canonical"
    (( page < 100 )) || { printf 'deployment failed: revision pagination exceeds 100 pages\n' >&2; return 1; }
    url=$next
    ((page += 1))
  done
}

http_check() {
  local url=$1 kind=$2 expected=${3:-} token=$4
  local headers="$work_dir/headers-$token" body="$work_dir/body-$token" status
  status=$(curl --disable --no-location --silent --show-error --connect-timeout 10 --max-time 30 --request GET --dump-header "$headers" --output "$body" --write-out '%{http_code}' "$url") || return 1
  json_tool smoke "$headers" "$body" "$status" "$kind" "$expected"
}

canonical_smoke() {
  local prefix=$1
  http_check "$public_base_url/health/live" health '' "$prefix-live" || return 1
  http_check "$public_base_url/health/ready" health '' "$prefix-ready" || return 1
  http_check "$public_base_url/" canonical-root '' "$prefix-root"
}

rollback() {
  az containerapp ingress traffic set --subscription "$subscription_id" --resource-group "$resource_group" --name "$container_app_name" --revision-weight "$prior_revision=100" --output none || return 1
  local app_file="$work_dir/rollback-app.json" prior_file="$work_dir/rollback-prior.json"
  local verified=0 attempt
  for ((attempt = 1; attempt <= 24; attempt++)); do
    if read_app "$app_file" && json_tool app-prior-traffic "$app_file" "$app_path" "$container_app_name" "$prior_revision" &&
       read_revision "$prior_revision" "$prior_file" && json_tool revision-promoted "$prior_file" "$revisions_path/$prior_revision" "$prior_revision" 100 "$container_name"; then
      verified=1
      break
    fi
    (( attempt < 24 )) && sleep 5
  done
  (( verified == 1 )) || return 1
  canonical_smoke rollback || return 1
}

account_file="$work_dir/account.json"
az account show --query '{id:id,state:state}' --output json >"$account_file"
json_tool account "$account_file" "$subscription_id"

expected_rg_id="/subscriptions/$subscription_id/resourceGroups/$resource_group"
actual_rg_id=$(az group show --subscription "$subscription_id" --name "$resource_group" --query id --output tsv)
[[ -n $actual_rg_id && ${actual_rg_id,,} == ${expected_rg_id,,} ]] || fail 'resource group identity mismatch'

app_file="$work_dir/app-preflight.json"
read_app "$app_file"
app_facts_text=$(json_tool app-preflight "$app_file" "$app_path" "$container_app_name")
mapfile -t app_facts <<<"$app_facts_text"
[[ ${#app_facts[@]} -eq 2 ]] || fail 'could not obtain prior revision and container names'
prior_revision=${app_facts[0]}
container_name=${app_facts[1]}

prior_file="$work_dir/prior.json"
read_revision "$prior_revision" "$prior_file"
json_tool revision-prior "$prior_file" "$revisions_path/$prior_revision" "$prior_revision" "$container_name"

fetch_revision_list preflight
json_tool list-preflight "$revisions_path" "$prior_revision" "$revision_suffix" "${LIST_FILES[@]}"
json_tool templates-equal "$app_file" "$prior_file"

rollback_required=1
if ! az containerapp revision copy --subscription "$subscription_id" --resource-group "$resource_group" --name "$container_app_name" --from-revision "$prior_revision" --container-name "$container_name" --image "$image_digest" --revision-suffix "$revision_suffix" --set-env-vars ARTICLE_STORAGE_SCHEMA_MODE=migrate --output none; then
  fail 'revision copy command failed after mutation may have started'
fi

candidate_revision=''
for ((attempt = 1; attempt <= 24; attempt++)); do
  fetch_revision_list "candidate-$attempt"
  candidate_revision=$(json_tool list-candidate "$revisions_path" "$prior_revision" "$revision_suffix" "${LIST_FILES[@]}")
  if [[ -n $candidate_revision ]]; then break; fi
  if (( attempt < 24 )); then sleep 5; fi
done
[[ -n $candidate_revision ]] || fail 'candidate revision did not become visible after 24 polls'

candidate_file="$work_dir/candidate.json"
candidate_fqdn=''
for ((attempt = 1; attempt <= 24; attempt++)); do
  if read_revision "$candidate_revision" "$candidate_file"; then
    candidate_fqdn=$(json_tool revision-candidate "$candidate_file" "$revisions_path/$candidate_revision" "$candidate_revision" "$image_digest" "$revision_suffix" "$prior_file" "$container_name" 2>/dev/null || true)
    [[ -n $candidate_fqdn ]] && break
  fi
  if (( attempt < 24 )); then sleep 5; fi
done
[[ -n $candidate_fqdn ]] || fail 'candidate revision did not become healthy and equivalent after 24 polls'

postcopy_app_file="$work_dir/app-postcopy.json"
read_app "$postcopy_app_file"
json_tool app-prior-traffic "$postcopy_app_file" "$app_path" "$container_app_name" "$prior_revision"

http_check "https://$candidate_fqdn/health/live" health '' candidate-live
http_check "https://$candidate_fqdn/health/ready" health '' candidate-ready
http_check "https://$candidate_fqdn/" candidate-root "$public_base_url" candidate-root

if ! az containerapp ingress traffic set --subscription "$subscription_id" --resource-group "$resource_group" --name "$container_app_name" --revision-weight "$candidate_revision=100" "$prior_revision=0" --output none; then
  fail 'traffic promotion command failed after mutation may have been applied'
fi

promoted_app_file="$work_dir/app-promoted.json"
promotion_verified=0
for ((attempt = 1; attempt <= 24; attempt++)); do
  if read_app "$promoted_app_file" && json_tool app-promoted-traffic "$promoted_app_file" "$app_path" "$container_app_name" "$prior_revision" "$candidate_revision" 2>/dev/null; then
    promotion_verified=1
    break
  fi
  if (( attempt < 24 )); then sleep 5; fi
done
(( promotion_verified == 1 )) || fail 'promoted traffic did not converge after 24 polls'

promoted_prior_file="$work_dir/prior-promoted.json"
promoted_candidate_file="$work_dir/candidate-promoted.json"
revision_weights_verified=0
for ((attempt = 1; attempt <= 24; attempt++)); do
  if read_revision "$prior_revision" "$promoted_prior_file" &&
     read_revision "$candidate_revision" "$promoted_candidate_file" &&
     json_tool revision-promoted "$promoted_prior_file" "$revisions_path/$prior_revision" "$prior_revision" zero "$container_name" 2>/dev/null &&
     json_tool revision-promoted "$promoted_candidate_file" "$revisions_path/$candidate_revision" "$candidate_revision" 100 "$container_name" 2>/dev/null; then
    revision_weights_verified=1
    break
  fi
  if (( attempt < 24 )); then sleep 5; fi
done
(( revision_weights_verified == 1 )) || fail 'promoted revision state did not converge after 24 polls'

canonical_smoke promoted

rollback_required=0
printf 'prior_revision=%s\n' "$prior_revision"
printf 'candidate_revision=%s\n' "$candidate_revision"
printf 'image_digest=%s\n' "$image_digest"
printf 'deployment_status=succeeded\n'
