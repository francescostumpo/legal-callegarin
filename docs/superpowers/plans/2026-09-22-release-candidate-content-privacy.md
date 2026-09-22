# Release-candidate Content and Privacy Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace all public development markers with coherent professional and privacy copy, record the materially revised notice version, and track genuinely unknown facts outside the public website.

**Architecture:** Keep public content in the existing Go page catalog and embedded templates. Preserve the contact persistence schema while changing the stored notice-version value and the admin-facing label from consent to acknowledgement. Keep external launch approvals blocked in documentation rather than weakening those gates.

**Tech Stack:** Go 1.26 templates and tests, embedded React/TypeScript administration, Vitest, Node contract tests, Playwright, Markdown operational documentation.

## Global Constraints

- Follow `docs/superpowers/specs/2026-09-22-release-candidate-content-privacy-design.md`.
- Never invent professional-register, forum, registration, qualification, fiscal, tax, digital-domicile, or postal-code facts.
- Never reintroduce the university grade, volunteer experience, or `avvocato associato`.
- Keep public pages free of cookies, analytics, tracking, third-party runtime resources, maps, widgets, and a cookie banner.
- Describe 24 months as review and 30 days as recovery before eligibility for opportunistic purge, never as exact automatic deletion.
- Store notice version exactly as `privacy-v2-2026-09-22`.
- Keep all production launch gates `BLOCKED` until real approver/date/evidence records exist.
- Do not modify infrastructure, storage schema, authentication, rate limits, contact state transitions, article behavior, or production configuration.

## File Structure

- `internal/web/public/pages.go`: canonical page and complete privacy copy.
- `internal/webassets/templates/pages/contact.html`: first-layer notice.
- `internal/webassets/templates/partials/footer.html`: confirmed identity only.
- `internal/web/public/contact.go`: stored notice version.
- `web/admin/src/App.tsx`: acknowledgement wording.
- Runtime tests: `internal/web/public/renderer_test.go`, `internal/web/public/contact_test.go`, `web/admin/src/App.test.tsx`, `e2e/public.spec.ts`.
- `docs/release-2.md`: non-public register of unresolved facts and approvals.
- `docs/launch-readiness.md` and `scripts/launch-readiness-docs.test.mjs`: truthful blocked gates and their contract.

---

### Task 1: Publish runtime content and privacy semantics

**Files:**

- Modify: `internal/web/public/pages.go`
- Modify: `internal/webassets/templates/pages/contact.html`
- Modify: `internal/webassets/templates/partials/footer.html`
- Modify: `internal/web/public/contact.go`
- Modify: `web/admin/src/App.tsx`
- Test: `internal/web/public/renderer_test.go`
- Test: `internal/web/public/contact_test.go`
- Test: `web/admin/src/App.test.tsx`
- Test: `e2e/public.spec.ts`

**Interfaces:**

- Consumes: existing `PageData`, `ContentSection`, `StudioContact`, form submission, and `Contact.consentVersion` API field.
- Produces: marker-free public pages, stored version `privacy-v2-2026-09-22`, and admin label `Presa visione informativa`.

**Acceptance criteria:** complete and first-layer notices agree; footer contains only confirmed facts; version and admin semantics are correct; unrelated runtime behavior does not change.

**Prohibited changes:** do not rename persistence/API fields; add automatic deletion, providers, cookies, banners, or third-party resources; or mark the candidate notice legally approved.

- [ ] **Step 1: Write failing public copy tests**

Extend `TestApprovedPublicCopyAndContactsRenderWithoutForbiddenClaims` in `renderer_test.go`. Require the `/approccio` sentences:

```go
"Le informazioni condivise con lo Studio sono trattate con riservatezza e con attenzione alla loro pertinenza rispetto alla richiesta.",
"Ogni comunicazione viene gestita nel rispetto degli obblighi professionali e della normativa applicabile.",
```

Require `/privacy-cookie-policy` to contain:

```go
[]string{
    "Titolare del trattamento", "Avv. Alessandro Callegarin",
    "Dati trattati e finalità", "articolo 6, paragrafo 1, lettera b)",
    "articolo 6, paragrafo 1, lettera f)", "Conferimento dei dati",
    "Destinatari e trasferimenti", "Italy North", "Conservazione",
    "24 mesi", "30 giorni", "Diritti dell’interessato",
    "Garante per la protezione dei dati personali",
    "Nessun processo decisionale automatizzato",
    "Cookie e strumenti di tracciamento", "otto ore",
}
```

For every public route assert absence of:

```go
[]string{
    "DATO DA CONFERMARE", "DA VALIDARE CON IL PROFESSIONISTA",
    "103/110", "volontariato", "avvocato associato",
    "partita IVA", "codice fiscale", "numero di iscrizione",
}
```

