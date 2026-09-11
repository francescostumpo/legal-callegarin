# Azure Storage schema

Storage schema version `1` uses three Azure Tables and one private Blob
container. The default names are `articles`, `contacts`, `sessions`, and
`article-bodies`; integration tests suffix those resource names for isolation.

All Table entities carry `PartitionKey`, `RowKey`, `entityType`, and
`schemaVersion`. Domain ETags are never stored as properties: repositories
return and compare the exact service ETag.

| Table | Partition | Row key | Entity type |
| --- | --- | --- | --- |
| `articles` | `articles` | stable article ID | `article` |
| `articles` | `articles` | `slug:<normalized-slug>` | `slug` |
| `contacts` | `contacts` | reverse Unix-nanoseconds plus contact ID | `contact` |
| `sessions` | `sessions` | lowercase SHA-256 token hash | `session` |

An article row and all of its linked slug reservations change in one
same-partition transaction. Preflight rejects batches above 100 operations or
4 MiB. Published canonical and historical slug rows are permanent; each stores
the current canonical published target so old slugs remain redirects.

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
