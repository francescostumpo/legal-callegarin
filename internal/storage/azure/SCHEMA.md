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

The reverse-time article key is the server-side listing index. In `migrate` and
`repair` modes, status and `RowKey gt <cursor-boundary>` are pushed into each
bounded Table query, so a logical page does not rescan earlier rows. `compat`
is the Azure default and performs no migration during startup. It reads both
direct-ID legacy rows and current rows, always creates current rows, and moves
a legacy row on update with one conditional same-partition transaction that
also applies slug changes. Because legacy RowKeys do not encode creation time,
compatibility listing scans bounded input pages and retains only the requested
logical page plus look-ahead in memory; it deterministically merges both
layouts newest-first with keyset-safe cursors. Ordinary reads never write.

`migrate` checks the durable marker before the HTTP listener starts. Without a
marker it moves each legacy row with a conditional same-partition add+delete
transaction. It preserves article IDs, metadata, body references, and slug
records. Reconciliation handles committed-but-unacknowledged transactions and
the safe intermediate state where identical old and new rows coexist. A
conflicting target is left untouched and fails startup. Only after a complete
pass finds no legacy rows does migration conditionally create
`schema:article-row-key:v1`; a valid marker makes later startup one point read
with no scan. The serving repository is then current-only and O(page).

`repair` is an operator-controlled recovery mode used only after writers are
quiesced. It does not trust the marker as proof of convergence: it forces the
same idempotent scan, leaves the valid marker intact, and switches to the same
current-only repository after success. The marker must never be blindly
deleted. The deployment contract requires all active revisions to run the same
immutable dual-reader/current-writer artifact in `compat` before that artifact
is switched to `migrate`; rollback must remain at or above that image floor.
See [`docs/article-storage-rollout.md`](../../../docs/article-storage-rollout.md).

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