Require footer text `Avv. Alessandro Callegarin — Gallarate (VA)`.

- [ ] **Step 2: Write failing form, version, admin, and browser tests**

In `contact_test.go`, replace marker expectations with:

```go
[]string{
    "Avv. Alessandro Callegarin", "callegarinale@gmail.com",
    "alessandro.callegarin@busto.pecavvocati.it", "misure precontrattuali",
    "articolo 6, paragrafo 1, lettera b)", "telefono è facoltativo",
    "fornitori tecnici", "Italy North", "24 mesi",
    "non comporta cancellazione automatica", "30 giorni",
    "cancellazione definitiva durante una successiva operazione",
    "accesso, rettifica, cancellazione, limitazione, opposizione e portabilità",
    "Garante per la protezione dei dati personali",
}
```

Assert both development markers are absent and submissions store:

```go
if stored.ConsentVersion != "privacy-v2-2026-09-22" {
    t.Fatalf("notice version = %q", stored.ConsentVersion)
}
```

Add to the existing contact-detail test in `App.test.tsx`:

```tsx
expect(screen.getByText("Presa visione informativa")).toBeInTheDocument()
expect(screen.queryByText("Consenso privacy")).not.toBeInTheDocument()
```

Extend the privacy Playwright test with headings `Titolare del trattamento`, `Conservazione`, `Diritti dell’interessato`, and `Cookie e strumenti di tracciamento`, retaining zero-cookie/zero-banner assertions.

- [ ] **Step 3: Prove the new tests fail**

Run:

```bash
go test ./internal/web/public -count=1
npm test -- --run src/App.test.tsx
npx playwright test e2e/public.spec.ts --project=chromium --grep "privacy page"
```

Expected: assertions fail on the old markers, version, label, and missing headings. If restricted sandbox socket policy blocks Playwright, record that exact failure and rerun through the approved external path after implementation.

- [ ] **Step 4: Implement canonical page and policy copy**

In `pages.go`, replace the approach marker with the Step 1 sentences and remove the dead article-catalog `Sections` marker. Set privacy lead to:

```text
Questa informativa descrive come sono trattati i dati personali durante la navigazione e quando viene inviata una richiesta di contatto.
```

Implement `ContentSection` entries with these exact facts:

```go
[]ContentSection{
    {Heading: "Titolare del trattamento", Paragraphs: []string{
        "Il titolare del trattamento è l’Avv. Alessandro Callegarin, con studio in Via Borghi 8, Gallarate (VA).",
        "Per richieste relative alla protezione dei dati è possibile scrivere a callegarinale@gmail.com oppure alla PEC alessandro.callegarin@busto.pecavvocati.it.",
    }},
    {Heading: "Dati trattati e finalità", Paragraphs: []string{
        "Il modulo raccoglie nome e cognome, indirizzo email, eventuale numero di telefono, contenuto del messaggio, versione dell’informativa presa in visione e dati temporali e operativi necessari alla gestione della richiesta.",
        "I dati sono trattati per ricevere e rispondere alla richiesta e per svolgere misure precontrattuali richieste dall’interessato ai sensi dell’articolo 6, paragrafo 1, lettera b), del GDPR. I dati tecnici strettamente necessari alla sicurezza del sito, alla prevenzione degli abusi e alla diagnosi degli errori sono trattati sulla base del legittimo interesse del titolare ai sensi dell’articolo 6, paragrafo 1, lettera f), del GDPR.",
        "L’invio del modulo non costituisce conferimento di incarico. Non devono essere inviati documenti o dati particolari o giudiziari non necessari in questa fase di primo contatto.",
    }},
    {Heading: "Conferimento dei dati", Paragraphs: []string{
        "L’uso del modulo è facoltativo ed è sempre possibile contattare direttamente lo Studio. Nome, email, messaggio e conferma di lettura dell’informativa sono necessari per gestire la richiesta tramite il modulo; il numero di telefono è facoltativo. In mancanza dei dati necessari non sarà possibile inviare la richiesta attraverso il modulo.",
    }},
    {Heading: "Destinatari e trasferimenti", Paragraphs: []string{
        "I dati sono accessibili al titolare e alle persone espressamente autorizzate per le attività tecniche necessarie. Possono essere trattati da fornitori dell’infrastruttura e della manutenzione, vincolati dalle condizioni contrattuali e dagli obblighi applicabili in materia di protezione dei dati.",
        "L’archiviazione applicativa è configurata su Microsoft Azure nell’Unione europea, regione Italy North. Eventuali trattamenti che comportino trasferimenti verso Paesi esterni allo Spazio economico europeo devono avvenire nel rispetto delle garanzie previste dal GDPR e degli accordi applicabili con il fornitore.",
    }},
    {Heading: "Conservazione", Paragraphs: []string{
        "Le richieste sono conservate per il tempo necessario a rispondere e gestire il contatto e sono segnalate per una revisione 24 mesi dopo la raccolta. La data di revisione non comporta cancellazione automatica.",
        "Una richiesta di cancellazione manuale apre una finestra tecnica di recupero di 30 giorni. Dopo tale periodo il record diventa idoneo alla cancellazione definitiva durante una successiva operazione della console di amministrazione. Un periodo ulteriore è ammesso soltanto quando necessario per obblighi di legge o per accertare, esercitare o difendere un diritto.",
    }},
    {Heading: "Diritti dell’interessato", Paragraphs: []string{
        "Nei casi previsti dal GDPR è possibile chiedere accesso, rettifica, cancellazione, limitazione, opposizione e portabilità dei dati scrivendo ai recapiti indicati. È inoltre possibile proporre reclamo al Garante per la protezione dei dati personali.",
        "Nessun processo decisionale automatizzato o attività di profilazione è svolto sui dati inviati.",
    }},
    {Heading: "Cookie e strumenti di tracciamento", Paragraphs: []string{
        "Le pagine pubbliche non impostano cookie, non usano strumenti di analisi o profilazione e non caricano risorse da servizi di terze parti. Non è quindi mostrato alcun banner cookie.",
        "La sola area di amministrazione utilizza, dopo l’accesso, un cookie tecnico di sessione strettamente necessario, protetto con Secure, HttpOnly e SameSite=Strict, revocabile con il logout e con durata massima di otto ore.",
        "L’eventuale introduzione futura di cookie non tecnici o strumenti di tracciamento richiederà una nuova valutazione legale e tecnica prima dell’attivazione.",
    }},
}
```

