# Article storage schema rollout

The article RowKey migration is an explicit two-stage rollout. The same
immutable image digest is used for both stages. `compat` is the Azure default:
it reads legacy and current rows, writes only current rows, and never creates a
migration marker during startup. `migrate` completes the migration before the
HTTP listener starts and then uses the current-only repository. `repair` is a
recovery mode that ignores an existing marker, performs a full idempotent
convergence scan, and then uses the current-only repository.

The commands below are an operator runbook. Tasks 11–13 must wire the exact
resource names, deployment workflow gates, and post-deploy checks into Bicep
and CI before production use. Run all blocks in one POSIX shell session. Every
block repeats `set -eu`; when starting a new shell, rerun the variables block
so its placeholders and `assert_no_legacy_article_rows` function are defined.

## Variables and rollback floor

Use an immutable GHCR digest, never a tag. Capture the first digest that
contains the `compat|migrate|repair` implementation as the rollback floor and
retain it in GHCR:

```bash
set -eu

RESOURCE_GROUP='<resource-group>'
CONTAINER_APP='<container-app>'
STORAGE_ACCOUNT='<storage-account>'
IMAGE_DIGEST='ghcr.io/<owner>/<package>@sha256:<digest>'

assert_no_legacy_article_rows() {
  LEGACY_COUNT="$(
    az storage entity query \
      --account-name "$STORAGE_ACCOUNT" \
      --auth-mode login \
      --table-name articles \
      --filter "PartitionKey eq 'articles' and entityType eq 'article'" \
      --select RowKey id \
      --query 'length(items[?id == `null` || RowKey == id])' \
      --output tsv
  )"
  test "$LEGACY_COUNT" = 0 || {
    printf 'legacy article rows remain: %s\n' "$LEGACY_COUNT" >&2
    exit 1
  }
}

az containerapp show \
  --resource-group "$RESOURCE_GROUP" \
  --name "$CONTAINER_APP" \
  --query '{image:properties.template.containers[0].image,mode:properties.template.containers[0].env[?name==`ARTICLE_STORAGE_SCHEMA_MODE`].value|[0]}' \
  --output json
```

Record `IMAGE_DIGEST`, the Git commit, UTC time, and operator in the change
ticket. That digest is the rollback floor: after the migration marker exists,
never activate an older image that can write direct-ID rows.

## Stage 1: compatibility deployment

Deploy the rollback-floor artifact in compatibility mode:

```bash
set -eu

az containerapp revision set-mode \
  --resource-group "$RESOURCE_GROUP" \
  --name "$CONTAINER_APP" \
  --mode multiple

ACTIVE_REVISIONS_MODE="$(
  az containerapp show \
    --resource-group "$RESOURCE_GROUP" \
    --name "$CONTAINER_APP" \
    --query properties.configuration.activeRevisionsMode \
    --output tsv
)"
test "$ACTIVE_REVISIONS_MODE" = Multiple || {
  printf 'active revision mode is not Multiple: %s\n' "$ACTIVE_REVISIONS_MODE" >&2
  exit 1
}

az containerapp update \
  --resource-group "$RESOURCE_GROUP" \
  --name "$CONTAINER_APP" \
  --image "$IMAGE_DIGEST" \
  --set-env-vars ARTICLE_STORAGE_SCHEMA_MODE=compat

az containerapp revision list \
  --resource-group "$RESOURCE_GROUP" \
  --name "$CONTAINER_APP" \
  --query '[].{name:name,active:properties.active,health:properties.healthState,image:properties.template.containers[0].image,mode:properties.template.containers[0].env[?name==`ARTICLE_STORAGE_SCHEMA_MODE`].value|[0]}' \
  --output table
```

Wait for the new revision to be healthy, assign it 100% of traffic, and
deactivate every older active revision by its exact name:

