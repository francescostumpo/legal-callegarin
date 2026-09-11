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
and CI before production use.

## Variables and rollback floor

Use an immutable GHCR digest, never a tag. Capture the first digest that
contains the `compat|migrate|repair` implementation as the rollback floor and
retain it in GHCR:

```bash
RESOURCE_GROUP='<resource-group>'
CONTAINER_APP='<container-app>'
STORAGE_ACCOUNT='<storage-account>'
IMAGE_DIGEST='ghcr.io/<owner>/<package>@sha256:<digest>'

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
COMPAT_REVISION='<healthy-compat-revision>'

az containerapp ingress traffic set \
  --resource-group "$RESOURCE_GROUP" \
  --name "$CONTAINER_APP" \
  --revision-weight "$COMPAT_REVISION=100"

az containerapp revision deactivate \
  --resource-group "$RESOURCE_GROUP" \
  --name "$CONTAINER_APP" \
  --revision '<older-revision-name>'
```

Repeat the revision-list command. Do not continue until every active revision
uses exactly `IMAGE_DIGEST` and `compat`, and every older revision is inactive.
Exercise create, save, publish, withdraw, and list from the admin console; this
proves all active writers emit current RowKeys before the marker can exist.

## Stage 2: migration

Switch the same immutable artifact to migration mode:

```bash
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
revision is healthy, route 100% traffic to it and deactivate the compatibility
revision as in stage 1.

Verify the durable marker with Microsoft Entra authentication:

```bash
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
```

Compare the logical article count with the count recorded before migration and
confirm there are no article entities whose `RowKey` equals their `id`.
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
listener. Verify marker, row count, zero `RowKey == id` rows, health, and the
article lifecycle again. Finally deploy the same digest with
`ARTICLE_STORAGE_SCHEMA_MODE=migrate`, wait for health, route traffic to it,
and deactivate the repair revision. A failed or ambiguous repair remains a
hard stop; keep writers quiesced and rerun `repair` after investigating.
