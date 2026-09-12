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
    '' | [!a-z]* | *[!a-z0-9-]* | *-) fail "invalid Container Apps app name: $CONTAINER_APP" ;;
  esac

  case "$ROLLOUT_ID" in
    '' | [!a-z]* | *[!a-z0-9-]* | *-) fail "invalid ROLLOUT_ID: $ROLLOUT_ID" ;;
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
}

observe_revisions() {
  az containerapp revision list \
    --resource-group "$RESOURCE_GROUP" \
    --name "$CONTAINER_APP" \
    --query '[].{name:name,active:properties.active,health:properties.healthState,image:properties.template.containers[0].image,mode:properties.template.containers[0].env[?name==`ARTICLE_STORAGE_SCHEMA_MODE`].value|[0]}' \
    --output table >&2
}

assert_revision_suffix_available() {
  expected_revision=$1
  suffix_match_count="$(
    az containerapp revision list \
      --resource-group "$RESOURCE_GROUP" \
      --name "$CONTAINER_APP" \
      --query "length([?name == '$expected_revision'])" \
      --output tsv
  )"
  case "$suffix_match_count" in
    '' | *[!0-9]*) fail "invalid suffix match count for $expected_revision: $suffix_match_count" ;;
  esac
  test "$suffix_match_count" = 0 || fail "revision suffix already exists: $expected_revision"
}

create_revision() {
  schema_mode=$1
  revision_suffix=$2
  expected_revision=$3

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
  test "$created_revision" = "$expected_revision" || fail "update returned unexpected revision: $created_revision"
  printf '%s\n' "$created_revision"
}

verify_revision() {
  revision=$1
  expected_mode=$2
  validate_revision_name "$revision"

  revision_facts="$(
    az containerapp revision show \
      --resource-group "$RESOURCE_GROUP" \
      --name "$CONTAINER_APP" \
      --revision "$revision" \
      --query '[[to_string(properties.active),properties.healthState,properties.template.containers[0].image,(properties.template.containers[0].env[?name==`ARTICLE_STORAGE_SCHEMA_MODE`].value|[0])]]' \
      --output tsv
  )"

  old_ifs=$IFS
  IFS="$(printf '\t')"
  set -- $revision_facts
  IFS=$old_ifs
  test "$#" -eq 4 || fail "could not verify revision $revision"

  revision_active=$1
  health_state=$2
  revision_image=$3
  revision_mode=$4
  test "$revision_active" = true || fail "revision $revision is not active: $revision_active"
  test "$health_state" = Healthy || fail "revision $revision is not Healthy: $health_state"
  test "$revision_image" = "$IMAGE_DIGEST" || fail "revision $revision image does not match IMAGE_DIGEST"
  test "$revision_mode" = "$expected_mode" || fail "revision $revision schema mode is not $expected_mode: $revision_mode"
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
  expected_repair_revision="$CONTAINER_APP--$repair_suffix"
  validate_revision_name "$expected_repair_revision"

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
  assert_revision_suffix_available "$expected_repair_revision"
  repair_revision="$(
    create_revision repair "$repair_suffix" "$expected_repair_revision"
  )"

  observe_revisions
  verify_revision "$repair_revision" repair
  printf '%s\n' "$repair_revision"
}

create_and_promote_recovery_migrate() {
  require_rollout_configuration
  repair_revision=$1
  validate_revision_name "$repair_revision"

  migrate_suffix="migrate-$ROLLOUT_ID"
  expected_migrate_revision="$CONTAINER_APP--$migrate_suffix"
  validate_revision_name "$expected_migrate_revision"

  set_and_verify_multiple_mode
  assert_revision_suffix_available "$expected_migrate_revision"
  recovery_revision="$(
    create_revision migrate "$migrate_suffix" "$expected_migrate_revision"
  )"

  observe_revisions
  verify_revision "$recovery_revision" migrate

  set_and_verify_multiple_mode
  az containerapp ingress traffic set \
    --resource-group "$RESOURCE_GROUP" \
    --name "$CONTAINER_APP" \
    --revision-weight "$recovery_revision=100" >/dev/null

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
