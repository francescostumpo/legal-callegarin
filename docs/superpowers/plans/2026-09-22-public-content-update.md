# Public Content Update Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Publish the approved professional profile, presentation copy, contact details, and opening hours consistently across the public site without exposing unapproved qualifications or legal claims.

**Architecture:** Keep one immutable `StudioContact` value in the Go public-page model and expose it through `PageData`, including error and contact-form render paths. Render reusable linked contact markup from a first-party template partial, while keeping page-specific presentation copy in the existing catalog and templates. Preserve all unapproved privacy, bar-registration, and fiscal gates as blocked.

**Tech Stack:** Go 1.25 `html/template`, embedded first-party templates, Node test runner, Playwright.

## Global Constraints

- Display phone `0331 792529` with link `tel:+390331792529`.
- Display address `Via Borghi 8, Gallarate (VA)` without an external map link.
- Display email `callegarinale@gmail.com` and PEC `alessandro.callegarin@busto.pecavvocati.it` as `mailto:` links.
- Display hours `Dal lunedì al venerdì, 09:00–12:30 e 15:00–19:00`.
- State only that Alessandro Callegarin graduated in Law from the University of Milan in 2018 and has practised as a lawyer in Gallarate since 2022.
- Never publish the university grade, volunteering, or the wording `avvocato associato`.
- Never add claims of specialization, expertise, outcomes, success rates, or professional seniority.
- Do not change privacy consent version `privacy-v1-2026-09-11` or treat email/PEC as the approved privacy-rights channel.
- Keep bar/order registration, forum, registration number, fiscal data, privacy approval, and production domain blocked and explicit.
- Do not add third-party runtime resources, analytics, cookies, tracking, map embeds, or a cookie banner.

---

## File Structure

- Modify `internal/web/public/pages.go`: define the single contact-data model, attach it to every `PageData`, and replace approved page-catalog placeholders with final copy.
- Modify `internal/web/public/errors.go`: attach the same contact data to error pages.
- Create `internal/webassets/templates/partials/contact-details.html`: reusable linked contact list.
- Modify `internal/web/public/renderer.go`: parse the new partial for every public template.
- Modify `internal/webassets/templates/pages/home.html`: render approved profile, approach, statement, and contact copy.
- Modify `internal/webassets/templates/pages/standard.html`: render the linked contact partial in the no-contact-service route.
- Modify `internal/webassets/templates/pages/contact.html`: replace confirmed contact placeholders and failure-state wording, without approving privacy copy.
- Modify `internal/webassets/templates/pages/error.html`: render confirmed fallback contacts.
- Modify `internal/webassets/templates/partials/footer.html`: render confirmed location/contact data while retaining the unconfirmed professional/fiscal marker.
- Modify `internal/web/public/renderer_test.go`, `internal/web/public/contact_test.go`, and `internal/app/app_test.go`: cover confirmed copy, links, error paths, and forbidden claims.
- Modify `e2e/smoke.spec.ts` and `e2e/public.spec.ts`: expect the approved hero heading.
- Modify `scripts/launch-readiness-docs.test.mjs`: reject the obsolete “future contact form” sentence while continuing to require unresolved legal markers.

---

### Task 1: Publish approved profile and contact content

**Files:**
- Create: `internal/webassets/templates/partials/contact-details.html`
- Modify: `internal/web/public/pages.go`
- Modify: `internal/web/public/errors.go`
- Modify: `internal/web/public/renderer.go`
- Modify: `internal/webassets/templates/pages/home.html`
- Modify: `internal/webassets/templates/pages/standard.html`
- Modify: `internal/webassets/templates/pages/contact.html`
- Modify: `internal/webassets/templates/pages/error.html`
- Modify: `internal/webassets/templates/partials/footer.html`
- Test: `internal/web/public/renderer_test.go`
- Test: `internal/web/public/contact_test.go`
- Test: `internal/app/app_test.go`
- Test: `e2e/smoke.spec.ts`
- Test: `e2e/public.spec.ts`
- Test: `scripts/launch-readiness-docs.test.mjs`

**Interfaces:**
- Consumes: the existing `PageData`, `contactPageData`, `errorPageData`, and template parsing flow.
- Produces: `StudioContact` fields `PhoneDisplay`, `PhoneHref`, `Email`, `EmailHref`, `PEC`, `PECHref`, `Address`, and `Hours`; `PageData.Contact StudioContact`; reusable template `contact-details`.

