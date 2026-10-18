#!/bin/sh
set -eu
set -f

fail() {
  printf '%s\n' "$*" >&2
  exit 1
}

require_rollout_configuration() {
  : "${RESOURCE_GROUP:?RESOURCE_GROUP is required}"
  : "${CONTAINER_APP:?CONTAINER_APP is required}"
  : "${IMAGE_DIGEST:?IMAGE_DIGEST is required}"
  : "${ROLLOUT_ID:?ROLLOUT_ID is required}"

  case "$CONTAINER_APP" in
    '' | [!a-z]* | *[!a-z0-9-]* | *- | *--*) fail "invalid Container Apps app name: $CONTAINER_APP" ;;
  esac
  test "${#CONTAINER_APP}" -ge 2 || fail "Container Apps app name is shorter than 2 characters"
  test "${#CONTAINER_APP}" -lt 32 || fail "Container Apps app name must be shorter than 32 characters"

  case "$ROLLOUT_ID" in
    '' | [!a-z]* | *[!a-z0-9-]* | *- | *--*) fail "invalid ROLLOUT_ID: $ROLLOUT_ID" ;;
  esac
  test "${#ROLLOUT_ID}" -le 16 || fail "ROLLOUT_ID exceeds 16 characters"

  case "$IMAGE_DIGEST" in
    ghcr.io/*@sha256:*) ;;
    *) fail 'IMAGE_DIGEST must be a canonical lowercase GHCR sha256 reference' ;;
  esac
  repository_and_digest=${IMAGE_DIGEST#ghcr.io/}
  image_repository=${repository_and_digest%@sha256:*}
  image_sha256=${IMAGE_DIGEST##*@sha256:}
  case "$image_repository" in
    '' | /* | */ | *//* | *[!a-z0-9._/-]*) fail 'IMAGE_DIGEST has an invalid GHCR repository path' ;;
  esac
  case "$image_repository" in
    */*) ;;
    *) fail 'IMAGE_DIGEST must include a GHCR owner and package' ;;
  esac
  test "${#image_sha256}" -eq 64 || fail 'IMAGE_DIGEST sha256 must contain exactly 64 lowercase hex characters'
  case "$image_sha256" in
    *[!0-9a-f]*) fail 'IMAGE_DIGEST sha256 must contain exactly 64 lowercase hex characters' ;;
  esac
}

validate_revision_name() {
  case "$1" in
    '' | *[!a-z0-9-]*) fail "invalid Container Apps revision name: $1" ;;
  esac
  test "${#1}" -le 64 || fail "Container Apps revision name exceeds 64 characters: $1"
}

validate_revision_suffix() {
  revision_suffix=$1
  case "$revision_suffix" in
    '' | [!a-z]* | *[!a-z0-9-]* | *- | *--*) fail "invalid Container Apps revision suffix: $revision_suffix" ;;
  esac
  test "${#revision_suffix}" -le 64 || fail "Container Apps revision suffix exceeds 64 characters: $revision_suffix"
}

validate_repair_revision_argument() {
  validate_revision_name "$1"
}

assert_distinct_revisions() {
  test "$1" != "$2" || fail "repair and migrate revisions must be distinct: $1"
}

observe_revisions() {
  az containerapp revision list \
    --resource-group "$RESOURCE_GROUP" \
    --name "$CONTAINER_APP" \
    --query '[].{name:name,active:properties.active,health:properties.healthState,image:properties.template.containers[0].image,mode:properties.template.containers[0].env[?name==`ARTICLE_STORAGE_SCHEMA_MODE`].value|[0]}' \
    --output table >&2
}

assert_revision_suffix_available() {
  expected_suffix=$1
  suffix_match_count="$(
    az containerapp revision list \
      --resource-group "$RESOURCE_GROUP" \
      --name "$CONTAINER_APP" \
      --all \
      --query "length([?properties.template.revisionSuffix == '$expected_suffix'])" \
      --output tsv
  )"
  case "$suffix_match_count" in
    '' | *[!0-9]*) fail "invalid suffix match count for $expected_suffix: $suffix_match_count" ;;
  esac
  test "$suffix_match_count" = 0 || fail "revision suffix already exists: $expected_suffix"
}

assert_revision_exists() {
  expected_revision=$1
  revision_match_count="$(
    az containerapp revision list \
      --resource-group "$RESOURCE_GROUP" \
      --name "$CONTAINER_APP" \
      --all \
      --query "length([?name == '$expected_revision'])" \
      --output tsv
  )"
  case "$revision_match_count" in
    '' | *[!0-9]*) fail "invalid revision match count for $expected_revision: $revision_match_count" ;;
  esac
  test "$revision_match_count" = 1 || fail "revision does not exist exactly once in target app: $expected_revision"
}

assert_created_revision_exists() {
  created_revision=$1
  revision_match_count="$(
    az containerapp revision list \
      --resource-group "$RESOURCE_GROUP" \
      --name "$CONTAINER_APP" \
      --all \
      --query "length([?name == '$created_revision'].name)" \
      --output tsv
  )"
  case "$revision_match_count" in
    '' | *[!0-9]*) fail "invalid revision match count for $created_revision: $revision_match_count" ;;
  esac
  test "$revision_match_count" = 1 || fail "Azure-returned revision does not exist exactly once in target app: $created_revision"
}

assert_named_only_traffic() {
  traffic_facts="$(
    az containerapp show \
      --resource-group "$RESOURCE_GROUP" \
      --name "$CONTAINER_APP" \
      --query "[[length(properties.configuration.ingress.traffic),length(properties.configuration.ingress.traffic[?latestRevision == \`true\` || revisionName == \`null\` || revisionName == ''])]]" \
      --output tsv
  )"

  old_ifs=$IFS
  IFS="$(printf '\t')"
  set -- $traffic_facts
  IFS=$old_ifs
  test "$#" -eq 2 || fail 'could not verify named-only ingress traffic'

  traffic_rule_count=$1
  unsafe_traffic_rule_count=$2
  case "$traffic_rule_count:$unsafe_traffic_rule_count" in
    *[!0-9:]*) fail "invalid ingress traffic counts: $traffic_rule_count $unsafe_traffic_rule_count" ;;
  esac
  test "$traffic_rule_count" -gt 0 || fail 'ingress traffic has no explicit named revision rule'
  test "$unsafe_traffic_rule_count" = 0 || fail 'ingress traffic can target the latest revision; replace it manually with named revision weights before recovery'
}

create_revision() {
  schema_mode=$1
  revision_suffix=$2

  created_revision="$(
    az containerapp update \
      --resource-group "$RESOURCE_GROUP" \
      --name "$CONTAINER_APP" \
      --image "$IMAGE_DIGEST" \
      --revision-suffix "$revision_suffix" \
      --set-env-vars "ARTICLE_STORAGE_SCHEMA_MODE=$schema_mode" \
      --query properties.latestRevisionName \
      --output tsv
  )"
  validate_revision_name "$created_revision"
  assert_created_revision_exists "$created_revision"
  printf '%s\n' "$created_revision"
}

verify_revision() {
  revision=$1
  expected_mode=$2
  expected_suffix=$3
  validate_revision_name "$revision"

  revision_facts="$(
    az containerapp revision show \
      --resource-group "$RESOURCE_GROUP" \
      --name "$CONTAINER_APP" \
      --revision "$revision" \
      --query '[[to_string(properties.active),properties.healthState,properties.template.containers[0].image,(properties.template.containers[0].env[?name==`ARTICLE_STORAGE_SCHEMA_MODE`].value|[0]),properties.template.revisionSuffix]]' \
      --output tsv
  )"

  old_ifs=$IFS
  IFS="$(printf '\t')"
  set -- $revision_facts
  IFS=$old_ifs
  test "$#" -eq 5 || fail "could not verify revision $revision"

  revision_active=$1
  health_state=$2
  revision_image=$3
  revision_mode=$4
  revision_suffix=$5
  test "$revision_active" = true || fail "revision $revision is not active: $revision_active"
  test "$health_state" = Healthy || fail "revision $revision is not Healthy: $health_state"
  test "$revision_image" = "$IMAGE_DIGEST" || fail "revision $revision image does not match IMAGE_DIGEST"
  test "$revision_mode" = "$expected_mode" || fail "revision $revision schema mode is not $expected_mode: $revision_mode"
  test "$revision_suffix" = "$expected_suffix" || fail "revision $revision suffix is not $expected_suffix: $revision_suffix"
}

set_and_verify_multiple_mode() {
  az containerapp revision set-mode \
    --resource-group "$RESOURCE_GROUP" \
    --name "$CONTAINER_APP" \
    --mode multiple >/dev/null

  active_revisions_mode="$(
    az containerapp show \
      --resource-group "$RESOURCE_GROUP" \
      --name "$CONTAINER_APP" \
      --query properties.configuration.activeRevisionsMode \
      --output tsv
  )"
  test "$active_revisions_mode" = Multiple || fail "active revision mode is not Multiple: $active_revisions_mode"
}

assert_no_legacy_article_rows() {
  : "${STORAGE_ACCOUNT:?STORAGE_ACCOUNT is required}"

  legacy_count="$(
    az storage entity query \
      --account-name "$STORAGE_ACCOUNT" \
      --auth-mode login \
      --table-name articles \
      --filter "PartitionKey eq 'articles' and entityType eq 'article'" \
      --select RowKey id \
      --query 'length(items[?id == `null` || RowKey == id])' \
      --output tsv
  )"
  case "$legacy_count" in
    '' | *[!0-9]*) fail "invalid legacy article row count: $legacy_count" ;;
  esac
  test "$legacy_count" = 0 || fail "legacy article rows remain: $legacy_count"
}

quiesce_and_create_repair() {
  require_rollout_configuration
  repair_suffix="repair-$ROLLOUT_ID"
  validate_revision_suffix "$repair_suffix"

  assert_named_only_traffic

  active_revisions="$(
    az containerapp revision list \
      --resource-group "$RESOURCE_GROUP" \
      --name "$CONTAINER_APP" \
      --query '[?properties.active].name' \
      --output tsv
  )"

  if test -n "$active_revisions"; then
    old_ifs=$IFS
    IFS='
'
    for revision in $active_revisions; do
      IFS=$old_ifs
      validate_revision_name "$revision"
      az containerapp revision deactivate \
        --resource-group "$RESOURCE_GROUP" \
        --name "$CONTAINER_APP" \
        --revision "$revision" >/dev/null
      IFS='
'
    done
    IFS=$old_ifs
  fi

  residual_active_count="$(
    az containerapp revision list \
      --resource-group "$RESOURCE_GROUP" \
      --name "$CONTAINER_APP" \
      --query 'length([?properties.active])' \
      --output tsv
  )"
  case "$residual_active_count" in
    '' | *[!0-9]*) fail "invalid active revision count: $residual_active_count" ;;
  esac
  test "$residual_active_count" = 0 || fail "active revisions remain after quiescence: $residual_active_count"

  sleep 30

  set_and_verify_multiple_mode
  assert_revision_suffix_available "$repair_suffix"
  assert_named_only_traffic
  repair_revision="$(
    create_revision repair "$repair_suffix"
  )"

  observe_revisions
  verify_revision "$repair_revision" repair "$repair_suffix"
  printf '%s\n' "$repair_revision"
}

create_and_promote_recovery_migrate() {
  require_rollout_configuration
  repair_revision=$1
  validate_repair_revision_argument "$repair_revision"

  repair_suffix="repair-$ROLLOUT_ID"
  validate_revision_suffix "$repair_suffix"
  migrate_suffix="migrate-$ROLLOUT_ID"
  validate_revision_suffix "$migrate_suffix"

  assert_revision_exists "$repair_revision"
  verify_revision "$repair_revision" repair "$repair_suffix"

  set_and_verify_multiple_mode
  assert_revision_suffix_available "$migrate_suffix"
  assert_named_only_traffic
  recovery_revision="$(
    create_revision migrate "$migrate_suffix"
  )"
  assert_distinct_revisions "$repair_revision" "$recovery_revision"

  observe_revisions
  verify_revision "$recovery_revision" migrate "$migrate_suffix"

  set_and_verify_multiple_mode
  az containerapp ingress traffic set \
    --resource-group "$RESOURCE_GROUP" \
    --name "$CONTAINER_APP" \
    --revision-weight "$recovery_revision=100" >/dev/null

  assert_distinct_revisions "$repair_revision" "$recovery_revision"
  verify_revision "$repair_revision" repair "$repair_suffix"
  az containerapp revision deactivate \
    --resource-group "$RESOURCE_GROUP" \
    --name "$CONTAINER_APP" \
    --revision "$repair_revision" >/dev/null

  printf '%s\n' "$recovery_revision"
}

case "${1:-}" in
  assert-no-legacy)
    test "$#" -eq 1 || fail 'usage: article-storage-rollout.sh assert-no-legacy'
    assert_no_legacy_article_rows
    ;;
  quiesce-and-create-repair)
    test "$#" -eq 1 || fail 'usage: article-storage-rollout.sh quiesce-and-create-repair'
    quiesce_and_create_repair
    ;;
  create-and-promote-recovery-migrate)
    test "$#" -eq 2 || fail 'usage: article-storage-rollout.sh create-and-promote-recovery-migrate REPAIR_REVISION'
    create_and_promote_recovery_migrate "$2"
    ;;
  *)
    fail 'usage: article-storage-rollout.sh {assert-no-legacy|quiesce-and-create-repair|create-and-promote-recovery-migrate REPAIR_REVISION}'
    ;;
esac