```bash
set -eu

COMPAT_REVISION='<healthy-compat-revision>'

az containerapp revision set-mode \
  --resource-group "$RESOURCE_GROUP" \
  --name "$CONTAINER_APP" \
  --mode multiple

ACTIVE_REVISIONS_MODE="$(
  az containerapp show \
    --resource-group "$RESOURCE_GROUP" \
    --name "$CONTAINER_APP" \
    --query properties.configuration.activeRevisionsMode \
    --output tsv
)"
test "$ACTIVE_REVISIONS_MODE" = Multiple || {
  printf 'active revision mode is not Multiple: %s\n' "$ACTIVE_REVISIONS_MODE" >&2
  exit 1
}

az containerapp ingress traffic set \
  --resource-group "$RESOURCE_GROUP" \
  --name "$CONTAINER_APP" \
  --revision-weight "$COMPAT_REVISION=100"

az containerapp revision deactivate \
  --resource-group "$RESOURCE_GROUP" \
  --name "$CONTAINER_APP" \
  --revision '<older-revision-name>'
```

Azure Container Apps defaults to single-revision mode. Weighted revision
traffic is therefore forbidden until `revision set-mode --mode multiple` has
succeeded and the exact resource property confirms `Multiple`; the guard above
aborts under `set -eu` if the command, query, or comparison fails. Stage 1 sets
and verifies the mode before its revision-producing update so the prior
revision is not implicitly replaced under single-revision semantics.

Repeat the revision-list command. Do not continue until every active revision
uses exactly `IMAGE_DIGEST` and `compat`, and every older revision is inactive.
Exercise create, save, publish, withdraw, and list from the admin console; this
proves all active writers emit current RowKeys before the marker can exist.

## Stage 2: migration

Switch the same immutable artifact to migration mode:

```bash
set -eu

az containerapp revision set-mode \
  --resource-group "$RESOURCE_GROUP" \
  --name "$CONTAINER_APP" \
  --mode multiple

ACTIVE_REVISIONS_MODE="$(
  az containerapp show \
    --resource-group "$RESOURCE_GROUP" \
    --name "$CONTAINER_APP" \
    --query properties.configuration.activeRevisionsMode \
    --output tsv
)"
test "$ACTIVE_REVISIONS_MODE" = Multiple || {
  printf 'active revision mode is not Multiple: %s\n' "$ACTIVE_REVISIONS_MODE" >&2
  exit 1
}

az containerapp update \
  --resource-group "$RESOURCE_GROUP" \
  --name "$CONTAINER_APP" \
  --image "$IMAGE_DIGEST" \
  --set-env-vars ARTICLE_STORAGE_SCHEMA_MODE=migrate

az containerapp revision list \
  --resource-group "$RESOURCE_GROUP" \
  --name "$CONTAINER_APP" \
  --query '[].{name:name,active:properties.active,health:properties.healthState,image:properties.template.containers[0].image,mode:properties.template.containers[0].env[?name==`ARTICLE_STORAGE_SCHEMA_MODE`].value|[0]}' \
  --output table
```

Startup must fail before listening if migration cannot converge. When the new
revision is healthy, re-establish and verify multiple-revision mode before
changing traffic, then deactivate the compatibility revision:

```bash
set -eu

MIGRATE_REVISION='<healthy-migrate-revision>'

az containerapp revision set-mode \
  --resource-group "$RESOURCE_GROUP" \
  --name "$CONTAINER_APP" \
  --mode multiple

ACTIVE_REVISIONS_MODE="$(
  az containerapp show \
    --resource-group "$RESOURCE_GROUP" \
    --name "$CONTAINER_APP" \
    --query properties.configuration.activeRevisionsMode \
    --output tsv
)"
test "$ACTIVE_REVISIONS_MODE" = Multiple || {
  printf 'active revision mode is not Multiple: %s\n' "$ACTIVE_REVISIONS_MODE" >&2
  exit 1
}

az containerapp ingress traffic set \
  --resource-group "$RESOURCE_GROUP" \
  --name "$CONTAINER_APP" \
  --revision-weight "$MIGRATE_REVISION=100"

az containerapp revision deactivate \
  --resource-group "$RESOURCE_GROUP" \
  --name "$CONTAINER_APP" \
  --revision "$COMPAT_REVISION"
```

Verify the durable marker with Microsoft Entra authentication:

```bash
set -eu

az storage entity show \
  --account-name "$STORAGE_ACCOUNT" \
  --auth-mode login \
  --table-name articles \
  --partition-key articles \
  --row-key 'schema:article-row-key:v1' \
  --output json

az storage entity query \
  --account-name "$STORAGE_ACCOUNT" \
  --auth-mode login \
  --table-name articles \
  --filter "PartitionKey eq 'articles' and entityType eq 'article'" \
  --select RowKey id createdAt status \
  --output json

assert_no_legacy_article_rows
```

