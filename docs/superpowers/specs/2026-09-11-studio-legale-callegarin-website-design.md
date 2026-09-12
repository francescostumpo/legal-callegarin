# Studio Legale Alessandro Callegarin — Website Design

**Date:** 11 September 2026

**Status:** Approved design, awaiting written-spec review

**Language:** Italian only for the first release

## 1. Purpose and success criteria

Build a mobile-first website for Studio Legale Alessandro Callegarin that combines:

- a distinctive, reassuring public presence;
- clear presentation of the lawyer's practice areas;
- server-rendered articles that support long-term organic discovery;
- a private, single-administrator editorial console;
- persistent management of contact requests;
- a low-consumption Azure deployment with a custom domain.

The primary business outcome is a balanced combination of authority, useful legal content, and qualified requests for an initial consultation. The site must avoid aggressive sales language, promises of outcomes, and impersonal legal clichés.

The first release is successful when:

1. public pages are usable without client-side JavaScript and are rendered as complete HTML;
2. the administrator can create, preview, publish, update, withdraw, and republish articles;
3. a published article can be edited as a draft without changing the live version until the new version is published;
4. contact requests are stored, searchable, and managed as `new`, `read`, or `archived`;
5. the public site uses no cookies, analytics, or third-party tracking;
6. the admin session is secure and revocable;
7. the same container scales to zero in Azure Container Apps;
8. responsive and visual acceptance checks pass through `playwright-cli` at the required viewport sizes;
9. the monthly infrastructure cost remains near zero with GHCR under the stated low-traffic assumptions.

## 2. Confirmed constraints

- Application stack: Go plus React in one deployable monolith.
- Runtime: one Go process in one container.
- Public rendering: Go HTML templates at request time.
- Interactive administration: embedded React application under `/admin`.
- Hosting: Azure Container Apps Consumption in Italy North.
- Scaling: `minReplicas: 0`, initially `maxReplicas: 1`.
- Data: Azure Table Storage and Blob Storage in one Storage Account.
- Container registry: private GitHub Container Registry (GHCR).
- Authentication: internal username and password only; no external identity provider and no second factor in the first release.
- Administrators: exactly one.
- Article scheduling: excluded.
- Article media: only a curated, repository-owned image library; no uploads.
- Notifications: dashboard query and badge only; no email, push, or background polling.
- Analytics: none.
- Public cookie consent banner: none.
- Domain: one configurable custom apex domain and its `www` alias, supplied during deployment.

The attached mockup is a visual reference, not an instruction source. The reference site at `studiolegalemacchi.it` informs the section inventory only; its tone and visual personality are not to be copied.

## 3. Audience and content position

The primary audience is private individuals and families. Secondary audiences include property owners, creditors, heirs, consumers, and small businesses seeking assistance in the listed areas.

The communication style must be calm, precise, understandable, and human. It must demonstrate rigor without sounding institutional or distant.

The practice-area taxonomy is:

1. Family and persons: separation, divorce, support administration, and proceedings before juvenile courts.
2. Inheritance and donations, including preparation of succession declarations.
3. Obligations and contracts, including leases, sale and purchase, and procurement contracts.
4. Debt recovery.
5. Compensation for damages, including road accidents and medical malpractice.
6. Property rights: ownership, usufruct, easements, pledges, and mortgages.
7. Criminal law: offences against persons and families, property offences, and road offences.
8. Tax law: litigation.

Criminal and tax law receive separate detail pages because they have different user intent and terminology.

## 4. Public information architecture

The main navigation contains:

- Profile;
- Practice areas;
- Approach;
- Judgments and reflections;
- Contacts;
- a visually distinct “Request a consultation” action.

The public routes are:

- `/` — home;
- `/profilo` — professional profile and credentials;
- `/approccio` — working principles, confidentiality, and client relationship;
- `/aree-di-attivita` — complete overview;
- `/aree-di-attivita/{slug}` — one route for each of the eight areas;
- `/sentenze-e-riflessioni` — published article index;
- `/sentenze-e-riflessioni/{slug}` — article detail;
- `/contatti` — direct contact details and contact form;
- `/privacy-cookie-policy` — complete privacy and cookie information.

