# Studio Legale Callegarin Website Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` to execute this plan one task at a time. Use `superpowers:test-driven-development` for every code-changing task, `superpowers:systematic-debugging` for failures, and `superpowers:verification-before-completion` before each hand-off.

**Goal:** Build and deploy the approved privacy-first, mobile-first Italian website and single-administrator editorial console for Studio Legale Alessandro Callegarin as one Go executable with an embedded React admin bundle.

**Architecture:** Go owns all public HTML rendering, HTTP routing, security middleware, domain services, caching, Azure Table/Blob access, and embedded assets. React is build-time-only and owns `/admin`; its compiled files are embedded in the Go binary. Azure Container Apps runs one scale-to-zero container; Azure Table Storage stores metadata, contacts, and sessions, while private Blob Storage stores immutable article bodies. Public pages set no cookies and load no third-party resources.

**Tech Stack:** Go 1.27.1, standard `net/http` and `html/template`, Bluemonday allowlist sanitization, React 19.3, TypeScript, Vite 8.1, Vitest 5, TipTap, Azure SDK for Go, Azure Storage/Azurite, Docker, Bicep, GitHub Actions, GHCR, Playwright Test, and the official Playwright agent CLI.

**Approved design:** `docs/superpowers/specs/2026-09-11-studio-legale-callegarin-website-design.md`

**Approved locality input:** The user explicitly supplied `Gallarate, provincia di Varese` and requested local/provincial/regional positioning. This locality may be used; street address, bar registration, telephone, email, PEC, business identifiers, and office hours remain unapproved.

## Global Constraints

- Follow `AGENTS.md`: Sol retains specification decisions; delegate exactly one bounded task at a time to `luna_implementer`; then delegate the same task to `luna_task_reviewer`; route every actionable finding back until `APPROVED`.
- Execute tasks sequentially. A later task may depend only on approved earlier tasks.
- Start execution in an isolated `codex/` worktree or branch using `superpowers:using-git-worktrees`; do not implement directly on `main`.
- Keep public pages server-rendered. Do not turn the public site into a React SPA or add a public framework/runtime; the small first-party accessibility controller for mobile navigation is permitted.
- Never add analytics, advertising pixels, remote fonts, remote images, maps, video embeds, chat widgets, social widgets, or a public cookie-consent banner.
- The only browser cookie is the strictly necessary admin session cookie, set and consumed by admin/auth handlers. Its `__Host-` security prefix requires `Path=/`, so the browser may attach it to public requests; public handlers must ignore it, never vary/cache by it, and never set another cookie.
- Do not add uploads. Article covers must come only from the committed curated image library.
- Do not invent lawyer-specific facts. Local fixtures must be visibly marked synthetic. Production readiness remains blocked until the lawyer supplies and approves identity, professional, contact, privacy, and legal-notice content.
- Target WCAG 2.2 AA, semantic HTML, keyboard access, visible focus, reduced-motion support, and touch targets of at least 44 CSS pixels.
- Preserve the one-process runtime and initial `minReplicas: 0`, `maxReplicas: 1` deployment.
- Every mutation using persisted metadata must use optimistic concurrency. A stale `ETag` returns HTTP `409` with a machine-readable error.
- Run `go fmt`, frontend formatting, unit tests, and the task-specific validation before handing work to review. No task may claim completion from stale results.
- Do not commit generated Playwright reports, local browser profiles, `.env` files, credentials, Azure outputs containing secrets, or build caches.

## Fixed Application Limits

These values are part of the first-release contract; do not choose different values inside individual tasks.

| Concern | Limit |
|---|---:|
| Contact name | 2–120 Unicode characters after trim |
| Contact email | 254 characters, syntactically valid address |
| Contact phone | optional, at most 40 characters |
| Contact message | 20–2,000 Unicode characters after trim |
| Contact form encoded body | 16 KiB |
| Contact minimum completion time / signed-token lifetime | 3 seconds / 2 hours |
| Contact submit throttle | capacity 5 per coarse client key, refill 1 every 3 minutes, at most 5,000 live buckets |
| Article title / summary / slug | 5–160 / 20–320 / 3–100 Unicode or normalized ASCII characters as applicable |
| Article editor document | 512 KiB JSON, depth 32, 5,000 nodes, link 2,048 characters |
| Admin JSON request | 640 KiB; login form body 8 KiB |
| Login throttles | capacity 5 per client and 5 per normalized username, refill 1 every 3 minutes, at most 5,000 live keys each |
| Session | 8-hour absolute expiry; no idle extension |
| List pagination | default 25, maximum 100 |
| Public render cache | 256 entries, 32 MiB copied bytes, 15-minute TTL |
| HTTP timeouts | read header 5 s, read 15 s, write 30 s, idle 60 s, shutdown drain 20 s |
| Storage operation deadline | 5 s per call, at most 3 attempts with capped jittered backoff |
| Contact review/recovery | review due 24 months after submission; read/archive/restore actions never extend it; purge only 30 days after explicit deletion schedule |
| Trusted proxy parsing | development/test use `RemoteAddr`; production accepts `X-Forwarded-For` only with `TRUSTED_PROXY_HOPS=1` and removes exactly the trusted ingress hop from the right |
| Image derivatives | 1600×900 landscape and 900×1125 card crop, AVIF and WebP, at most 300 KiB each |

## Planned Repository Map

Create this structure incrementally; do not create empty speculative packages before the task that owns them.

```text
.
├── .github/workflows/
│   ├── ci.yml
│   └── deploy.yml
├── cmd/
│   ├── adminhash/main.go
│   └── web/main.go
├── docs/
│   ├── operations.md
│   └── superpowers/{plans,specs}/
├── e2e/
│   ├── admin.spec.ts
│   ├── public.spec.ts
│   └── visual.spec.ts
├── infra/
│   ├── custom-domain.bicep
│   ├── main.bicep
│   ├── modules/{container-app,cost,identity,observability,platform,storage}.bicep
│   └── main.example.bicepparam
├── internal/
│   ├── app/app.go
│   ├── articles/{model,repository,service}.go
│   ├── auth/{password,service,session}.go
│   ├── config/config.go
│   ├── contacts/{model,repository,service}.go
│   ├── storage/
│   │   ├── azure/{articles,bodies,contacts,sessions}.go
│   │   ├── contracttest/{articles,bodies,contacts,sessions}.go
│   │   └── memory/{articles,bodies,contacts,sessions}.go
│   ├── web/
│   │   ├── adminapi/{articles,auth,contacts,dashboard,errors}.go
│   │   ├── middleware/{csrf,headers,logging,recovery,ratelimit,requestid}.go
│   │   ├── public/{articles,cache,contacts,pages,renderer,seo}.go
│   │   └── server.go
│   └── webassets/
│       ├── admin/.keep
│       ├── admin/dist/                 # generated by Vite, not committed
│       ├── covers/.keep
│       ├── covers/**/*.{avif,webp}
│       ├── embed.go
│       ├── public/fonts/*
│       ├── public/{nav.js,site.css}
│       └── templates/{layouts,pages,partials}/*.html
├── scripts/{bootstrap-github-oidc.sh,verify-assets.mjs}
├── web/admin/
│   ├── src/{api,components,features,styles,test}/
│   ├── index.html
│   ├── package.json
│   ├── tsconfig.json
│   ├── vite.config.ts
│   └── vitest.config.ts
├── .dockerignore
├── .editorconfig
├── .gitignore
├── Dockerfile
├── Makefile
├── compose.test.yaml
├── go.mod
├── go.sum
├── package.json
├── package-lock.json
├── playwright.config.ts
└── README.md
```

## Shared Contracts