- [ ] **Step 5: Implement footer, first layer, version, and admin semantics**

Replace the footer marker with:

```html
<p class="site-footer__muted">Avv. Alessandro Callegarin — Gallarate (VA)</p>
```

Remove the contact-page development notice. Replace the first-layer copy with concise paragraphs using the same policy facts. Its deletion sentence must be:

```text
La revisione dopo 24 mesi non comporta cancellazione automatica. Una cancellazione manuale resta recuperabile per 30 giorni; trascorso tale periodo, il record diventa idoneo alla cancellazione definitiva durante una successiva operazione della console di amministrazione.
```

Keep the acknowledgement checkbox and link. Set in `contact.go`:

```go
contactConsentVersion = "privacy-v2-2026-09-22"
```

Replace only the admin `<dt>` text:

```tsx
<dt>Presa visione informativa</dt>
```

- [ ] **Step 6: Run focused validation and commit Task 1**

Run:

```bash
gofmt -w internal/web/public/pages.go internal/web/public/contact.go internal/web/public/renderer_test.go internal/web/public/contact_test.go
npx prettier --write internal/webassets/templates/pages/contact.html internal/webassets/templates/partials/footer.html web/admin/src/App.tsx web/admin/src/App.test.tsx e2e/public.spec.ts
go test ./internal/web/public ./internal/app -count=1
npm test -- --run src/App.test.tsx
npm run typecheck
npx playwright test e2e/public.spec.ts --project=chromium --grep "privacy page"
rg -n 'DATO DA CONFERMARE|DA VALIDARE CON IL PROFESSIONISTA' internal/web/public internal/webassets/templates
```

Expected: tests pass; final `rg` exits 1 with no output.

Commit:

```bash
git add internal/web/public/pages.go internal/web/public/contact.go internal/web/public/renderer_test.go internal/web/public/contact_test.go internal/webassets/templates/pages/contact.html internal/webassets/templates/partials/footer.html web/admin/src/App.tsx web/admin/src/App.test.tsx e2e/public.spec.ts
git commit -m "feat: publish release candidate privacy content"
```

---

### Task 2: Track Release 2 facts and preserve launch gates

**Files:**

- Create: `docs/release-2.md`
- Modify: `docs/launch-readiness.md`
- Test: `scripts/launch-readiness-docs.test.mjs`

**Interfaces:**

- Consumes: marker removal and notice version from Task 1.
- Produces: durable Release 2 register and contract tests distinguishing a complete candidate from production approval.

**Acceptance criteria:** all unknown facts/external approvals are tracked; launch readiness references the new version and zero-marker expectation; every gate remains blocked.

**Prohibited changes:** do not fill approver/date/evidence, production, fiscal, registration, or recovery fields with invented values; do not rewrite historical approved specs.