The home page contains, in order:

1. a positioning statement and consultation action;
2. a concise professional introduction;
3. highlighted practice areas with access to the complete list;
4. the working approach;
5. the three latest published articles;
6. a short authored statement;
7. contact details, location, and contact action.

Production publication is blocked until the lawyer supplies and approves the biography, bar and court information, addresses, telephone, email, PEC, business identifiers, office hours, and final legal copy. Development fixtures must be visibly synthetic and must never invent professional facts.

## 5. Visual and responsive direction

The design retains the mockup's editorial elegance while avoiding human imagery and an overly impersonal atmosphere.

### 5.1 Visual system

- near-black charcoal surfaces rather than absolute black;
- warm ivory text;
- restrained copper or bronze accents;
- a high-character serif for display headings;
- a highly legible sans-serif for body copy and controls;
- open-source fonts self-hosted by the application;
- thin borders, generous spacing, and restrained transitions;
- no gavels, scales of justice, handshakes, robes, people, faces, hands, or generic courthouse stock photography.

The curated image library contains 10–12 approved images based on architecture, stone, wood, paper, books, pens, geometry, and natural light. Assets are committed to the repository and built into the application. Each image has an editorial identifier, alternative text, aspect-ratio variants, and optimized AVIF/WebP outputs. The admin selects from this library but cannot upload media.

### 5.2 Responsive behavior

- mobile-first CSS with content-driven breakpoints;
- one-column mobile layouts that progress to two or three columns only when space permits;
- collapsible mobile navigation with a visible close action and focus management;
- minimum 44-by-44-pixel pointer targets;
- no layout-critical hover behavior;
- fluid display type with bounded `clamp()` sizing;
- readable line length and body size at every viewport;
- reduced-motion support;
- visible keyboard focus;
- WCAG 2.2 AA contrast and interaction targets.

## 6. Rendering and application architecture

The application is a modular Go monolith. Node.js is a build-time tool only and is absent from the production runtime.

### 6.1 Runtime components

1. **Public HTTP handlers** load view models and execute Go templates.
2. **Public renderer** owns the shared layout, metadata, navigation, practice-area pages, article pages, and error pages.
3. **Contact service** validates and persists contact requests.
4. **Article service** owns draft and publication transitions.
5. **Authentication service** validates credentials and manages server-side sessions.
6. **Repository interfaces** isolate application logic from Azure Table and Blob SDK details.
7. **Azure repositories** implement the interfaces through `aztables`, `azblob`, and `azidentity`.
8. **HTML cache** stores rendered public responses in memory.
9. **Admin API** exposes same-origin JSON endpoints under `/api/admin`.
10. **React admin application** is compiled to static files and embedded in the Go binary with `go:embed`.

Public and admin code share design tokens and asset outputs, but not presentation components. The public site favors semantic server HTML; React controls the interactive admin workspace.

### 6.2 Route ownership

- Public content routes: Go-rendered HTML.
- Public contact submission: Go endpoint with progressive enhancement.
- `/admin/login`: minimal server-rendered login page.
- `/admin/*`: React application after successful authentication.
- `/admin/preview/articles/{id}`: authenticated Go-rendered preview using the exact public article template.
- `/api/admin/*`: authenticated JSON API.
- `/health/live` and `/health/ready`: platform health probes.

### 6.3 Preview fidelity

The React editor opens the authenticated preview route in an iframe. The preview reads the saved draft and uses the same Go template, typography, asset pipeline, and renderer as the public page. It sends `Cache-Control: no-store`, `X-Robots-Tag: noindex, nofollow`, and a restrictive framing policy that permits only the same-origin admin page.

Preview always reflects the last saved draft. Unsaved editor state is visibly identified and is not silently previewed.

## 7. Data design