All tasks must converge on these public contracts. A task may add private helpers but may not change these shapes without Sol revising the specification and downstream task inputs.

```go
// internal/articles/model.go
type Status string

const (
	StatusDraft     Status = "draft"
	StatusPublished Status = "published"
	StatusWithdrawn Status = "withdrawn"
)

type BodyRef struct {
	BlobName string    `json:"blobName"`
	Version  string    `json:"version"`
	SavedAt  time.Time `json:"savedAt"`
}

type Article struct {
	ID               string
	Slug             string
	Title            string
	Summary          string
	Area             string
	CoverID          string
	Status           Status
	DraftBody        *BodyRef
	PublishedBody    *BodyRef
	FirstPublishedAt *time.Time
	LastPublishedAt  *time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
	DeletedAt        *time.Time
	ETag             string
}

type Body struct {
	SchemaVersion int             `json:"schemaVersion"`
	Document      json.RawMessage `json:"document"`
	HTML          string          `json:"html"`
	PlainText     string          `json:"plainText"`
}

type MetadataRepository interface {
	Create(context.Context, Article) (Article, error)
	Get(context.Context, string) (Article, error)
	GetBySlug(context.Context, string) (Article, error)
	List(context.Context, ListOptions) (ArticlePage, error)
	Update(context.Context, Article, string) (Article, error)
}

type ListOptions struct {
	Status *Status
	Cursor string
	Limit  int
}

type ArticlePage struct {
	Items      []Article
	NextCursor string
}

type BodyStore interface {
	Put(context.Context, string, Body) (BodyRef, error)
	Get(context.Context, BodyRef) (Body, error)
	Delete(context.Context, BodyRef) error
}
```

```go
// internal/contacts/model.go
type State string

const (
	StateNew      State = "new"
	StateRead     State = "read"
	StateArchived State = "archived"
)

type Contact struct {
	ID                string
	Name              string
	Email             string
	Phone             string
	Message           string
	ConsentVersion    string
	PrivacyAcceptedAt time.Time
	State             State
	CreatedAt         time.Time
	UpdatedAt         time.Time
	ReadAt            *time.Time
	ArchivedAt        *time.Time
	ReviewDueAt       time.Time
	DeletionDueAt     *time.Time
	ETag              string
}

type ListOptions struct {
	State           *State
	Query           string
	Cursor          string
	Limit           int
	RetentionReview bool
}

type ContactPage struct {
	Items      []Contact
	NextCursor string
}

type Repository interface {
	Create(context.Context, Contact) (Contact, error)
	Get(context.Context, string) (Contact, error)
	List(context.Context, ListOptions) (ContactPage, error)
	Update(context.Context, Contact, string) (Contact, error)
	Delete(context.Context, string, string) error
}
```

```go
// internal/auth/session.go
type Session struct {
	TokenHash string
	Username  string
	CredentialVersion string
	CreatedAt time.Time
	ExpiresAt time.Time
	RevokedAt *time.Time
	ETag      string
}

type SessionRepository interface {
	Create(context.Context, Session) error
	Get(context.Context, string) (Session, error)
	Revoke(context.Context, string, string, time.Time) error
	DeleteExpired(context.Context, time.Time) (int, error)
}
```

Standard JSON errors from `/api/admin/**`:

```json
{
  "error": {
    "code": "conflict",
    "message": "La risorsa è stata modificata. Ricarica e riprova.",
    "requestId": "01J...",
    "fields": {}
  }
}
```

## Task 1: Establish the Reproducible Monolith Skeleton

**Boundary:** Toolchain, build graph, validated configuration, embedded asset plumbing, and a minimal process only. No domain behavior, styled pages, persistence, auth, or Azure resources.

**Inputs:** Approved design sections 2, 6, and 15; planned repository map above.

**Acceptance criteria:**

- Go 1.27.1 is declared and `cmd/web` starts one HTTP server.
- Node 24 is declared in `engines`; React admin builds into `internal/webassets/admin`.
- `go build ./cmd/web` succeeds even before the full admin build because `.keep` is embedded.
- Configuration rejects missing production secrets and accepts safe local-memory defaults only when `APP_ENV=development` or isolated `APP_ENV=test`.
- `make check` has one stable entry point for formatting, vet, static analysis, Go tests, TypeScript checks, frontend tests, and frontend build.

**Validation:** `make check && go build ./cmd/web`

**Prohibited changes:** No business models, no page designs, no network calls, no credentials, no Docker/Bicep/CI.

### Steps

1. Create `internal/config/config_test.go` first. Table-test development and test defaults, invalid environment values, production rejection of memory storage, missing production `PUBLIC_BASE_URL`, missing Azure account URL, missing `ADMIN_USERNAME`, missing `ADMIN_PASSWORD_HASH`, and session-key length below 32 bytes. Confirm failure with `go test ./internal/config` before implementation.
2. Add `go.mod`, `.editorconfig`, `.gitignore`, and `internal/config/config.go`. Expose only:

   ```go
   type Config struct {
       Environment, HTTPAddress, PublicBaseURL, StorageMode, AzureAccountURL string
       AdminUsername, AdminPasswordHash string
       SessionKey []byte
   }
   func Load(getenv func(string) string) (Config, error)
   ```

   Decode `SESSION_KEY_BASE64`; never accept a literal fallback outside development. Make `go test ./internal/config` pass.
3. Add `internal/webassets/embed.go`, `internal/webassets/admin/.keep`, `internal/webassets/covers/.keep`, and `internal/webassets/public/site.css` with only a harmless root token so embedding compiles:

   ```go
   //go:embed all:admin all:covers all:public all:templates
   var Files embed.FS
   ```

   Add the smallest valid base template under `internal/webassets/templates/layouts/base.html`.
4. Add `internal/app/app.go` with dependency injection via an `Options` struct, and `cmd/web/main.go` with signal-aware shutdown. The root route may return `503 application not initialized`; `/health/live` must return `200` while the process is alive.
5. Scaffold `web/admin` as an npm workspace with React, TypeScript, Vite, Vitest, Testing Library, and a single smoke-tested `App` that renders `Console di amministrazione`. Keep one root `package-lock.json`. Configure Vite output as `../../internal/webassets/admin/dist`, preserve the parent `.keep`, and set Vite base to `/admin/` so emitted assets resolve below `/admin/assets/`.
6. Add root npm workspace scripts for `web/admin`, declare a pinned Staticcheck tool dependency, and add a `Makefile` whose `build` target builds React before Go and whose `check` target runs a fail-on-difference `gofmt` check, `go vet ./...`, `go tool staticcheck ./...`, `go test ./...`, `npm ci`, `npm run typecheck`, `npm test -- --run`, and `npm run build` in deterministic order. Add toolchain/setup instructions to `README.md`.
7. Run validation fresh, inspect `git diff --check`, and hand off with changed files and command output. Reviewer verifies the runtime remains a single Go process and returns `APPROVED` or concrete findings.

## Task 2: Implement Domain Models, Services, and In-Memory Contract Adapters

**Boundary:** Article/contact/session domain contracts, deterministic service logic, typed errors, clock/ID abstractions, and in-memory adapters. No HTTP, HTML, React, Azure, or styling.

**Inputs:** Task 1; shared contracts; approved design sections 7–9.

**Acceptance criteria:**

- Domain validation covers slugs, statuses, required fields, timestamps, and allowed transitions.
- Article publication preserves the live published body while a later draft is edited.
- Contact transitions allow `new -> read -> archived -> read`, schedule deletion explicitly, and never delete just because 24 months elapsed.
- Memory adapters obey optimistic concurrency and share reusable contract tests with later Azure adapters.
- Tests use injected clocks/IDs and do not sleep.