- [ ] **Step 1: Add failing public rendering tests**

Add a table-driven test to `internal/web/public/renderer_test.go` that requests `/`, `/profilo`, `/approccio`, `/aree-di-attivita`, `/contatti`, and a designed 404, then checks the relevant approved text. The assertions for the profile and contact surfaces must include:

```go
required := []string{
	"Università degli Studi di Milano",
	"2018",
	"Svolge l’attività di avvocato a Gallarate dal 2022.",
	`href="tel:+390331792529"`,
	"0331 792529",
	`href="mailto:callegarinale@gmail.com"`,
	`href="mailto:alessandro.callegarin@busto.pecavvocati.it"`,
	"Via Borghi 8, Gallarate (VA)",
	"Dal lunedì al venerdì, 09:00–12:30 e 15:00–19:00",
}
forbidden := []string{"103/110", "volontariato", "avvocato associato"}
```

Update existing renderer, contact-handler, and app error assertions so confirmed direct contacts replace `Telefono: DATO DA CONFERMARE`. Keep assertions for `DATO DA CONFERMARE` and `DA VALIDARE CON IL PROFESSIONISTA` only where unresolved privacy/professional/fiscal copy still renders.

Update `scripts/launch-readiness-docs.test.mjs` so runtime page sources must not contain the obsolete sentence:

```js
assert.doesNotMatch(
  publicPages,
  /Il modulo di contatto sarà attivato in una fase successiva/,
)
```

Keep the launch-readiness document assertion that names that sentence as a marker that must be absent before release.

- [ ] **Step 2: Run focused tests and verify the new expectations fail**

Run:

```bash
go test ./internal/web/public ./internal/app
node --test scripts/launch-readiness-docs.test.mjs
```

Expected: Go tests fail because the approved copy and contact links are not rendered; the Node test fails because the obsolete contact-form sentence is still present in `pages.go`.

- [ ] **Step 3: Add the shared contact model and template partial**

In `internal/web/public/pages.go`, add the contact field to `PageData` and define the immutable package value:

```go
type StudioContact struct {
	PhoneDisplay string
	PhoneHref    string
	Email        string
	EmailHref    string
	PEC          string
	PECHref      string
	Address      string
	Hours        string
}

var confirmedStudioContact = StudioContact{
	PhoneDisplay: "0331 792529",
	PhoneHref:    "tel:+390331792529",
	Email:        "callegarinale@gmail.com",
	EmailHref:    "mailto:callegarinale@gmail.com",
	PEC:          "alessandro.callegarin@busto.pecavvocati.it",
	PECHref:      "mailto:alessandro.callegarin@busto.pecavvocati.it",
	Address:      "Via Borghi 8, Gallarate (VA)",
	Hours:        "Dal lunedì al venerdì, 09:00–12:30 e 15:00–19:00",
}
```

Set `page.Contact = confirmedStudioContact` in the existing final `pageCatalog` loop. Set the same value on the `PageData` constructed by `WriteError`.

Create `internal/webassets/templates/partials/contact-details.html`:

```html
{{define "contact-details"}}
<ul class="contact-list">
  <li>Telefono: <a href="{{.Contact.PhoneHref}}">{{.Contact.PhoneDisplay}}</a></li>
  <li>Email: <a href="{{.Contact.EmailHref}}">{{.Contact.Email}}</a></li>
  <li>PEC: <a href="{{.Contact.PECHref}}">{{.Contact.PEC}}</a></li>
  <li>Indirizzo: {{.Contact.Address}}</li>
  <li>Orari: {{.Contact.Hours}}</li>
</ul>
{{end}}
```

Add that file to `parsePageTemplate` in `internal/web/public/renderer.go` so `missingkey=error` continues protecting every render path.

- [ ] **Step 4: Replace presentation and profile placeholders with approved copy**

Apply the exact approved Italian copy from `docs/superpowers/specs/2026-09-22-public-content-update-design.md`:

