import assert from "node:assert/strict"
import { readFile } from "node:fs/promises"
import test from "node:test"

const repositoryRoot = new URL("../", import.meta.url)

async function source(path, { optional = false } = {}) {
  try {
    return await readFile(new URL(path, repositoryRoot), "utf8")
  } catch (error) {
    if (optional && error?.code === "ENOENT") return ""
    throw error
  }
}

const [
  launchReadiness,
  releaseTwo,
  readme,
  operations,
  contactHandler,
  publicPages,
  homeTemplate,
  contactTemplate,
  errorTemplate,
  footerTemplate,
] = await Promise.all([
  source("docs/launch-readiness.md", { optional: true }),
  source("docs/release-2.md", { optional: true }),
  source("README.md"),
  source("docs/operations.md"),
  source("internal/web/public/contact.go"),
  source("internal/web/public/pages.go"),
  source("internal/webassets/templates/pages/home.html"),
  source("internal/webassets/templates/pages/contact.html"),
  source("internal/webassets/templates/pages/error.html"),
  source("internal/webassets/templates/partials/footer.html"),
])

const runtimePublicCopy = [
  publicPages,
  homeTemplate,
  contactTemplate,
  errorTemplate,
  footerTemplate,
].join("\n")

const expectedGateHeadings = [
  "1. Identità e presentazione professionale",
  "2. Recapiti e canali di contatto",
  "3. Articoli, disclaimer e responsabilità editoriale",
  "4. Informativa privacy e comportamento dei dati di contatto",
  "5. Versione del consenso privacy",
  "6. Gate privacy-first e cookie banner",
  "7. Dominio, metadati e indicizzazione",
  "8. Immagini, accessibilità e revisione visuale",
  "9. Eliminazione dei marker di sviluppo",
  "10. Approvazione finale dell'avvocato",
  "11. Sign-off dell'operatore al rilascio",
]