**Validation:** `go test -race ./internal/articles ./internal/contacts ./internal/auth ./internal/storage/memory ./internal/storage/contracttest`

**Prohibited changes:** No routes, templates, admin UI, Azure SDK, filesystem database, SQLite, or automatic retention deletion.

### Steps

1. Write failing model tests in `internal/articles/model_test.go` and `internal/contacts/model_test.go`. Cover valid/invalid slug normalization, fixed field limits, every status transition, published-body preservation, state timestamps, explicit deletion scheduling/cancellation, and 24-month review calculation that is not extended by read/archive/restore actions.
2. Implement models and typed errors (`ErrNotFound`, `ErrConflict`, `ErrValidation`, `ErrSlugTaken`, `ErrInvalidTransition`) in the owning packages. Use lowercase ASCII slugs with hyphens; reject rather than silently transliterate ambiguous input after normalization.
3. Write reusable test helpers under the deliberately non-production `internal/storage/contracttest` package. They accept factories for article metadata, body, contact, and session repositories and cover CRUD, pagination, stable reverse-chronological order, bounded case-insensitive contact search, slug uniqueness, ETags, session revocation, and session expiry cleanup. Memory and Azure adapter test files import these helpers; no application package may import `contracttest`.
4. Implement mutex-protected memory adapters. Copy byte slices and `json.RawMessage` on read/write so callers cannot mutate stored values. Increment opaque ETags on every update.
5. Write failing service tests for these commands before service code:

   ```go
   type ArticleService interface {
       CreateDraft(context.Context, DraftInput) (Article, error)
       SaveDraft(context.Context, string, DraftInput, string) (Article, error)
       Publish(context.Context, string, string) (Article, error)
       Withdraw(context.Context, string, string) (Article, error)
       GetPreview(context.Context, string) (ArticleWithBody, error)
   }

   type ContactService interface {
       Submit(context.Context, Submission) (Contact, error)
       Open(context.Context, string, string) (Contact, error)
       Archive(context.Context, string, string) (Contact, error)
       Restore(context.Context, string, string) (Contact, error)
       ScheduleDeletion(context.Context, string, string) (Contact, error)
       CancelDeletion(context.Context, string, string) (Contact, error)
       PurgeDue(context.Context, time.Time) (int, error)
   }
   ```

6. Implement services with injected `Clock` and `IDGenerator`. Saving a draft writes the new immutable body first, swaps metadata with ETag protection, and best-effort deletes the old unreferenced draft; publishing points `PublishedBody` to the saved draft without deleting it. Contact submission alone fixes `ReviewDueAt = CreatedAt + 24 months`; administrative state changes do not extend it.
7. Run all contract and race tests. Reviewer checks transition completeness, aliasing, concurrency behavior, and absence of hidden time/global state.

## Task 3: Build the Public Visual System and Static Information Architecture

**Boundary:** Committed no-human image library, design tokens, templates, responsive navigation, and all static public routes. No article database reads, contact submission, admin UI, or Azure.

**Inputs:** Task 2; approved design sections 3–6; attached approved mockup.

**Acceptance criteria:**

- All static routes in the approved information architecture return complete semantic HTML from Go templates.
- The visual system matches the mockup direction: charcoal/ivory/copper, editorial serif display, restrained sans body, layered architectural composition, no people.
- Exactly 12 editorial image IDs are committed with responsive AVIF/WebP derivatives and described in an asset manifest; none contains a person, hand, face, readable third-party text, logo, gavel, scales, handshake, robe, or generic courthouse composition.
- Mobile navigation is keyboard-usable without JavaScript; desktop navigation does not overflow.
- No public response sets a cookie or contacts a third-party host.
- All lawyer-specific fields use unmistakable synthetic development copy such as `DATO DA CONFERMARE`, never invented facts.

**Validation:** `go test ./internal/web/public ./internal/webassets && make build && git diff --check`

**Prohibited changes:** No stock-photo hotlinks, no remote font service, no human imagery, no cookie banner, no client-side public application framework, no production contact facts.

### Steps

1. Invoke the `imagegen` skill before creating raster assets. Generate and inspect these 12 dark editorial, object/architecture-only compositions: `hero-architecture`, `approach-library`, `family-objects`, `succession-seal`, `contracts-pen`, `debt-ledger`, `damages-road`, `property-key`, `criminal-threshold`, `tax-ledger`, `article-notebook`, and `contact-entrance`. Avoid all legal-stock clichés named above. Produce consistent landscape/card derivatives in AVIF and WebP, strip metadata, and keep each served derivative below the agreed performance budget (target 200 KB; hard maximum 300 KB).
2. Add `internal/webassets/covers/manifest.json` with stable ID, derivative filenames, Italian alt text, width, height, aspect ratio, and usage role. Add `docs/assets.md` documenting generation/provenance and the no-human/no-cliché review. Add a locked build-time Sharp verification script that reads actual image metadata and fails unless every derivative exists, dimensions match, IDs are unique, files are under 300 KB, both formats exist, and no unlisted file exists.
3. Write failing renderer/router tests for `/`, `/profilo`, `/approccio`, `/aree-di-attivita`, `/aree-di-attivita/famiglia-e-persone`, `/aree-di-attivita/successioni-e-donazioni`, `/aree-di-attivita/obbligazioni-e-contratti`, `/aree-di-attivita/recupero-crediti`, `/aree-di-attivita/risarcimento-danni`, `/aree-di-attivita/diritti-reali`, `/aree-di-attivita/diritto-penale`, `/aree-di-attivita/diritto-tributario`, `/sentenze-e-riflessioni`, `/contatti`, and `/privacy-cookie-policy`. Assert title, canonical URL, one `h1`, landmarks, skip link, and absence of `Set-Cookie`. Assert the home sections follow the approved order and locality copy says only `Gallarate e provincia di Varese` until further details are approved.
4. Implement `internal/web/public/renderer.go`, `pages.go`, route registration, partials, and layouts. Use an explicit `PageData` type; never pass untyped maps into templates. Parse all templates at startup and fail startup on a parse error.
5. Add self-hosted variable WOFF2 files for Fraunces (display) and Source Sans 3 (body/UI), commit their OFL license texts, document sources/versions in `docs/assets.md`, and define a safe local system fallback stack. No runtime font request may leave the origin.
6. Implement design tokens and mobile-first layout in `site.css`: base at 360 px, one tablet breakpoint around 48 rem, one desktop breakpoint around 72 rem, fluid `clamp()` typography, container queries only where progressive enhancement is safe, visible focus, reduced motion, and print-safe article typography.
7. Add a progressively enhanced `<details>` mobile navigation plus a tiny first-party `nav.js` controller for a visible close action, focus placement/return, Escape handling, and outside navigation. The menu must remain usable without JavaScript. Add correct current-page semantics and desktop navigation at the larger breakpoint; test HTML behavior and keyboard order.
8. Run validation and manually inspect generated HTML for synthetic-content markers. Reviewer compares the templates and CSS to the approved mockup and checks for accidental personal data or external requests.

## Task 4: Deliver Public Articles, Shared Preview Rendering, SEO, and In-Memory HTML Cache

**Boundary:** Public article listing/detail, shared renderer used by preview, sitemap/robots metadata, conditional requests, and bounded render cache. No admin mutation API or React editor.

**Inputs:** Tasks 2–3; approved design sections 6.3, 8, 10, and 13.

**Acceptance criteria:**

