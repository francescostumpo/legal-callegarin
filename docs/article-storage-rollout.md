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
so its placeholders and rollout-script path are defined. Run the commands from
the repository root at the exact Git commit recorded in the change ticket.

## Variables and rollback floor

Use an immutable GHCR digest, never a tag. The operational script accepts only
a canonical lowercase `ghcr.io/<owner>/<package>@sha256:<digest>` reference
whose digest is exactly 64 lowercase hexadecimal characters. Capture the first
digest that contains the `compat|migrate|repair` implementation as the rollback
floor and retain it in GHCR:

```bash
set -eu

RESOURCE_GROUP='<resource-group>'
CONTAINER_APP='<container-app>'
STORAGE_ACCOUNT='<storage-account>'
IMAGE_DIGEST='ghcr.io/<owner>/<package>@sha256:<digest>'
ROLLOUT_ID='r20260912t0037'
ROLLOUT_SCRIPT='./scripts/article-storage-rollout.sh'
test -r "$ROLLOUT_SCRIPT"

az containerapp show \
  --resource-group "$RESOURCE_GROUP" \
  --name "$CONTAINER_APP" \
  --query '{image:properties.template.containers[0].image,mode:properties.template.containers[0].env[?name==`ARTICLE_STORAGE_SCHEMA_MODE`].value|[0]}' \
  --output json
```

`ROLLOUT_ID` must be unique for the recovery attempt, begin with a lowercase
letter, contain only lowercase letters, digits, and hyphens, end with a letter
or digit, contain no consecutive hyphens, and contain at most 16 characters.
The script derives distinct `repair-$ROLLOUT_ID` and
`migrate-$ROLLOUT_ID` revision suffixes and validates both suffixes plus the
complete Container Apps revision-name length before any Azure call. Record
`IMAGE_DIGEST`, `ROLLOUT_ID`, the Git commit, UTC time, and operator in the
change ticket. That digest is the rollback floor: after the migration marker
exists, never activate an older image that can write direct-ID rows.

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

sh "$ROLLOUT_SCRIPT" assert-no-legacy
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
migration marker. Recovery requires ingress traffic to contain at least one
rule and every rule to name an exact `revisionName`. A default or residual
`latestRevision: true` rule, or any rule without a revision name, could route a
new repair revision automatically and is forbidden. The script checks this
precondition before any deactivation and fails without changing traffic.

If the precondition fails, stop and inspect all revisions. Select an exact
currently serving revision only after confirming its health, image, and schema
mode. Replace the placeholders below and perform this traffic preparation as a
separate, deliberate operator action before invoking recovery; the rollout
script will not perform it for you:

```bash
set -eu

SAFE_CURRENT_REVISION='<exact-verified-current-revision-name>'

az containerapp revision list \
  --resource-group "$RESOURCE_GROUP" \
  --name "$CONTAINER_APP" \
  --all \
  --query '[].{name:name,active:properties.active,health:properties.healthState,image:properties.template.containers[0].image,mode:properties.template.containers[0].env[?name==`ARTICLE_STORAGE_SCHEMA_MODE`].value|[0]}' \
  --output table

az containerapp revision show \
  --resource-group "$RESOURCE_GROUP" \
  --name "$CONTAINER_APP" \
  --revision "$SAFE_CURRENT_REVISION" \
  --query '{active:properties.active,health:properties.healthState,image:properties.template.containers[0].image,mode:properties.template.containers[0].env[?name==`ARTICLE_STORAGE_SCHEMA_MODE`].value|[0]}' \
  --output json

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
test "$ACTIVE_REVISIONS_MODE" = Multiple

az containerapp ingress traffic set \
  --resource-group "$RESOURCE_GROUP" \
  --name "$CONTAINER_APP" \
  --revision-weight "$SAFE_CURRENT_REVISION=100"

az containerapp show \
  --resource-group "$RESOURCE_GROUP" \
  --name "$CONTAINER_APP" \
  --query properties.configuration.ingress.traffic \
  --output json
```