- home heading: `Assistenza legale chiara e rigorosa, vicina alle persone e alle loro esigenze.`;
- home lead: `Lo Studio Legale Alessandro Callegarin offre consulenza e assistenza a Gallarate e nel territorio della provincia di Varese, con un approccio fondato sull’ascolto, sulla chiarezza e sulla valutazione concreta di ogni situazione.`;
- profile: `Alessandro Callegarin si è laureato in Giurisprudenza presso l’Università degli Studi di Milano nel 2018. Svolge l’attività di avvocato a Gallarate dal 2022.`;
- approach: `Ogni questione richiede attenzione, metodo e una valutazione costruita sulle reali esigenze della persona.` and `L’obiettivo è offrire indicazioni comprensibili, illustrare con trasparenza le possibili strade e individuare la tutela più appropriata per il caso concreto.`;
- areas introduction: `Lo Studio assiste privati, famiglie e realtà del territorio in materia di diritto civile, penale e tributario. L’attività comprende, in particolare, separazioni e divorzi, tutela delle persone e dei minori, successioni e donazioni, contratti e locazioni, recupero crediti, risarcimento dei danni, diritti reali, procedimenti penali e contenzioso tributario.`;
- statement: `Comprendere il problema, chiarire le possibilità, costruire una tutela concreta.`

Remove `DevelopmentNotice` from `PageData` and from the standard template because the home and profile markers it represented are resolved. Do not remove privacy, article-editorial, or professional/fiscal markers that remain unresolved.

- [ ] **Step 5: Render confirmed contacts on every public fallback path**

Use `{{template "contact-details" .}}` in:

- `contact.html` for the main direct-contact block;
- `standard.html` when `eq .Kind "contact"`, covering the renderer-without-contact-service path;
- `error.html` for designed errors.

Use the same `.Contact` fields in the home contact panel and footer. Preserve the footer line `DATO DA CONFERMARE — dati professionali e fiscali.` because those facts remain unknown. Replace contact-form failure wording with `usa i recapiti diretti indicati in questa pagina` and remove claims that confirmed contacts are still pending.

On `/contatti`, replace the obsolete future-form paragraph with `L’invio di una richiesta non costituisce conferimento di incarico.` Do not change the privacy retention wording, its legal markers, or the consent version.

- [ ] **Step 6: Update browser expectations and format**

In `e2e/smoke.spec.ts` and the not-found return-home assertion in `e2e/public.spec.ts`, replace the old heading with:

```ts
name: "Assistenza legale chiara e rigorosa, vicina alle persone e alle loro esigenze.",
```

Run:

```bash
gofmt -w internal/web/public/pages.go internal/web/public/errors.go internal/web/public/renderer_test.go internal/web/public/contact_test.go internal/app/app_test.go
npm run format
```

Expected: formatters finish successfully and touch only files in this task.

- [ ] **Step 7: Run focused and full validation**

Run:

```bash
go test ./internal/web/public ./internal/app
node --test scripts/launch-readiness-docs.test.mjs
npm run format:check
make check
npm run e2e
```

Expected: every command exits 0. The unresolved legal markers may remain only in privacy, article-editorial, and professional/fiscal public copy; confirmed contact surfaces must contain no contact placeholder or obsolete future-form sentence.

- [ ] **Step 8: Perform visual responsive verification**

Use the existing Playwright configuration or Playwright CLI against the local port `8080`. Inspect home, profile, areas, contact, and a designed 404 at approximately `390x844`, `768x1024`, and `1440x1000`.

Acceptance criteria:

- no horizontal overflow, clipped text, or overlapping navigation;
- the long home heading wraps without covering the hero CTA;
- email and PEC wrap inside their containers on mobile;
- contact details remain readable in the footer and error page;
- no human imagery, cookie banner, or third-party content appears.

- [ ] **Step 9: Commit the approved task**

```bash
git add docs/superpowers/specs/2026-09-22-public-content-update-design.md docs/superpowers/plans/2026-09-22-public-content-update.md internal/web/public/pages.go internal/web/public/errors.go internal/web/public/renderer.go internal/webassets/templates/partials/contact-details.html internal/webassets/templates/pages/home.html internal/webassets/templates/pages/standard.html internal/webassets/templates/pages/contact.html internal/webassets/templates/pages/error.html internal/webassets/templates/partials/footer.html internal/web/public/renderer_test.go internal/web/public/contact_test.go internal/app/app_test.go e2e/smoke.spec.ts e2e/public.spec.ts scripts/launch-readiness-docs.test.mjs
git commit -m "feat: publish approved studio content"
```

Expected: one commit containing only the approved public-content update, tests, specification, and plan.