- Only published articles appear publicly; withdrawn/draft articles return `404` from public routes.
- `/sentenze-e-riflessioni` paginates deterministically and `/sentenze-e-riflessioni/{slug}` renders the current published body.
- The preview rendering function uses the exact same article template with `Preview=true`; it is not yet exposed without auth.
- Cache is bounded to 256 entries and 32 MiB of rendered bytes, concurrency-safe, suppresses duplicate fills, uses a 15-minute TTL, and invalidates article detail, index, home, sitemap, and area pages after publication/withdrawal.
- HTML supports `ETag`/`If-None-Match`; fingerprinted static assets get immutable cache headers.
- Sitemap, robots, canonical tags, Open Graph, structured `LegalService`/`Article` JSON-LD, and missing-image fallbacks are valid.

**Validation:** `go test -race ./internal/web/public ./internal/articles && go test ./internal/web/public -run 'Cache|SEO|Preview' -count=50`

**Prohibited changes:** No cache for preview/admin/forms/errors, no Redis/CDN, no unpublished-body exposure, no duplicate template for preview.

### Steps

1. Write failing handler tests using memory repositories for empty lists, pagination, published/draft/withdrawn visibility, slug lookup, canonical redirects from historical slugs, missing cover, and conditional `304` responses.
2. Implement article handlers and typed view models. Render sanitized stored HTML as `template.HTML` only at the final template boundary; document the invariant beside that conversion.
3. Extract `RenderArticle(io.Writer, ArticlePageData) error` and test byte-equivalent public/preview article bodies when chrome flags are normalized. Add a preview ribbon through a layout flag, not a separate body template.
4. Write failing cache tests with an injected clock. Implement a sharded or single-lock LRU with maximum 256 entries and 32 MiB of copied response bytes, 15-minute TTL, duplicate-fill suppression, key namespaces, and explicit invalidation methods. Do not spawn cleanup goroutines; prune on access/write.
5. Add an `ArticleEvents` hook from the service so publish, withdraw, cover/title/slug changes invalidate exact dependent keys. Test that draft-only saves do not evict the public article.
6. Add `sitemap.xml`, `robots.txt`, metadata, `LegalService`, `Person`, and `Article` JSON-LD, and `ETag` generation from rendered bytes. Article pages show author/publication/update dates, an informational-content disclaimer, and bidirectional practice-area links. Reject a `PUBLIC_BASE_URL` whose scheme/host would produce unsafe canonicals. Add golden tests for critical template metadata and semantic HTML, admin/preview robots exclusion, withdrawn sitemap removal, and intentional `404`/`410` behavior without draft leakage.
7. Add a storage-outage test proving an already cached public response can still be served while any write or uncached read reports failure. Reviewer checks there is no dynamic data leak across cached pages and that preview never enters the cache.
8. Run repeated race tests and hand the task to review.

## Task 5: Implement the Privacy-First Contact Intake and Retention UX

**Boundary:** Public form, anti-abuse controls, persistence through `ContactService`, success/error UX, retention disclosure, and admin-ready contact data. No admin display yet and no email delivery.

**Inputs:** Tasks 2–4; approved design sections 9 and 12.

**Acceptance criteria:**

- Form stores name, email, optional phone, message, privacy-acceptance timestamp, state `new`, and server timestamp.
- The form states the 24-month operational retention policy in reassuring plain Italian and links to privacy information.
- The form states that submission does not create a professional engagement and asks the visitor not to send documents or unnecessary sensitive information.
- Submission uses POST/Redirect/GET and refresh cannot duplicate a successful request.
- No public cookie is created. Anti-abuse uses honeypot, signed form timestamp, minimum completion time, body/field limits, same-origin checks, and bounded in-memory IP token buckets.
- Logs and error pages never contain form values. Invalid fields are accessible and preserve safe user input.
- No contact is automatically deleted at 24 months; the service only exposes a review flag until explicit admin deletion scheduling.

**Validation:** `go test -race ./internal/web/public ./internal/contacts -run 'Contact|Form|Rate|Retention'`

**Prohibited changes:** No CAPTCHA, external email provider, attachments, analytics, public session/CSRF cookie, automated 24-month purge.

### Steps

1. Write failing unit/handler tests for valid submission, consent absent, malformed email, field limits, honeypot hit, too-fast timestamp, bad signature, cross-origin POST, oversized body, rate exhaustion/recovery, storage failure, PRG redirect, and no `Set-Cookie`.
2. Implement an HMAC-signed hidden form timestamp using the application session key with a distinct derivation label. Accept only tokens generated within a bounded window and compare signatures in constant time.
3. Implement an in-memory token bucket keyed by a privacy-preserving truncated/hash representation of the client address. Bound the map and expire idle buckets on access; trust forwarded headers only when `TRUSTED_PROXY=true`.
4. Implement contact templates with inline accessible validation summary, per-field errors, privacy checkbox, direct contact fields rendered as `DATO DA CONFERMARE`, and PRG success state via a short signed query token rather than a cookie.
5. Add privacy copy that explicitly covers controller, purpose, legal basis, recipients/processors, Azure EU region, limited access, expected 24-month review horizon, rights/contact channel, and the 30-day recovery window after manual deletion request. Mark legal wording `DA VALIDARE CON IL PROFESSIONISTA` in development content.
6. Add redaction tests for structured logs and panic/error paths. Reviewer verifies that body values never appear in logs and no public cookie/header regression exists.

## Task 6: Add Azure Table and Blob Persistence with Azurite Contract Tests

**Boundary:** Azure SDK adapters, storage naming/serialization, optimistic concurrency, retry/timeout policy, Azurite integration tests, and application storage wiring. No Bicep or production deployment.

**Inputs:** Tasks 1–5; approved design section 7 and resilience requirements.

**Acceptance criteria:**

- Table records use documented partition/row keys: article metadata and `slug:<normalized-slug>` lookups share table/partition `articles`; contacts use table/partition `contacts`; sessions use table/partition `sessions`.
- Slug reservation and metadata mutation use same-partition transactional batches where atomicity is required.
- Article bodies are private JSON blobs under `articles/{articleID}/{version}.json`; body refs never expose a public URL.
- Azure ETags map losslessly to domain ETags; `412` becomes `ErrConflict`, `404` becomes `ErrNotFound`, and timeouts remain distinguishable.
- Adapter contract suites pass against both memory and Azurite.
- Managed Identity via `DefaultAzureCredential` is the only production authentication path; connection strings are accepted only in development/test.

**Validation:** `docker compose -f compose.test.yaml up -d azurite && go test -race -tags=integration ./internal/storage/azure ./internal/storage/contracttest; docker compose -f compose.test.yaml down`

**Prohibited changes:** No account keys in source, no public containers, no SQL/Cosmos/SQLite, no network-dependent unit tests, no infinite retry loops.

### Steps

1. Add Azure SDK dependencies and `compose.test.yaml` pinned to a specific Azurite image digest. Extend config tests for `AZURE_STORAGE_ACCOUNT_URL` and development-only `AZURE_STORAGE_CONNECTION_STRING`.
2. Write failing serialization tests for every Table entity and Blob body envelope. Assert round-trip equality, UTC timestamps, schema version, row/partition keys, and absence of plaintext session tokens.
3. Implement shared Azure client construction with per-operation contexts, bounded exponential retry for transient statuses, and user-agent identification. Never retry validation, conflict, authentication, or not-found results.
4. Implement article metadata and slug entities in the same `articles` partition so conditional slug reservation and metadata mutation can use one transactional batch. On a post-publication slug change, reserve the new slug, update metadata, and keep the old lookup as a permanent redirect record.
5. Implement body store with immutable create semantics (`If-None-Match: *`), content type `application/json`, private access, and best-effort removal of superseded unreferenced drafts.
6. Implement contacts and sessions, including bounded in-Go contact search over the low-volume result set, review filtering, session revocation state, expiry cleanup, and ETag guards. Store only SHA-256 session token hashes.
7. Wire `STORAGE_MODE=memory|azure` in `internal/app`. Readiness fails when Azure dependencies are unavailable; liveness remains process-only.
8. Run memory and Azurite contracts. Reviewer checks Table transaction constraints, serialization compatibility, retry bounds, and secret handling.

