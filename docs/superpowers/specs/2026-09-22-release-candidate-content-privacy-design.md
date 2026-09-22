# Release-candidate content and privacy design

**Date:** 22 September 2026  
**Status:** approved for implementation by the project owner; final professional,
privacy, and production release approval remains an external launch gate.

## 1. Objective

Remove every user-visible development marker and make the website a coherent
release candidate without inventing professional, registration, or fiscal
facts. Publish a complete privacy notice grounded in the application's verified
behavior and the confirmed studio contacts. Keep facts and approvals that can
only come from the lawyer or the production environment in a separate Release 2
register rather than exposing placeholders to visitors.

## 2. Non-negotiable boundaries

- Do not invent the professional register, registration number or date, forum,
  VAT number, tax code, digital domicile, qualifications, or fiscal data.
- Do not publish the university grade, volunteer experience, or the wording
  `avvocato associato`.
- Do not add analytics, advertising, profiling, third-party embeds, remote
  fonts, maps, social widgets, or a public cookie-consent banner.
- Do not describe a contact as automatically deleted exactly at 24 months or
  exactly 30 days after a deletion request. The implementation performs a
  24-month review and an opportunistic purge after a 30-day recovery window.
- Do not mark any launch gate approved without an approver, date, and concrete
  evidence from the real release environment.
- Do not place production credentials, secrets, domains, or mutable image tags
  in tracked example configuration.

## 3. Public identity and footer

The public footer identifies the practice only as:

> Avv. Alessandro Callegarin — Gallarate (VA)

It continues to show the already confirmed address, telephone, email, PEC, and
opening hours from the central `StudioContact` value. It contains no substitute
or marker for missing fiscal or register data.

The `/approccio` privacy paragraph becomes:

> Le informazioni condivise con lo Studio sono trattate con riservatezza e con
> attenzione alla loro pertinenza rispetto alla richiesta. Ogni comunicazione
> viene gestita nel rispetto degli obblighi professionali e della normativa
> applicabile.

The article catalog must use its existing truthful empty state when no articles
are published. The dead catalog marker is removed from runtime source.

## 4. Privacy notice structure and content

The route `/privacy-cookie-policy` becomes the complete public notice. Its lead
states plainly that the notice explains how the site and contact form process
personal data. It contains the following sections.

### 4.1 Controller and privacy channels

The candidate notice identifies:

- controller: Avv. Alessandro Callegarin;
- office: Via Borghi 8, Gallarate (VA);
- ordinary email: `callegarinale@gmail.com`;
- PEC: `alessandro.callegarin@busto.pecavvocati.it`.

Email and PEC are presented as channels for privacy requests in this candidate.
Their final designation remains subject to the lawyer's launch approval and is
recorded in the Release 2 register.

### 4.2 Data, purposes, and legal bases

The notice describes only verified processing:

- navigation and security data processed transiently to deliver the site,
  protect it, prevent abuse, rate-limit submissions, and diagnose failures;
- name, email, optional telephone number, message, privacy-notice version, and
  operational timestamps/statuses stored when a contact request is submitted;
- contact data processed to receive and answer the request and to take steps at
  the data subject's request before any engagement, under Article 6(1)(b) GDPR;
- technical security processing based on the controller's legitimate interest
  in protecting the site and communications, under Article 6(1)(f) GDPR.

The notice says that submitting the form does not create a professional
engagement and asks visitors not to send documents or unnecessary special or
judicial-category data through the first-contact form.

### 4.3 Provision and recipients

Using the form is optional; visitors may instead use the direct contacts.
Name, email, message, and acknowledgement of the notice are required to answer
through the form; telephone is optional. Without the required data the form
cannot be processed.

Access is limited to the lawyer and persons expressly authorized for technical
operation. Data may be handled by necessary infrastructure and maintenance
providers acting under the applicable contractual and data-protection terms.
The application storage is configured for Microsoft Azure in the European
Union, region Italy North. The notice does not claim that regional storage by
itself excludes every possible international processing operation; any such
operation must rely on safeguards required by the GDPR and the provider's
applicable agreements.

### 4.4 Retention and deletion behavior

Contact requests are retained for the time needed to answer and manage the
request and are flagged for review 24 months after collection. That date is a
review deadline, not automatic deletion. A manual deletion request opens a
30-day technical recovery window; after it expires the record becomes eligible
for permanent purge during a subsequent administration operation. Longer
retention is limited to legal obligations or the establishment, exercise, or
defence of legal claims.