The Storage Account uses Standard ZRS in Italy North. ZRS is preferred over LRS because the price difference at this data volume is only a few cents while data remains available across a zone failure.

### 7.1 Tables

#### Articles

An article entity contains:

- `PartitionKey` fixed to `articles`;
- stable article identifier as `RowKey`;
- title, summary, category, slug, cover-library identifier;
- lifecycle state: `draft`, `published`, or `withdrawn`;
- draft blob identifier and version;
- published blob identifier and version;
- created, updated, first-published, and last-published timestamps;
- optimistic concurrency ETag;
- logical deletion metadata where applicable.

Slugs are unique. They may be edited before first publication and become stable afterwards. If a future editorial need changes a published slug, the old route must permanently redirect to the new one.

#### Article slugs

A separate slug entity maps a normalized slug directly to its stable article identifier. Conditional creation enforces uniqueness without scanning the Articles table. When a published slug changes, the previous slug entity becomes a permanent redirect record instead of being reused.

#### Contacts

A contact entity contains:

- `PartitionKey` fixed to `contacts`;
- reverse-time sortable unique `RowKey`;
- name, email, optional telephone, and message;
- consent text version and consent timestamp;
- state: `new`, `read`, or `archived`;
- received, updated, read, archived, and expiry timestamps;
- logical deletion metadata;
- optimistic concurrency ETag.

The expiry timestamp is 24 months after the last meaningful update. Archive does not delete or hide the contact. The admin can search the active 24-month working set and filter by state; the low expected volume permits bounded filtering in Go without a separate search service.

#### Sessions

A session entity contains a hash of a cryptographically random opaque session token, creation and expiry timestamps, revocation state, and minimal security metadata. Raw tokens are never stored server-side.

### 7.2 Blobs

Article bodies are immutable, sanitized version blobs. A draft save writes a new blob first and then updates the article entity pointer using its ETag. Publication atomically updates the metadata pointer to a complete existing blob. A failed metadata update can leave an unreachable blob, which is safe and can be removed by lifecycle cleanup.

Only the current draft and current published version are retained as application-visible versions. Blob platform versioning and 30-day soft delete provide operational recovery; the product does not expose a general revision-history interface.

### 7.3 Credentials and secrets

- The admin username is configuration.
- The admin password is stored only as an Argon2id hash in an Azure Container Apps secret.
- The GHCR read-only token and session-secret material are Container Apps secrets.
- Secret values never appear in Bicep parameters committed to Git or application logs.
- Password recovery is an operator-run Azure procedure that replaces the Argon2id hash and revokes existing sessions; it sends no email.
- The application accesses Table and Blob through a system-assigned Managed Identity with only the required data-contributor roles.

## 8. Article workflow

The admin can:

1. create a draft;
2. enter title, summary, category, cover, and body;
3. save explicitly;
4. open an exact private preview;
5. publish the saved draft;
6. create a new draft from a published article while leaving the current version live;
7. publish the replacement version;
8. withdraw an article from public access;
9. republish a withdrawn article.

The constrained visual editor supports headings, paragraphs, ordered and unordered lists, bold, italic, links, and block quotations. It does not expose raw HTML or Markdown. The backend applies an allow-list sanitizer even if the client already sanitizes the content.

The editor warns before navigation when changes are unsaved. There is no autosave, scheduled publication, approval workflow, multi-author support, or arbitrary embed.

## 9. Contact workflow

The public contact form and direct telephone, email, and PEC details coexist. The form collects only:

- name;
- email;
- optional telephone;
- a brief message;
- explicit acknowledgement of the privacy information.

The form states that submission does not create a professional engagement and asks the user not to include documents or unnecessary sensitive information.

On successful persistence, the user receives an unambiguous confirmation. If storage fails, the site must not claim success; it provides a retry action and the direct contact channels.

Opening the admin dashboard runs one query for the current number of `new` contacts. There is no background polling. Opening a contact changes it from `new` to `read`. Archiving is reversible and the complete archived list remains available.