## Task 7: Secure the Internal Login, Server-Side Sessions, CSRF, and Recovery Tool

**Boundary:** Password hashing/verification, server-rendered login, logout/session API, protected-route middleware, session cookie, CSRF, login throttling, and offline hash command. No React admin feature screens beyond the existing stub.

**Inputs:** Tasks 2 and 6; approved design section 11.

**Acceptance criteria:**

- Password verification uses Argon2id with parameters stored in the encoded hash; only the hash is supplied through secrets.
- Session tokens contain 256 bits from `crypto/rand`; only SHA-256 token hashes are persisted.
- Cookie is named `__Host-callegarin_admin`, has `Path=/`, `Secure`, `HttpOnly`, `SameSite=Strict`, no `Domain`, and an eight-hour maximum age.
- Authenticated mutations require a session-derived CSRF token and trusted origin. Logout revokes the server session.
- Login responses do not reveal whether username or password was wrong; separate bounded per-IP and per-normalized-username throttles are tested.
- Each session records a non-secret credential-version digest derived from the configured password hash, so replacing the hash invalidates all prior sessions even before expired rows are cleaned up.
- `cmd/adminhash` reads a password twice from a terminal/stdin without accepting it on the command line and prints only the PHC hash.

**Validation:** `go test -race ./internal/auth ./internal/web/adminapi ./internal/web/middleware && go run ./cmd/adminhash </dev/null` (the final command must fail safely without echoing a secret)

**Prohibited changes:** No plaintext password persistence/logging, no JWT, no localStorage/sessionStorage auth token, no reset email, no external IdP, no MFA, no long-lived refresh token.

### Steps

1. Write password tests with a fixed valid PHC fixture plus invalid, malformed, oversized, and parameter-limit cases. Implement Argon2id using OWASP-compatible memory/time/parallelism values and cap decoded parameters before allocating memory.
2. Write session service tests for creation, rotation when an already authenticated client logs in again, lookup, eight-hour expiry, logout, expired-session rejection, password-hash replacement invalidation through `CredentialVersion`, randomness/read failure, and constant-time comparisons. Implement token hashing and repository calls.
3. Write handler/middleware tests for the server-rendered `GET/POST /admin/login`, login success/failure, uniform error, secure cookie attributes, protected route, expired session, logout revocation, bad origin, missing/bad CSRF, safe methods, and rate limiting.
4. Implement `GET/POST /admin/login`, `GET /api/admin/session`, and `DELETE /api/admin/session`. The login POST uses a short-lived HMAC-signed hidden form token plus strict origin validation, then redirects to `/admin` on success. Return a CSRF token from authenticated session GET by HMAC-deriving it from the raw cookie token and the session key; never persist or expose the raw session token in JSON.
5. Implement middleware order: request ID, recovery, security headers, access log, body limit, auth, origin, CSRF, handler. Ensure auth failures are `Cache-Control: no-store`; use separate bounded login buckets for coarse client address and normalized username.
6. Implement `cmd/adminhash` with hidden terminal input when interactive and safe refusal on empty/mismatched input. Document the Azure operator recovery sequence without placing a sample real hash in the repository.
7. Reviewer attempts cookie scope, CSRF, timing/error disclosure, PHC resource-exhaustion, and logout bypass cases before approval.

## Task 8: Build the React Admin Shell, Dashboard, and Persistent Contact Console

**Boundary:** Embedded React application, login screen, authenticated shell, dashboard notification query, paginated contact list/detail, read/archive/restore/delete-schedule actions. No article editor yet.

**Inputs:** Tasks 5–7; approved design contact/admin decisions.

**Acceptance criteria:**

- `/admin` and nested client routes load the embedded React app only after auth; an unauthenticated request redirects to the server-rendered `/admin/login`. Public routes remain server-rendered.
- The shell fetches `GET /api/admin/session`, keeps CSRF only in memory, and redirects to login after `401`.
- Dashboard queries contact counts on open and shows a non-push notification when `new > 0`.
- Opening a new contact changes it to read; archive is reversible; explicit delete schedules 30-day recovery and clearly communicates that state.
- Opening the dashboard opportunistically purges only contacts whose administrator-scheduled 30-day recovery window has elapsed; reaching the general 24-month review date never triggers purge.
- Contacts remain searchable with a bounded case-insensitive name/email query, paginated, and filterable by `new/read/archived/deletion scheduled/retention review`; an empty state is useful.
- An administrator may cancel a scheduled deletion at any point before purge and recover the contact to its prior non-deletion state.
- UI is responsive and keyboard accessible at 360 px through desktop.

**Validation:** `go test -race ./internal/web/adminapi ./internal/contacts && npm run typecheck && npm test -- --run && npm run build`

**Prohibited changes:** No push notifications, polling while console is closed, email sending, contact attachments, hard delete from the normal UI, token persistence in Web Storage.

### Steps

1. Write failing Go handler tests for dashboard counts, opportunistic purge after an explicit deletion window, no purge at the ordinary 24-month review date, bounded query search, paginated/filter contact list, explicit mark-read transition, archive, restore, delete schedule/cancel, ETags, `409`, and all auth/CSRF requirements. A detail GET is side-effect free.
2. Implement DTOs and routes:

   ```text
   GET    /api/admin/dashboard
   GET    /api/admin/contacts?q=&state=&cursor=&retentionReview=
   GET    /api/admin/contacts/{id}
   POST   /api/admin/contacts/{id}/read
   POST   /api/admin/contacts/{id}/archive
   POST   /api/admin/contacts/{id}/restore
   POST   /api/admin/contacts/{id}/schedule-deletion
   POST   /api/admin/contacts/{id}/cancel-deletion
   ```

   Require `If-Match` on mutations and return `ETag` headers on detail responses.
3. Write failing React tests for bootstrap auth, dashboard badge, empty/error/loading states, debounced bounded search, open-to-read transition through the explicit POST, archive/restore, deletion confirmation/cancellation, stale conflict, keyboard navigation, and mobile sidebar/dialog behavior. Login rendering/submit behavior stays covered by Go handler tests because it is not a React route.
4. Implement a typed `fetchJSON` client that sends credentials same-origin, attaches CSRF and `If-Match`, parses standard errors, and performs one auth-state transition on `401`; do not add a state-management library.
5. Implement semantic React Router routes `/admin`, `/admin/contatti`, and `/admin/contatti/:id`. Use CSS modules or locally scoped classes with shared design tokens; do not import public-page React components because none should exist.
6. Add `adminapi` SPA fallback that serves embedded `index.html` only below `/admin`; asset misses must remain `404` and API misses JSON `404`.
7. Build, then inspect the emitted bundle for remote URLs and source maps. Production build must omit source maps. Reviewer exercises the notification lifecycle and verifies contact history remains visible.

## Task 9: Implement the Article Console, Visual Editor, Cover Library, and Exact Preview

**Boundary:** Article admin API and UI, TipTap document handling/sanitization, lifecycle actions, curated cover selection, unsaved-change protection, and authenticated preview iframe.

**Inputs:** Tasks 3–4 and 7–8; approved design sections 6.3 and 8.

**Acceptance criteria:**