function numberedGateSections(document) {
  const headings = [...document.matchAll(/^## (\d+\. .+)$/gm)]
  return headings.map((heading, index) => ({
    heading: heading[1],
    body: document.slice(
      heading.index,
      headings[index + 1]?.index ?? document.length,
    ),
  }))
}

function assertEveryGateHasBlockedEvidenceRecord(document) {
  const gates = numberedGateSections(document)
  assert.deepEqual(
    gates.map(({ heading }) => heading),
    expectedGateHeadings,
  )

  const requiredFields = [
    ["Stato del gate: BLOCKED", /^- \*\*Stato del gate:\*\* `BLOCKED`$/gm],
    ["Revisore/approvatore", /^- \*\*Revisore\/approvatore:\*\*\s*$/gm],
    ["Data", /^- \*\*Data:\*\*\s*$/gm],
    ["Evidenza/riferimento", /^- \*\*Evidenza\/riferimento:\*\*\s*$/gm],
  ]

  for (const gate of gates) {
    for (const [field, pattern] of requiredFields) {
      assert.equal(
        [...gate.body.matchAll(pattern)].length,
        1,
        `Gate ${gate.heading} must contain exactly one ${field} field`,
      )
    }
  }
}

function requireTerms(document, terms) {
  for (const term of terms) {
    assert.match(document, term)
  }
}

test("launch remains blocked until every auditable gate has evidence", () => {
  assert.match(launchReadiness, /^# .+\n\n> \*\*Stato: BLOCKED\*\*/m)
  assertEveryGateHasBlockedEvidenceRecord(launchReadiness)
  assert.doesNotMatch(
    launchReadiness,
    /^- \*\*Stato del gate:\*\* `APPROVED`$/gm,
  )
  requireTerms(launchReadiness, [
    /Stato del gate/i,
    /Revisore\/approvatore/i,
    /Data/i,
    /Evidenza\/riferimento/i,
    /campo[^.]*vuoto[^.]*blocc/is,
    /approvazione finale[^.]*avvocat/is,
    /sign-off[^.]*operatore/is,
    /\[registro Release 2\]\(release-2\.md\)/i,
  ])
  assert.match(
    launchReadiness,
    /approvazione finale[^#]*avvocat[\s\S]*?sign-off[^#]*operatore/i,
  )
})

test("the gate-record contract rejects one missing per-gate evidence field", () => {
  const gateFourStart = launchReadiness.indexOf(
    "## 4. Informativa privacy e comportamento dei dati di contatto",
  )
  const gateFiveStart = launchReadiness.indexOf(
    "## 5. Versione del consenso privacy",
  )
  assert.ok(gateFourStart >= 0 && gateFiveStart > gateFourStart)

  const gateFour = launchReadiness.slice(gateFourStart, gateFiveStart)
  const mutatedGateFour = gateFour.replace("- **Evidenza/riferimento:**", "")
  assert.notEqual(mutatedGateFour, gateFour)
  const controlledMutation = `${launchReadiness.slice(0, gateFourStart)}${mutatedGateFour}${launchReadiness.slice(gateFiveStart)}`

  assert.throws(
    () => assertEveryGateHasBlockedEvidenceRecord(controlledMutation),
    /Gate 4\.[^\n]*must contain exactly one Evidenza\/riferimento field/,
  )
})

test("lawyer, contact, editorial, and runtime-marker gates are complete", () => {
  requireTerms(launchReadiness, [
    /identità[^.]*ordine[^.]*iscrizion/is,
    /qualifiche/i,
    /dati[^.]*professionali[^.]*fiscali/is,
    /affermazioni territoriali/i,
    /biografia|profilo/i,
    /approccio[^.]*citazione/is,
    /(?:area|materia)[^.]*descrizion[^.]*terminolog/is,
    /telefono[^.]*email[^.]*PEC[^.]*indirizzo[^.]*orari/is,
    /fallback[^.]*errore/is,
    /responsabilit[aà] editorial/i,
    /nessun[^.]*caso[^.]*risultato[^.]*testimonianza[^.]*garanzia/is,
    /pubblicazione\s+iniziale[^.]*approvazione[^.]*autor/is,
  ])

  assert.doesNotMatch(runtimePublicCopy, /DATO DA CONFERMARE/)
  assert.doesNotMatch(runtimePublicCopy, /DA VALIDARE CON IL PROFESSIONISTA/)
  assert.doesNotMatch(
    publicPages,
    /Il modulo di contatto sarà attivato in una fase successiva/,
  )
  requireTerms(launchReadiness, [
    /DATO DA CONFERMARE/,
    /DA VALIDARE CON IL PROFESSIONISTA/,
    /modulo[^.]*fase\s+successiva/is,
    /sorgenti runtime[^.]*zero[^.]*marker/is,
    /artefatto[^.]*rilascio[^.]*evidenza/is,
  ])
})

test("Release 2 tracks every unknown without inventing approval evidence", () => {
  assert.match(releaseTwo, /^# Registro Release 2$/m)
  requireTerms(releaseTwo, [
    /Ordine/,
    /numero e data di iscrizione/i,
    /partita IVA/i,
    /codice fiscale/i,
    /domicilio digitale/i,
    /approvazione dell’avvocato/i,
    /responsabili/i,
    /trasferimenti/i,
    /dominio/i,
    /DNS/,
    /TLS/,
    /articoli/i,
    /RPO/,
    /RTO/,
    /restore/i,
    /accessibilità/i,
    /sign-off/i,
  ])
  assert.equal(
    [...releaseTwo.matchAll(/`DA ACQUISIRE`/g)].length,
    11,
    "Release 2 must leave its introduction and ten inputs explicitly unfilled",
  )
  assert.doesNotMatch(releaseTwo, /`APPROVED`/)
})

test("privacy gate matches consent, retention, recovery, and purge behavior", () => {
  const consentVersion = contactHandler.match(
    /contactConsentVersion\s*=\s*"([^"]+)"/,
  )?.[1]
  assert.equal(consentVersion, "privacy-v2-2026-09-22")
  assert.match(launchReadiness, new RegExp(`\\b${consentVersion}\\b`))

  requireTerms(launchReadiness, [
    /titolare[^.]*contatto privacy/is,
    /finalit[aà][^.]*base\s+giuridica/is,
    /responsabili[^.]*destinatari/is,
    /Italy North/i,
    /trasferiment/i,
    /diritti[^.]*reclam/is,
    /formulazioni[^.]*sicurezza/is,
    /creazione[^.]*24 mesi[^.]*revisione[^.]*non[^.]*cancellazione automatica/is,
    /richiesta[^.]*cancellazione manuale[^.]*(?:30 giorni[^.]*recuper|recuper[^.]*30 giorni)/is,
    /annullat[^.]*30 giorni/is,
    /purge[^.]*opportunistic[^.]*successiva[^.]*console[^.]*non[^.]*scadenza/is,
    /testo privacy[^.]*differisce[^.]*materialmente[^.]*cambiare[^.]*versione[^.]*nuova approvazione/is,
    /versione[^.]*distribuita/is,
  ])
})

test("privacy-first gate makes the no-banner decision conditional", () => {
  requireTerms(launchReadiness, [
    /nessun cookie pubblico/i,
    /nessun[^.]*analytics/i,
    /nessun[^.]*tracciamento/i,
    /nessuna[^.]*dipendenza runtime[^.]*terz/is,
    /nessuna[^.]*pubblicit[aà]/i,
    /nessun[^.]*contenuto[^.]*incorporato[^.]*terz/is,
    /banner[^.]*non[^.]*mostrato[^.]*solo[^.]*condizioni/is,
    /aggiunta[^.]*revisione[^.]*legale[^.]*tecnica[^.]*prima[^.]*rilascio/is,
  ])
})

test("domain, metadata, error paths, assets, and responsive visuals are gated", () => {
  requireTerms(launchReadiness, [
    /dominio[^.]*canonico/i,
    /apex[^.]*www/i,
    /title[^.]*description[^.]*Open Graph[^.]*canonical/is,
    /pagin[ae][^.]*errore[^.]*contatt/is,
    /sitemap[^.]*robots[^.]*applicabil/is,
    /provenienza[^.]*licen[sz]a/is,
    /alt text/i,
    /senza[^.]*figure umane/is,
    /mobile[^.]*tablet[^.]*desktop/is,
  ])
})

test("README and external operations gates link to the single checklist", () => {
  const link =
    /\[checklist[^\]]*(?:lancio|produzione)[^\]]*\]\(docs\/launch-readiness\.md\)/i
  assert.match(readme, link)
  assert.match(
    operations,
    /## Gate esterni e responsabilità[\s\S]*\[checklist[^\]]*(?:lancio|produzione)[^\]]*\]\(launch-readiness\.md\)/i,
  )
})