Compare the logical article count with the count recorded before migration and
retain the fail-closed check output in the change ticket. Azure CLI returns
queried entities below top-level `items`, with `RowKey` capitalized and `id`
lowercase. A legacy row can either omit `id` (JMESPath evaluates the missing
property as `null`) or store an `id` equal to its direct-ID `RowKey`; both cases
are counted. Every current row must have a non-null `id` and a reverse-time
`RowKey` different from that `id`. The assertion exits unless the count is
exactly zero. The command intentionally omits `--num-results`, allowing Azure
CLI to enumerate all service pages.

Then prove the lifecycle: publish v1, save draft v2, confirm the public page is
still v1 while authenticated preview is v2, republish and confirm public v2,
withdraw and confirm the public route is absent, then republish. Record the
marker, counts, health checks, and lifecycle result in the change ticket.

## Rollback

Rollback may use only `IMAGE_DIGEST` (the compatibility floor) or a newer
digest. Keep `ARTICLE_STORAGE_SCHEMA_MODE=migrate` for normal constant-time
operation. Never reactivate a pre-floor revision, even if it is still present
in Container Apps, because it can create legacy rows behind the marker.

## Recovery after a late legacy write

`repair` is deliberately a downtime procedure. Do not delete or edit the
migration marker. First quiesce all writers by deactivating every active
revision and verify the active-revision query returns no entries. Wait at least
the application write timeout (30 seconds) for in-flight requests to finish.

```bash
set -eu

az containerapp revision list \
  --resource-group "$RESOURCE_GROUP" \
  --name "$CONTAINER_APP" \
  --query '[?properties.active].name' \
  --output tsv

az containerapp revision deactivate \
  --resource-group "$RESOURCE_GROUP" \
  --name "$CONTAINER_APP" \
  --revision '<each-active-revision-name>'

az containerapp update \
  --resource-group "$RESOURCE_GROUP" \
  --name "$CONTAINER_APP" \
  --image "$IMAGE_DIGEST" \
  --set-env-vars ARTICLE_STORAGE_SCHEMA_MODE=repair
```

The repair revision ignores the marker, scans all article rows, conditionally
converges late legacy rows, verifies a clean pass, and only then starts its
listener. Verify the marker and row count, then run the exact same fail-closed
legacy-row assertion before the lifecycle check:

```bash
set -eu

assert_no_legacy_article_rows
```

Finally deploy the same digest with `ARTICLE_STORAGE_SCHEMA_MODE=migrate`, wait
for health, and only then restore traffic after repeating the revision-mode
preflight:

```bash
set -eu

REPAIR_REVISION='<healthy-repair-revision>'
RECOVERY_MIGRATE_REVISION='<healthy-recovery-migrate-revision>'

az containerapp revision set-mode \
  --resource-group "$RESOURCE_GROUP" \
  --name "$CONTAINER_APP" \
  --mode multiple

ACTIVE_REVISIONS_MODE="$(
  az containerapp show \
    --resource-group "$RESOURCE_GROUP" \
    --name "$CONTAINER_APP" \
    --query properties.configuration.activeRevisionsMode \
    --output tsv
)"
test "$ACTIVE_REVISIONS_MODE" = Multiple || {
  printf 'active revision mode is not Multiple: %s\n' "$ACTIVE_REVISIONS_MODE" >&2
  exit 1
}

az containerapp update \
  --resource-group "$RESOURCE_GROUP" \
  --name "$CONTAINER_APP" \
  --image "$IMAGE_DIGEST" \
  --set-env-vars ARTICLE_STORAGE_SCHEMA_MODE=migrate

az containerapp ingress traffic set \
  --resource-group "$RESOURCE_GROUP" \
  --name "$CONTAINER_APP" \
  --revision-weight "$RECOVERY_MIGRATE_REVISION=100"

az containerapp revision deactivate \
  --resource-group "$RESOURCE_GROUP" \
  --name "$CONTAINER_APP" \
  --revision "$REPAIR_REVISION"
```

A failed or ambiguous repair remains a hard stop; keep writers quiesced and
rerun `repair` after investigating.