- Admin can create, list, filter, edit, publish, withdraw, and republish articles without scheduling.
- Visual editor supports only paragraphs, H2/H3, ordered/unordered lists, bold, italic, links, and block quotes.
- Server independently validates TipTap JSON, renders/sanitizes HTML with an allowlist, normalizes safe links, and rejects unsupported nodes/marks.
- Cover is optional and selectable only from the 12-item committed library; no upload control or upload endpoint exists.
- Explicit Save controls persistence; navigation with unsaved changes requires confirmation. No autosave.
- Save/publish conflicts and transient API failures preserve the current editor document in React memory and offer reload/retry without silently overwriting either version.
- Editing a published article creates/updates a draft while the old published version remains live until Publish.
- Preview loads `/admin/preview/articles/{id}` in a same-origin iframe, uses the saved draft and exact public template, and sets `no-store` plus `X-Robots-Tag: noindex, nofollow`.

**Validation:** `go test -race ./internal/articles ./internal/web/adminapi ./internal/web/public && npm run typecheck && npm test -- --run && npm run build`

**Prohibited changes:** No raw HTML editing, uploads, scheduled publishing, autosave, full revision-history UI, unpublished content in public routes, second preview template.

### Steps

1. Write failing sanitizer/document tests for every allowed node/mark plus script, style, image, iframe, event attribute, `javascript:`/`data:` URL, malformed JSON, excessive depth, excessive link length, and pasted unsupported content.
2. Implement a bounded TipTap JSON validator and renderer in `internal/articles`. Sanitize rendered output using an explicit allowlist; links permit only `https`, `http`, `mailto`, and relative paths, adding safe `rel` values to external targets.
3. Write failing API tests for:

   ```text
   GET    /api/admin/articles?status=&cursor=
   POST   /api/admin/articles
   GET    /api/admin/articles/{id}
   PUT    /api/admin/articles/{id}/draft
   POST   /api/admin/articles/{id}/publish
   POST   /api/admin/articles/{id}/withdraw
   GET    /api/admin/covers
   GET    /admin/preview/articles/{id}
   ```

   Cover create/save/publish/withdraw, slug conflict, invalid transition, stale ETag, published-body preservation, auth, CSRF, cache invalidation, and preview headers.
4. Implement API handlers as thin adapters over `ArticleService`. Mutations require `If-Match`; create returns `201`, validation `422`, stale/slug conflicts `409`, invalid transitions `409`, and missing resources `404`.
5. Write failing React tests for article list states, new draft, restricted toolbar, cover picker/no-upload assertion, explicit save, dirty indicator, browser/router navigation guard, publish, withdraw, conflict/transient-failure recovery without losing editor input, and preview refresh after save.
6. Implement TipTap with only `StarterKit` subfeatures in scope plus safe Link. Paste handling strips unsupported formatting. Keep editor document state in React; do not mirror body HTML in localStorage.
7. Implement the preview panel iframe with a resize selector for 360, 768, and desktop widths. The preview route authenticates via the admin cookie, fetches the last saved draft, calls the shared public renderer, permits framing only by same origin, and is excluded from cache.
8. Reviewer publishes an article, edits a new draft, proves the live page is unchanged, previews the draft, republishes, and proves cache/public output updates atomically.

## Task 10: Harden HTTP Behavior, Errors, Health, Observability, and the Container Image

**Boundary:** Cross-cutting HTTP production behavior, structured/privacy-safe logging, readiness/liveness, graceful shutdown, Docker image, runtime user, and local production smoke test.

**Inputs:** Tasks 1–9; approved design sections 11, 14, and 15.

**Acceptance criteria:**

- Public/admin security headers include strict CSP, HSTS in production, no sniffing, referrer policy, permissions policy, and route-appropriate frame policy; non-canonical `www`/apex traffic permanently redirects to the configured canonical host.
- All handlers have server timeouts, request-size limits, panic recovery, request IDs, and graceful shutdown.
- Logs are structured JSON and omit passwords, cookies, CSRF, contact bodies, names, email, phone, and article draft bodies.
- `/health/live` is process-only; `/health/ready` checks initialized templates/config and storage with a tight timeout.
- Container is multi-stage, runs as a numeric non-root user, contains no Node/toolchains/source/secrets, exposes 8080, and supports scale-to-zero cold start.
- Error pages match the public design and admin API errors keep the shared JSON contract.
- Public error pages retain navigation and approved direct contact channels; contact form/admin editor state survives recoverable `409`/`5xx` failures in memory until the user retries, reloads, or discards it.

**Validation:** `make check && docker build -t legal-callegarin:local . && docker run --rm -d --name legal-callegarin-smoke -p 18080:8080 -e APP_ENV=development legal-callegarin:local` followed by HTTP smoke checks and `docker stop legal-callegarin-smoke`.

**Prohibited changes:** No secrets in image layers, no root runtime user, no debug endpoints, no PII logs, no blanket `unsafe-inline` scripts, no separate frontend server.

### Steps

1. Write failing middleware tests for request IDs, panic recovery, JSON/HTML negotiation, all headers, HSTS environment behavior, frame policy difference for preview, redaction, body limits, slow handler timeout, and trusted proxy parsing.
2. Implement explicit CSP values. Public pages permit only the same-origin navigation controller; admin permits only same-origin bundled scripts/styles and same-origin framing for preview. Avoid inline script/style so nonces are unnecessary.
3. Add `slog` structured logging with an allowlist of fields: timestamp, level, request ID, method, normalized route, status, duration, coarse client hash, and error code. Never log raw URL queries on contact/admin routes.
4. Implement error templates for `404`, `409`, `422`, `429`, and `500`, plus storage-unavailable `503`. Exercise them with handler tests that assert navigation/direct contact channels remain present and no internal detail leaks.
5. Add server read-header/read/write/idle timeouts, maximum headers, signal shutdown, canonical-host redirect, and readiness dependency probes. Retry storage only inside the bounded adapter policy.
6. Add `.dockerignore` and a pinned multi-stage `Dockerfile`: Node build stage, Go build stage with `CGO_ENABLED=0`, and distroless/non-root final stage. Embed build version/commit through `-ldflags`; expose it only in structured startup logs, not a public diagnostic endpoint.
7. Perform container inspection (`docker history --no-trunc`, `docker inspect`) and local smoke tests for public, admin, live, ready, cookies, and remote requests. Reviewer checks cold-start path and final image contents.

## Task 11: Describe the Minimal Azure Infrastructure in Bicep

**Boundary:** Declarative production resources, identities/RBAC, storage protections, Container Apps scale/ingress/secrets, logging, and two-phase custom-domain binding. No actual deployment.

**Inputs:** Task 10; approved design sections 15–16; Italy North, GHCR private, registrar-managed DNS.

**Acceptance criteria:**

- A resource-group-scope `main.bicep` deploys the resource set into exactly one operator-created Italy North resource group, using deterministic names derived from a short project/environment prefix.
- Storage is Standard ZRS, HTTPS-only, TLS 1.2+, shared-key access disabled in production, public blob access disabled, Blob versioning/soft delete enabled, and private article-body container/table resources created.
- Container App uses Consumption, external HTTPS ingress on 8080, `minReplicas: 0`, `maxReplicas: 1`, CPU/memory sized minimally, health probes, and GHCR private credentials as secrets.
- The Container App's system-assigned managed identity receives only Storage Blob Data Contributor and Storage Table Data Contributor at the storage-account scope.
- Secure parameters cover GHCR token, admin password hash, and session key; outputs never expose them.
- Custom apex and `www` binding is a separate post-DNS Bicep deployment using free managed certificates.
- `what-if`/lint output is clean and parameter example contains no secrets.

**Validation:** after exporting the secure environment variables named by `infra/main.example.bicepparam`, run `az bicep build --file infra/main.bicep && az bicep build --file infra/custom-domain.bicep && az deployment group what-if --resource-group <approved-disposable-rg> --template-file infra/main.bicep --parameters infra/main.example.bicepparam`.