Contacts approaching or exceeding 24 months are visibly flagged. The first release requires explicit administrator review and does not automatically delete contacts merely because the expiry date is reached. Manual deletion requires confirmation, starts a 30-day recoverable deletion period, and then permits permanent purge. The published privacy information must describe both the operational 24-month retention target and the limited recovery window.

## 10. Cache design

The in-memory cache stores final rendered HTML bytes for public GET routes only.

- Fill strategy: cache-aside on the first request.
- Default TTL: 15 minutes.
- Capacity: bounded by entry count and total approximate bytes.
- Concurrency: duplicate-fill suppression and thread-safe access.
- Invalidations: article detail, article index, home, sitemap, and related practice-area pages after relevant editorial changes.
- Exclusions: drafts, previews, contact submission, admin, authentication, API responses, and errors.

Published writes invalidate affected entries after the durable metadata update succeeds. Cache failure never affects correctness. A cold start begins with an empty cache. With one replica, invalidation is process-consistent; a future move to multiple replicas requires distributed invalidation or a deliberately accepted bounded stale window.

Public responses include ETags. Immutable fingerprinted CSS, JavaScript, fonts, and image assets receive long-lived browser caching; public HTML receives revalidation-oriented caching.

## 11. Authentication and security

- one username and Argon2id password hash;
- constant-time credential comparisons where applicable;
- generic login errors that do not disclose whether the username exists;
- per-IP and per-username in-memory login throttling;
- server-side revocable sessions with eight-hour absolute expiry;
- session cookie configured as `Secure`, `HttpOnly`, `SameSite=Strict`, path-scoped as narrowly as practical;
- session rotation on login and password recovery;
- explicit logout and revocation;
- CSRF tokens for every state-changing admin request;
- request-size, field-length, and content-type limits;
- secure response headers, including CSP and HSTS;
- no secrets, contact fields, article drafts, raw tokens, or credentials in logs;
- least-privilege Managed Identity access;
- no public access to article body blobs or contact data.

The contact form uses a honeypot, minimum plausible completion time, payload limits, and an in-memory token bucket. No CAPTCHA, third-party anti-spam widget, or browser fingerprinting is used.

## 12. Privacy and cookies

The public site sets no cookies and performs no analytics. Therefore, it displays no cookie consent banner.

The only cookie is the strictly necessary technical session cookie used after an administrator signs in. The Privacy & Cookie Policy documents it. No external fonts, maps, videos, chat widgets, social widgets, trackers, or other third-party embeds load on public pages.

The contact form presents a concise first-layer notice and links to the complete information. The complete notice describes the controller, purpose, legal basis, recipients/processors, EU Azure region, 24-month retention target, 30-day deletion-recovery window, rights, and contact method for exercising those rights. Final legal language requires approval by the lawyer before production.

Any future introduction of non-technical cookies or tracking triggers a new privacy assessment and cannot be enabled merely by adding a script.

## 13. SEO and public metadata

- unique title and description for every public page;
- canonical URLs;
- readable, stable slugs;
- Open Graph metadata using curated images;
- structured data for `LegalService`, `Person`, and `Article` where applicable;
- automatically generated XML sitemap containing only canonical published routes;
- robots rules that exclude admin and preview paths;
- `noindex` headers on private and non-public content;
- author, publication date, and update date on articles;
- article-to-practice-area and practice-area-to-article internal linking;
- descriptive alternative text and semantic heading order;
- an informational-content disclaimer on article pages.

Withdrawn content is removed from the sitemap. Permanent slug changes create redirects; withdrawn articles without a replacement return an intentional gone/not-found response rather than leaking draft content.

## 14. Error handling and resilience

- Azure SDK calls use deadlines, bounded retries with jitter, and structured error classification.
- Optimistic concurrency conflicts produce a safe reload/resolve flow rather than silent overwrite.
- Contact persistence is all-or-nothing from the user's perspective.
- A storage outage may be temporarily masked for already cached public HTML, but stale data is never treated as a successful write.
- Public error pages preserve navigation and direct contact details without exposing internal information.
- Admin errors identify the failed action and preserve recoverable editor input.
- Liveness checks only process health; readiness checks required dependencies with tight timeouts.
- Application shutdown drains requests and closes clients cleanly.
- Logs are structured and correlation-based but exclude personal data.

