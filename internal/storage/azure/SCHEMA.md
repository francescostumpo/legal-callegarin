# Azure Storage schema

Storage schema version `1` uses three Azure Tables and one private Blob
container. The default names are `articles`, `contacts`, `sessions`, and
`article-bodies`; integration tests suffix those resource names for isolation.

All Table entities carry `PartitionKey`, `RowKey`, `entityType`, and
`schemaVersion`. Domain ETags are never stored as properties: repositories
return and compare the exact service ETag.

| Table | Partition | Row key | Entity type |
| --- | --- | --- | --- |
| `articles` | `articles` | reverse Unix-nanoseconds plus inverse article ID | `article` |
| `articles` | `articles` | `slug:<normalized-slug>` | `slug` |
| `articles` | `articles` | `schema:article-row-key:v1` | `schemaMigration` |
| `contacts` | `contacts` | reverse Unix-nanoseconds plus contact ID | `contact` |
| `sessions` | `sessions` | lowercase SHA-256 token hash | `session` |

An article row and all of its linked slug reservations change in one
same-partition transaction. Preflight rejects batches above 100 operations or
4 MiB. Published canonical and historical slug rows are permanent; each stores
the current canonical published target so old slugs remain redirects.

The reverse-time article key is the server-side listing index: status and
`RowKey gt <cursor-boundary>` are pushed into each bounded Table query, so a
logical page does not rescan earlier rows. Before the HTTP listener starts,
the explicit migration hook finds legacy rows whose RowKey is the direct
article ID and moves each one with a conditional same-partition add+delete
transaction. It preserves the article ID, metadata, body references, and slug
records. A retry reconciles both committed-but-unacknowledged transactions and
the safe intermediate state where identical old and new rows coexist. A
conflicting target is left untouched and fails startup. Only after a complete
pass finds no remaining legacy rows does the hook conditionally create the
`schema:article-row-key:v1` marker. Marker creation reconciles a duplicate or
committed-but-unacknowledged add. A valid marker makes later startups a single
point read with zero rows scanned; its distinct entity type is excluded from
article listing. After a verified migration, listing accepts current rows
only; ordinary reads never write.

The deployment contract prevents legacy rows from appearing behind a completed
marker: all serving revisions must be dual-reader/current-writer before the
marker-bearing revision starts, and rollback must stay at or above that floor.
An older direct-ID writer must never run concurrently or after marker creation.
If this contract is breached, stop that writer, delete the marker under
operator control, and restart the current revision so the full migration runs
again. This explicit rollout gate keeps normal startup constant-time without
silently accepting late legacy writes.

Article bodies are immutable JSON blobs named
`articles/{articleID}/{version}.json`. Uploads use `If-None-Match: *` and
`Content-Type: application/json`. `BodyRef.BlobName` stores that private blob
name, never a URL. The container is created without a public access option.

Production accepts only the canonical Blob origin in
`AZURE_STORAGE_ACCOUNT_URL` and derives the Table origin by changing `.blob.`
to `.table.`. It authenticates with `DefaultAzureCredential`. A connection
string is accepted only in development/test (including Azurite) and is never
included in returned initialization errors or logs. SDK retries are disabled;
the adapter performs at most three transient-only attempts with a per-attempt
deadline.