**Prohibited changes:** No ACR, Azure DNS zone, Key Vault, SQL/Cosmos, Redis, CDN/WAF, private endpoints, dedicated workload profile, permanent staging app, deployment, or real secret values.

### Steps

1. Write the resource-group-scope `infra/main.bicep` parameter/output contract first and compile it to expose syntax/type failures. Parameters include location default `italynorth`, project prefix, environment, image reference, GHCR username/token, admin username/hash, session key, public base URL, log retention, and optional monthly budget threshold. Document the one-time operator creation of the Italy North resource group.
2. Implement storage module with tables `articles`, `contacts`, `sessions` and private blob container `article-bodies`. Add delete retention, container delete retention, blob versioning, lifecycle cleanup for unreferenced/deleted draft blobs, and CORS disabled.
3. Enable the Container App system-assigned identity and implement its storage role assignments with built-in role definition IDs for Blob Data Contributor and Table Data Contributor. Do not create a user-assigned runtime identity or assign Owner/Contributor.
4. Implement observability with bounded Log Analytics retention and daily cap suitable for low traffic. Add actionable storage/log-volume alerts; document an operator-created subscription budget filtered to this resource group when the deployer lacks subscription budget permissions. Do not add Application Insights or client analytics.
5. Implement Container Apps environment/app module, GHCR registry secret, app secrets/env references, revision mode, ingress, health probes, concurrency rule, and scale range. Mark every secret parameter `@secure()`.
6. Implement `custom-domain.bicep` to bind apex and `www` only after the operator has created the exact registrar DNS records printed by non-secret outputs. Document the two-phase deployment and certificate validation in `docs/operations.md`.
7. Add `main.example.bicepparam` using Bicep `readEnvironmentVariable()` for `GHCR_TOKEN`, `ADMIN_PASSWORD_HASH`, and `SESSION_KEY_BASE64`, with ordinary non-secret example values for the other parameters. Never place illustrative secret literals in the file or pass secrets as visible CLI arguments.
8. Run Bicep build and, when an authorized disposable resource group is available, group `what-if`. If Azure access is unavailable, report it as an explicit validation blocker; reviewer may approve code structure but Sol must not claim deploy validation.

## Task 12: Add GHCR Build and Azure OIDC Deployment Workflows

**Boundary:** Pull-request CI, main-branch container publication to private GHCR, Azure OIDC deployment, immutable image references, SBOM/scan, and operator documentation. No production execution.

**Inputs:** Tasks 10–11; GitHub remote `francescostumpo/legal-callegarin`.

**Acceptance criteria:**

- PR CI runs Go race tests, frontend type/test/build, integration tests with Azurite, Bicep build, container build, SBOM generation, and high/critical vulnerability scan; the same pinned scanner/SBOM commands are exposed as local Make targets.
- Main workflow publishes one multi-stage image to private GHCR tagged by commit SHA and deploys by immutable digest; scheduled retention keeps the currently deployed digest and 10 newest versions, then removes older package versions after 30 days even if they still carry obsolete SHA tags.
- Azure login uses GitHub OIDC; no Azure client secret is stored in GitHub.
- A documented idempotent bootstrap creates the Entra application/service principal, a federated credential restricted to `repo:francescostumpo/legal-callegarin:environment:production`, and only the built-in Container Apps Contributor role (`358470bc-b998-42bd-ab17-a7e34c199c0f`) on the one resource group.
- GHCR pull credential is passed only as an Azure secure parameter/Container App secret and never printed.
- Every third-party action is pinned to a full commit SHA with its release tag in a comment.
- Concurrency permits only one production deployment and does not cancel an in-progress production rollout.
- Deployment checks `/health/live`, `/health/ready`, one public page, and absence of public cookies; failure leaves the prior healthy revision available for operator rollback.

**Validation:** `actionlint .github/workflows/*.yml` plus a pull request run; workflow syntax and permissions are manually inspected before merge.

**Prohibited changes:** No `pull_request_target`, no long-lived Azure secret, no floating action tags, no plaintext GHCR token, no mutable `latest` deployment, no automatic custom-domain mutation on every deploy.

### Steps

1. Add failing/static workflow policy checks (script or actionlint config) that reject `pull_request_target`, floating `uses:` refs, `latest` image deploys, and broad default write permissions.
2. Add `make sbom IMAGE=...` and `make scan IMAGE=... SEVERITY=HIGH,CRITICAL` using scanner/generator container images pinned by digest. Implement `.github/workflows/ci.yml` with least-privilege read permissions, pinned toolchain setup, npm cache keyed by lockfile, Go tests/race, frontend checks, Azurite integration service, Bicep compile, Docker build, those SBOM/scan commands, and a high/critical gate.
3. Add `scripts/bootstrap-github-oidc.sh` with `--dry-run` and explicit required inputs. It creates or reuses the Entra app/service principal, configures the exact production-environment federated subject and Azure token-exchange audience, and assigns Container Apps Contributor at the existing resource-group scope. Test argument validation/rendered intent without calling Azure. Document that only an authorized operator may execute the mutating mode.
4. Implement `.github/workflows/deploy.yml` for `main` and manual dispatch. Grant `id-token: write`, `contents: read`, `packages: write`; log in to GHCR with `GITHUB_TOKEN`, publish the SHA tag, resolve its digest, log in to Azure via OIDC, update only the existing Container App image to that digest, and smoke test. Initial or changed infrastructure remains an operator-reviewed Bicep deployment.
5. Add a protected scheduled retention job that discovers the currently deployed digest before deletion, preserves it plus the 10 newest versions, and deletes package versions older than 30 days even when they still carry obsolete SHA tags. Cover selection logic with a dry-run unit/script test and fail closed when Azure/GHCR state cannot be resolved.
6. Add environment protection instructions and required GitHub variables/secrets to `docs/operations.md`: Azure client/tenant/subscription IDs, resource group, Container App name, GHCR username/PAT, admin username/password hash, session key, domain/base URL. The Azure IDs are non-secret variables; the runtime PAT and application secrets remain protected. Explain that the PAT needs only `read:packages` for runtime pulls, and document cost/budget/package-usage alert ownership.
7. Document password recovery: generate a new Argon2id hash locally, update the Container App secret through the approved Azure deployment path, create a new revision, rely on the credential-version mismatch to invalidate existing sessions, clean expired/revoked rows, and verify login. Never include an actual command containing a password literal.
8. Run `actionlint` and local policy checks. Reviewer audits the federated subject, resource-group role scope, workflow permissions, log masking, digest use, retention fail-closed behavior, rollback path, and secret flow.

## Task 13: Add Automated Browser Journeys and Accessibility Assertions

**Boundary:** Playwright Test suite for public/admin behavior across target engines and viewports. No screenshot-baseline acceptance yet.

**Inputs:** Tasks 8–12; approved design verification section.

**Acceptance criteria:**

- E2E starts the production-like Go binary in memory mode with deterministic synthetic fixtures.
- Public journeys cover navigation, practice pages, article list/detail, contact validation/success, privacy page, no public cookies, and no third-party requests.
- Admin journeys cover login failure/success, notification, contact lifecycle, article create/save/preview/publish/edit-draft/republish/withdraw, conflict, logout, and session expiry.
- Chromium, Firefox, and WebKit pass at least the critical smoke suite; viewport suite covers 360x800, 390x844, 768x1024, 1440x900, and 1920x1080.
- Automated assertions detect horizontal overflow, clipped focus, missing accessible names, console errors, failed same-origin requests, and third-party network access.