Confirm manually that the final JSON contains only named revision weights and
no `latestRevision: true` rule. Then invoke recovery. The script rechecks the
same property before quiescence, deactivates every active revision, verifies a
fresh active-revision query returns zero, and waits the full 30-second
application write timeout for in-flight requests to finish. It will not accept
a shorter drain interval:

```bash
set -eu

REPAIR_REVISION="$(
  sh "$ROLLOUT_SCRIPT" quiesce-and-create-repair
)"
printf 'verified repair revision: %s\n' "$REPAIR_REVISION"
```

`quiesce-and-create-repair` obtains every active revision name and deactivates
each exact name. It then makes a new count query and aborts before both the
drain and update unless that count is zero. Only after `sleep 30` does it set
multiple-revision mode and freshly require the exact property value
`Multiple`. It rejects an already existing `repair-$ROLLOUT_ID` revision by
listing all revisions, including inactive ones. Immediately before update it
again requires at least one named-only traffic rule and no implicit latest
rule. The update uses the unique suffix and returns its own
`properties.latestRevisionName`; the script requires the response to equal
`$CONTAINER_APP-repair-$ROLLOUT_ID`, rather than making an app-wide latest
revision query. It then lists revisions for operator observation and requires
that exact generated revision to have `active=true`, `healthState=Healthy`, the
exact `IMAGE_DIGEST`, and `ARTICLE_STORAGE_SCHEMA_MODE=repair` before returning
its name. Under `set -eu`, a failed command substitution stops this runbook.

The repair revision ignores the marker, scans all article rows, conditionally
converges late legacy rows, verifies a clean pass, and only then starts its
listener. Repeat the Stage 2 marker and row-list queries, then run the same
versioned fail-closed legacy-row assertion before the lifecycle check:

```bash
set -eu

sh "$ROLLOUT_SCRIPT" assert-no-legacy
```

Finally let the rollout script deploy the same digest with
`ARTICLE_STORAGE_SCHEMA_MODE=migrate` and promote only the revision generated
by that update:

```bash
set -eu

RECOVERY_MIGRATE_REVISION="$(
  sh "$ROLLOUT_SCRIPT" \
    create-and-promote-recovery-migrate \
    "$REPAIR_REVISION"
)"
printf 'promoted recovery migrate revision: %s\n' "$RECOVERY_MIGRATE_REVISION"
```

Before the migration update, the supplied `REPAIR_REVISION` must belong
exactly to `CONTAINER_APP`, have a
valid suffix, differ from the expected migrate revision, and occur exactly once
in an all-revisions lookup. That exact revision must still have `active=true`,
`healthState=Healthy`, the exact `IMAGE_DIGEST`, and schema mode `repair`, or no
migrate update occurs. The script then freshly sets and verifies
`activeRevisionsMode=Multiple`, rejects an existing
`migrate-$ROLLOUT_ID` revision across active and inactive revisions, and
immediately rechecks named-only traffic before update. The update uses the
unique suffix and captures its own `properties.latestRevisionName`; the
response must equal `$CONTAINER_APP-migrate-$ROLLOUT_ID`.

After listing revisions for observation, the script requires that exact
migrate revision to have `active=true`, `healthState=Healthy`, the exact
`IMAGE_DIGEST`, and schema mode `migrate`. Immediately before promotion it
again sets and freshly verifies `activeRevisionsMode=Multiple`; only then does
it assign 100% traffic. Before the final deactivation it reasserts that repair
and migrate are distinct and reverifies every repair property. The repair
revision is deactivated only after both the traffic command and this final
verification succeed. No repair command assigns traffic implicitly or
explicitly.

Do not blindly retry either command with the same `ROLLOUT_ID`: an existing
operation-specific suffix is a hard stop before update, while a stale,
inactive, or mismatched update response cannot be promoted. First inspect the
exact repair and migrate revision names from the failed attempt and their
traffic, active, health, image, and schema-mode state. Record that inspection;
only then generate a new unique `ROLLOUT_ID`. A replacement migration may use
the previously verified `REPAIR_REVISION` argument with the new ID. An
unhealthy or mismatched revision, any failed mode check, or any ambiguous
result stops without changing traffic. A failed or ambiguous repair remains a
hard stop; keep writers quiesced and start a newly identified repair only after
investigating.
