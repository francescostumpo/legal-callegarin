#!/bin/sh
set -eu
set -f

fail() {
  printf '%s\n' "$*" >&2
  exit 1
}

require_container_app() {
  : "${RESOURCE_GROUP:?RESOURCE_GROUP is required}"
  : "${CONTAINER_APP:?CONTAINER_APP is required}"
  : "${IMAGE_DIGEST:?IMAGE_DIGEST is required}"
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

latest_revision_name() {
  az containerapp show \
    --resource-group "$RESOURCE_GROUP" \
    --name "$CONTAINER_APP" \
    --query properties.latestRevisionName \
    --output tsv
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
      --query '[properties.healthState,properties.template.containers[0].image,(properties.template.containers[0].env[?name==`ARTICLE_STORAGE_SCHEMA_MODE`].value|[0])]' \
      --output tsv
  )"

  old_ifs=$IFS
  IFS="$(printf '\t')"
  set -- $revision_facts
  IFS=$old_ifs
  test "$#" -eq 3 || fail "could not verify revision $revision"

  health_state=$1
  revision_image=$2
  revision_mode=$3
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
  require_container_app

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

  az containerapp update \
    --resource-group "$RESOURCE_GROUP" \
    --name "$CONTAINER_APP" \
    --image "$IMAGE_DIGEST" \
    --set-env-vars ARTICLE_STORAGE_SCHEMA_MODE=repair >/dev/null

  observe_revisions
  repair_revision="$(latest_revision_name)"
  verify_revision "$repair_revision" repair
  printf '%s\n' "$repair_revision"
}

create_and_promote_recovery_migrate() {
  require_container_app
  repair_revision=$1
  validate_revision_name "$repair_revision"

  az containerapp update \
    --resource-group "$RESOURCE_GROUP" \
    --name "$CONTAINER_APP" \
    --image "$IMAGE_DIGEST" \
    --set-env-vars ARTICLE_STORAGE_SCHEMA_MODE=migrate >/dev/null

  observe_revisions
  recovery_revision="$(latest_revision_name)"
  validate_revision_name "$recovery_revision"
  test "$recovery_revision" != "$repair_revision" || fail "migrate update did not create a new revision"
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