- [ ] **Step 1: Write failing documentation-contract tests**

Update `launch-readiness-docs.test.mjs` to load `docs/release-2.md`, expect `privacy-v2-2026-09-22`, assert runtime marker absence, require candidate zero-marker wording plus final-artifact evidence, require every Release 2 category, and assert every launch gate remains `BLOCKED`.

Use these marker assertions:

```js
assert.doesNotMatch(runtimePublicCopy, /DATO DA CONFERMARE/)
assert.doesNotMatch(runtimePublicCopy, /DA VALIDARE CON IL PROFESSIONISTA/)
```

Require Release 2 terms for `Ordine`, `numero e data di iscrizione`, `partita IVA`, `codice fiscale`, `domicilio digitale`, `approvazione dell’avvocato`, `responsabili`, `trasferimenti`, `dominio`, `DNS`, `TLS`, `articoli`, `RPO`, `RTO`, `restore`, `accessibilità`, and `sign-off`.

- [ ] **Step 2: Prove the documentation test fails**

Run `node --test scripts/launch-readiness-docs.test.mjs`.

Expected: FAIL because the Release 2 register and new version/gate wording do not exist.

- [ ] **Step 3: Create the Release 2 register**

Create `docs/release-2.md` with this exact structure:

```markdown
# Registro Release 2

Questo documento raccoglie dati e verifiche non inventati né pubblicati nella
release candidate. Ogni voce resta `DA ACQUISIRE` finché non dispone di valore,
approvatore, data ed evidenza verificabile.

## Dati professionali e fiscali

- Ordine professionale, foro, numero e data di iscrizione: `DA ACQUISIRE`.
- Partita IVA, codice fiscale, domicilio digitale e CAP dello studio se dovuti:
  `DA ACQUISIRE`.

## Approvazioni professionali e privacy

- Approvazione dell’avvocato per identità, biografia, aree, immagini, disclaimer,
  recapiti, informativa e canali privacy: `DA ACQUISIRE`.
- Conferma degli accordi con responsabili, categorie effettive di destinatari e
  valutazione degli eventuali trasferimenti: `DA ACQUISIRE`.

## Dominio e produzione

- Dominio, policy apex/`www`, DNS, TLS, canonical e indicizzazione:
  `DA ACQUISIRE`.
- Parametri e segreti Azure/GHCR/OIDC e prova dell’artefatto reale:
  `DA ACQUISIRE`.
- Articoli iniziali approvati, qualora il lancio non utilizzi il catalogo vuoto:
  `DA ACQUISIRE`.

## Continuità, accessibilità e rilascio

- Proprietari e valori RPO/RTO, retention del backup, restore drill e regole di
  cancellazione dei contatti: `DA ACQUISIRE`.
- Audit di accessibilità e revisione responsive dell’artefatto di produzione:
  `DA ACQUISIRE`.
- Sign-off finale dell’avvocato e dell’operatore: `DA ACQUISIRE`.
```

`DA ACQUISIRE` is allowed only in this non-runtime register.

- [ ] **Step 4: Align launch readiness without approving it**

In `launch-readiness.md`, reference `privacy-v2-2026-09-22`; state that candidate runtime-source scan must contain zero public markers while the exact release artifact still requires evidence; link `release-2.md`; leave every gate `BLOCKED` with empty approver/date/evidence fields.

- [ ] **Step 5: Validate and commit Task 2**

Run:

```bash
npx prettier --write docs/release-2.md docs/launch-readiness.md scripts/launch-readiness-docs.test.mjs
node --test scripts/launch-readiness-docs.test.mjs
npm run format:check
git diff --check
```

Expected: all commands exit 0.

Commit:

```bash
git add docs/release-2.md docs/launch-readiness.md scripts/launch-readiness-docs.test.mjs
git commit -m "docs: track release two launch inputs"
```

---

## Final Aggregate Validation and Delivery

After both task reviews are `APPROVED`:

1. Review `git diff origin/main...HEAD` against the approved design and plan.
2. Run `make check` with isolated writable caches.
3. Run the complete Playwright suite through its approved execution path.
4. Inspect home, approach, articles, contacts, privacy, and admin contact detail at mobile, tablet, and desktop widths.
5. Verify no public `Set-Cookie`, runtime third-party request, or cookie banner.
6. Verify a preview contact stores `privacy-v2-2026-09-22` and admin says `Presa visione informativa`.
7. Push the reviewed commits to `codex/website-v1` and update Pull Request #1.
8. Replace the port-8080 preview using the reversible-container procedure and retain the prior stopped container until health, content, login, and dashboard checks pass.