ZRS protects against a zone failure, not operator deletion. Blob versioning, Blob soft delete, application-level logical deletion, confirmation dialogs, and recovery procedures address accidental changes. Recovery behavior must be exercised before launch.

## 15. Azure deployment design

### 15.1 Resources

- one resource group in Italy North;
- one GA Azure Container Apps Consumption environment;
- one Container App with external HTTP ingress;
- one Standard ZRS StorageV2 account containing Table and private Blob services;
- one system-assigned Managed Identity and narrow RBAC assignments;
- one minimally configured Log Analytics workspace;
- two free managed TLS certificates, one for the apex hostname and one for `www`;
- no Azure Container Registry;
- no Azure DNS when the registrar's DNS is sufficient;
- no Dedicated plan, private endpoint, premium planned maintenance, CDN, Front Door, WAF, Redis, SQL database, or Key Vault in the first release.

The apex domain uses the required A and verification records. `www` uses a direct CNAME to the generated Container App hostname and redirects to the selected canonical host. DNS and CAA prerequisites are validated before cutover; managed-certificate issuance then runs after the A/CNAME records point directly to Container Apps, as required by Azure.

### 15.2 Bicep

Bicep files declaratively create the resources, scaling limits, health probes, ingress, identity, storage security, logging retention, role assignments, non-secret configuration, tags, and outputs needed by deployment. Deployments are idempotent and reviewed with Azure `what-if`.

Domain verification and secret-value injection are documented deployment steps when Azure or DNS ownership prevents them from being fully declarative. Secret values are supplied through protected CI environments or operator input, never source-controlled parameter files.

### 15.3 CI/CD

GitHub Actions:

1. runs formatting, static analysis, unit, integration, React, accessibility, and production-build checks;
2. builds a multi-stage, non-root container image;
3. scans dependencies and the final image;
4. pushes an immutable commit-SHA tag to private GHCR;
5. authenticates to Azure using GitHub OIDC rather than a client secret;
6. deploys a new Container Apps revision;
7. runs smoke and health checks;
8. preserves the previous healthy revision for rollback;
9. removes superseded GHCR versions according to a retention rule.

Container Apps stores a GHCR token limited to package read access. Token rotation is an explicit operations procedure. GitHub package budgets and usage alerts are enabled where the account supports them.

## 16. Cost model

The planning scenario assumes 5,000 page views, fewer than 100,000 HTTP requests, 2 GB of Storage data, fewer than 100,000 storage operations, under 10 GB of internet egress, and under 1 GB of technical logs each month.

Estimated monthly retail cost, excluding VAT and domain registration:

| Component | Private GHCR design | ACR Basic comparison |
| --- | ---: | ---: |
| Container Apps Consumption | $0.00 within monthly grant | $0.00 within monthly grant |
| Table Storage | < $0.01 | < $0.01 |
| Blob Storage and operations | $0.04–$0.15 | $0.04–$0.15 |
| Log Analytics | $0.00 within 5 GB grant | $0.00 within 5 GB grant |
| Internet egress | $0.00 within 100 GB grant | $0.00 within 100 GB grant |
| Identity, Bicep, managed TLS | $0.00 | $0.00 |
| Registry | $0.00 expected under current GHCR terms | approximately $5.07 |
| **Estimated total** | **$0.05–$0.30** | **$5.12–$5.40** |

The domain is external and is expected to cost approximately EUR 10–20 per year depending on registrar and TLD.

The estimate assumes the Azure Container Apps subscription-level free grant is available. Cost alerts must detect grant exhaustion, abnormal logging, storage growth, or a registry-policy change. The deployment must not enable Dedicated management, private endpoints, or premium planned maintenance because the current Italy North environment meter is approximately $0.13/hour per applicable feature.