**Validation:** `npm run e2e -- --project=chromium && npm run e2e:smoke`

**Prohibited changes:** No real lawyer/contact data, no production endpoint tests, no broad visual snapshot churn, no ignoring console/network errors.

### Steps

1. Add root Playwright Test and official `@playwright/cli` development dependencies and lock them. Configure `webServer` to run the built Go binary with development memory storage and a deterministic fixture seed.
2. Write public tests first in `e2e/public.spec.ts`, including an HTTP-level assertion that all public routes omit `Set-Cookie` and a route listener that fails any request whose origin differs from the local base URL.
3. Add a test-only fixture bootstrap behind `APP_ENV=test` and a random per-run secret; it must not compile into a callable production route when `APP_ENV=production`. Prefer direct repository seeding before server start over an HTTP test endpoint.
4. Write admin flows in `e2e/admin.spec.ts`. Obtain credentials only from test environment variables. Assert live published content remains unchanged while a new draft is edited.
5. Add `@axe-core/playwright` checks plus reusable assertions for `document.documentElement.scrollWidth <= innerWidth`, focused element bounding box, minimum interactive target sizes, landmark/heading structure, console errors, and failed response statuses.
6. Configure CI artifacts for traces/screenshots only on failure with short retention. Reviewer inspects at least one trace and checks tests fail when a deliberate overflow/third-party request is temporarily introduced and reverted.

## Task 14: Perform the Required Playwright CLI Visual Review and Release Audit

**Boundary:** Human-visible agent browser inspection, responsive defect fixes, aggregate verification, documentation completion, and production-content readiness checklist. No Azure deployment unless separately authorized.

**Inputs:** Every approved prior task; attached mockup; approved design section 17.2.

**Acceptance criteria:**

- The official Playwright agent CLI is used to inspect—not merely execute tests—at all five required viewport sizes.
- The complete public visual matrix covers home; practice index/detail; empty and populated article index; short and deliberately long article detail; contacts with normal, validation-error, success, and dependency-failure states; privacy; mobile navigation open/closed; and `404`.
- The complete admin visual matrix covers login success/error; dashboard with/without new contacts; contact list/search/filters/detail/archive/recovery/expiry warning; article list; editor with short and long titles/content; cover picker; exact preview; `409`; session expiry; and generic API failure.
- No overflow, clipping, overlap, unreadable contrast, invisible focus, undersized touch targets, abrupt layout shift, broken image, console error, failed asset request, or unexpected third-party request remains.
- Dark editorial style is coherent and balanced across public/admin while admin prioritizes clarity.
- `make check`, Go race suite, Azurite contracts, full Playwright suite, Bicep build, container build/scan, and `git diff --check` all pass freshly.
- `README.md`, `docs/operations.md`, privacy/content checklist, backup/restore procedure, and deployment/runbook match actual behavior.
- Before production launch, an authorized operator rehearses Blob version/soft-delete recovery and application logical-deletion recovery against a disposable Azure Storage account; missing authorization/evidence is a release blocker, never a silently skipped check.
- Production launch remains explicitly blocked until the lawyer approves all real personal/professional/legal content and the operator completes DNS/secrets/deployment checks.

**Validation:** Full command matrix below, followed by aggregate diff review by Sol.

**Prohibited changes:** No silent approval of visual defects, no deleting failing assertions, no invented production data, no deployment or domain mutation without explicit authorization, no cookie banner.

### Steps

1. Start the production-like container locally and install the CLI/browser from the locked project dependency. Confirm commands with `npx playwright cli --help` and use a named session so state does not collide with other projects.
2. For each target viewport, open the site with a CLI config containing exact viewport width/height, run `snapshot`, take named screenshots while also scrolling the full page, traverse the keyboard focus order, and inspect browser console/network output. Store temporary evidence under ignored `artifacts/playwright-cli/`.
3. Inspect the complete public/admin matrices from the acceptance criteria at both 360×800 and 1440×900; additionally inspect representative home, long article, contact, dashboard, contact detail, editor, and preview states at 390×844, 768×1024, and 1920×1080. Use synthetic long titles, long unbroken-safe strings, empty lists, validation errors, storage failure, `409`, `429`, `500`, and `503` states. Verify responsive recovery, not only the happy path.
4. For every defect, create a focused failing test first, return the finding to `luna_implementer`, rerun relevant unit/browser tests, then send the fix to `luna_task_reviewer`. Repeat until `APPROVED`.
5. Run the fresh aggregate matrix:

   ```bash
   make check
   go test -race ./...
   docker compose -f compose.test.yaml up -d azurite
   go test -race -tags=integration ./internal/storage/azure ./internal/storage/contracttest
   docker compose -f compose.test.yaml down
   npm run e2e
   az bicep build --file infra/main.bicep
   az bicep build --file infra/custom-domain.bicep
   docker build -t legal-callegarin:release-candidate .
   make sbom IMAGE=legal-callegarin:release-candidate
   make scan IMAGE=legal-callegarin:release-candidate SEVERITY=HIGH,CRITICAL
   git diff --check
   git status --short
   ```

6. Inspect the aggregate diff for cross-task contract drift, public cookies, external URLs, secret-like strings, PII fixtures, unresolved production-copy markers, uploaded-image paths, cache leaks, and scope additions.
7. In an explicitly authorized disposable Azure Storage account configured like production, create synthetic article/contact records, verify Blob version recovery after overwrite, Blob soft-delete recovery after deletion, and application contact deletion cancellation inside 30 days; capture timestamps/commands/results without secrets or data. Delete the synthetic records/account only within that authorization. If this rehearsal cannot run, mark production release blocked.
8. Finish documentation with exact local setup, test commands, secret generation, Azure bootstrap/what-if/deploy, DNS/certificate sequence, the exercised recovery evidence/process, password recovery, rollback, contact retention/manual deletion, and a content-approval checklist.
9. Sol issues final approval only when all task reviews are `APPROVED`, all available checks pass, and unavailable external checks are explicitly identified. Then use `superpowers:finishing-a-development-branch` to offer merge/PR/cleanup choices.

## Traceability Checklist

Before execution starts, Sol must confirm that every approved decision has an owning task:

| Approved requirement | Owning task(s) |
|---|---|
| Go SSR public site + embedded React admin | 1, 3, 8 |
| Mobile-first dark editorial design, no humans | 3, 14 |
| Eight practice areas and regional positioning | 3 |
| Articles draft/publish/withdraw, no scheduling | 2, 4, 9 |
| Exact preview from saved draft | 4, 9 |
| Curated cover library only, no uploads | 3, 9 |
| Three-field contact form and persistent console history | 2, 5, 8 |
| New/read/archived, retention review, 30-day recovery | 2, 5, 8 |
| Single admin, Argon2id, server session, CSRF, 8h | 7 |
| Azure Table + Blob, managed identity | 6, 11 |
| In-memory HTML cache and invalidation | 4 |
| No analytics/cookie banner/public cookies | 3, 5, 10, 13, 14 |
| Italy North ACA, scale 0–1, custom domain | 11 |
| Private GHCR and GitHub OIDC | 11, 12 |
| Lowest practical operating cost | 10–12 |
| Playwright CLI visual inspection at five viewports | 13, 14 |
| Synthetic data until lawyer content approval | 3, 5, 13, 14 |

## Execution Gate

This plan authorizes implementation scope but not production deployment, DNS mutation, Azure resource creation, GitHub secret changes, or publication of real legal content. Those external mutations require explicit user authorization at the relevant task. Task 11 may compile and run `what-if` only against a disposable resource group the user has approved. Task 12 may create workflow files but must not trigger production. Task 14 may inspect locally but must not deploy.