### 4.5 Rights and complaint

The notice lists access, rectification, erasure, restriction, objection, and
data portability where applicable. Requests can be sent to either confirmed
email channel. The visitor may lodge a complaint with the Italian Data
Protection Authority (`Garante per la protezione dei dati personali`). There is
no automated decision-making or profiling.

### 4.6 Cookies and tracking

The public site sets no cookies, performs no analytics or profiling, and loads
no runtime resources from third-party services. No public cookie banner is
shown. The administration area uses one strictly necessary session cookie only
after authentication; it is `Secure`, `HttpOnly`, `SameSite=Strict`, revocable,
and expires after at most eight hours. Introducing non-technical cookies or
tracking requires a new legal and technical assessment before release.

This follows the Garante's published distinction: technical cookies do not
require consent, while information under Article 13 remains required; a consent
banner is appropriate when non-technical tracking requires consent.

## 5. First-layer notice and acknowledgement

The contact page removes its development banner and presents a concise notice
next to the form. It identifies the controller and privacy channels, explains
the purpose and Article 6(1)(b) basis, summarizes required/optional fields,
recipients, Italy North storage, retention behavior, and rights, and links to
the complete notice.

The checkbox remains an acknowledgement:

> Confermo di aver letto l'informativa privacy.

It must not be described as consent because contact processing relies on
pre-contractual steps rather than consent. The stored implementation field may
retain its existing internal name for compatibility, but administration copy
must say `Presa visione informativa`, not `Consenso privacy`.

Because the public privacy text changes materially, the recorded notice version
becomes `privacy-v2-2026-09-22`. Tests must prove that new submissions store this
exact value and that the administration console displays it as the notice
version acknowledged by the visitor.

## 6. Release 2 register

Create a tracked document listing the following unresolved items without
showing them on public pages:

- professional Order, register number and date, forum, and any additional
  qualification the lawyer wants to publish;
- VAT number, tax code, complete fiscal identity, digital domicile, and office
  postal code if legally or operationally required;
- the lawyer's signed approval of identity, biography, practice areas,
  editorial disclaimer, contact copy, privacy notice, privacy channels,
  retention behavior, and images;
- confirmation of processor agreements, actual recipient categories,
  international-transfer assessment, and the final Azure configuration;
- production domain, apex/`www` policy, DNS, TLS, canonical metadata, and search
  indexing decision;
- initial approved articles, if the launch should not use an empty catalog;
- storage recovery ownership, RPO/RTO, backup retention, restore drill, and
  contact-erasure policy;
- accessibility and responsive review of the exact production artifact;
- final operator sign-off after every launch gate has evidence.

The register is not a substitute for launch approval. Every launch gate remains
`BLOCKED` until its real evidence exists.

## 7. Testing and acceptance criteria

Implementation is accepted only when:

- runtime public sources contain no `DATO DA CONFERMARE` or
  `DA VALIDARE CON IL PROFESSIONISTA`;
- public pages contain no invented fiscal, register, grade, volunteer, or
  employment facts;
- the full and first-layer notices agree on controller, channels, purposes,
  legal bases, recipients, retention, rights, and cookie behavior;
- a submitted contact stores `privacy-v2-2026-09-22`;
- administration labels the event as acknowledgement of the notice;
- public pages set no cookies and show no cookie banner;
- the real article empty state remains truthful and article disclaimers remain
  visible when articles are published;
- unit, contract, frontend, build, E2E, and clean-worktree checks pass;
- mobile, tablet, and desktop visual checks find no clipping, overflow, broken
  navigation, inaccessible form states, or unreadable policy sections;
- the pull request and port-8080 preview are updated only after independent task
  review approval.

## 8. Sources informing the privacy structure

- Regulation (EU) 2016/679, especially Articles 5, 6, 12, 13, 15–22, 25, 32,
  and 77: <https://eur-lex.europa.eu/eli/reg/2016/679/oj>
- Garante per la protezione dei dati personali, Cookie FAQ:
  <https://www.garanteprivacy.it/faq/cookie>
- Garante, Guidelines on cookies and other tracking tools, 10 June 2021:
  <https://www.garanteprivacy.it/home/docweb/-/docweb-display/docweb/9677876>