## 17. Verification strategy

### 17.1 Automated checks

- Go formatting, vetting, static analysis, race-enabled tests, and unit tests;
- repository contract tests against an Azure Storage emulator or isolated test account;
- auth, CSRF, session expiry/revocation, rate-limit, sanitization, transition, cache, ETag, and retention tests;
- Go template golden tests for critical metadata and semantic HTML;
- React component and API-integration tests;
- end-to-end Playwright tests for login, article draft/preview/publish/update/withdraw, contact submission, dashboard badge, state transitions, archive, and deletion recovery;
- automated accessibility checks and keyboard-flow tests;
- Bicep lint/build and Azure `what-if` in an authorized environment;
- container build, non-root verification, health probes, and dependency/image scanning.

### 17.2 Required `playwright-cli` visual review

`playwright-cli` must open and visually inspect the running application. Screenshots alone do not replace interaction; the reviewer must also navigate, resize, operate menus, and examine browser console and failed network requests.

Required viewports:

- 360 × 800;
- 390 × 844;
- 768 × 1024;
- 1440 × 900;
- 1920 × 1080.

Required public states:

- home;
- practice-area index and detail;
- empty and populated article index;
- short and deliberately long article detail;
- contacts and form validation;
- mobile navigation open and closed;
- 404 and dependency-failure presentation.

Required admin states:

- login success and error;
- dashboard with and without new contacts;
- editor with short and long titles/content;
- exact article preview;
- contact list, filters, detail, archive, and expiry warning;
- session expiry and API error presentation.

The review fails for horizontal overflow, clipped controls, unexpected wrapping, overlap, unreadable contrast, missing focus, inaccessible touch targets, broken sticky elements, layout shift, console errors, or unexplained failed requests.

## 18. Explicitly out of scope

- multiple administrators or roles;
- external identity providers or TOTP;
- article scheduling or approval workflow;
- autosave and full editorial revision history;
- visitor accounts, comments, newsletter, or CRM integration;
- media upload, attachments, or document intake;
- analytics, advertising, profiling, CAPTCHA, or cookie consent banner;
- third-party embeds;
- email or push notifications;
- multilingual content;
- online booking, payments, or video consultation;
- a separate frontend runtime, Node.js production server, microservices, or distributed cache;
- permanent staging infrastructure in Azure;
- multi-region failover.

## 19. Sources used for infrastructure and privacy decisions

- Azure Container Apps pricing and free grants: <https://azure.microsoft.com/en-us/pricing/details/container-apps/>
- Azure Container Apps custom domains and managed certificates: <https://learn.microsoft.com/en-us/azure/container-apps/custom-domains-managed-certificates>
- Azure Container Apps managed identities: <https://learn.microsoft.com/en-us/azure/container-apps/managed-identity>
- Azure Table Storage overview: <https://learn.microsoft.com/en-us/azure/storage/tables/table-storage-overview>
- Azure Tables SDK for Go: <https://pkg.go.dev/github.com/Azure/azure-sdk-for-go/sdk/data/aztables>
- Azure Blob SDK for Go: <https://pkg.go.dev/github.com/Azure/azure-sdk-for-go/sdk/storage/azblob>
- Azure-hosted Go authentication: <https://learn.microsoft.com/en-us/azure/developer/go/sdk/authentication/system-assigned-managed-identity>
- Bicep overview: <https://learn.microsoft.com/en-us/azure/azure-resource-manager/bicep/overview>
- Azure bandwidth pricing: <https://azure.microsoft.com/en-us/pricing/details/bandwidth/>
- Azure Monitor pricing: <https://azure.microsoft.com/en-us/pricing/details/monitor/>
- GitHub Packages billing: <https://docs.github.com/en/billing/concepts/product-billing/github-packages>
- Garante Privacy cookie FAQ: <https://www.garanteprivacy.it/faq/cookie>
- Garante Privacy fundamental processing principles: <https://www.garanteprivacy.it/home/principi-fondamentali-del-trattamento>
