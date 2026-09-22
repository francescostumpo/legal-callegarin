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
  recovery,
  readme,
  operations,
  launchReadiness,
  storageBicep,
  articleModel,
  articleRepository,
  articleMigration,
  articleBodies,
  storageSchema,
  contactService,
  contactModel,
  sessionModel,
  sessionService,
  publicCache,
] = await Promise.all([
  source("docs/storage-recovery.md", { optional: true }),
  source("README.md"),
  source("docs/operations.md"),
  source("docs/launch-readiness.md"),
  source("infra/modules/storage.bicep"),
  source("internal/articles/model.go"),
  source("internal/storage/azure/articles.go"),
  source("internal/storage/azure/migration.go"),
  source("internal/storage/azure/bodies.go"),
  source("internal/storage/azure/schema.go"),
  source("internal/contacts/service.go"),
  source("internal/contacts/model.go"),
  source("internal/auth/session.go"),
  source("internal/auth/service.go"),
  source("internal/web/public/cache.go"),
])

function requireTerms(document, terms) {
  for (const term of terms) assert.match(document, term)
}

test("runbook remains explicitly blocked with every recovery decision blank", () => {
  assert.match(recovery, /^# .+\n\n> \*\*Stato: BLOCKED\*\*/m)

  for (const field of [
    "RPO approvato",
    "RTO approvato",
    "Frequenza o trigger manuale",
    "Retenzione degli snapshot",
    "Destinazione cifrata",
    "Custode della chiave",
    "Revisore degli accessi al backup",
    "Autorità di ripristino",
    "Policy approvata dall'avvocato per backup ed erasure dei contatti",
  ]) {
    const escaped = field.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")
    assert.match(recovery, new RegExp(`^- \\*\\*${escaped}:\\*\\*\\s*$`, "m"))
  }

  requireTerms(recovery, [
    /nessuna[^.]*approvazione[^.]*compilat/i,
    /nessuna[^.]*prova[^.]*ripristino[^.]*eseguit/i,
    /non[^.]*pront[oa][^.]*produzione/i,
  ])
})

test("documented storage topology and limits match deployed sources", () => {
  requireTerms(storageBicep, [
    /name:\s*'Standard_ZRS'/,
    /var articlesTableName = 'articles'/,
    /var contactsTableName = 'contacts'/,
    /var sessionsTableName = 'sessions'/,
    /var articleBodiesContainerName = 'article-bodies'/,
    /deleteRetentionPolicy:[\s\S]{0,140}days:\s*30/,
    /containerDeleteRetentionPolicy:[\s\S]{0,140}days:\s*30/,
    /isVersioningEnabled:\s*true/,
    /publicNetworkAccess:\s*'Enabled'/,
    /allowBlobPublicAccess:\s*false/,
    /allowSharedKeyAccess:\s*false/,
    /defaultToOAuthAuthentication:\s*true/,
  ])
  requireTerms(recovery, [
    /Standard_ZRS/i,
    /ZRS[^.]*disponibilit[aà][^.]*non[^.]*backup/i,
    /repliche[^.]*eliminazion|eliminazion[^.]*repliche/i,
    /Table[^.]*nessun[^.]*soft delete[^.]*version/i,
    /Blob[^.]*30 giorni/i,
    /soft delete[^.]*non[^.]*account/i,
    /eliminazione[^.]*account/i,
    /perdita[^.]*region/i,
    /`publicNetworkAccess:\s*Enabled`/,
    /`allowBlobPublicAccess:\s*false`/,
    /`allowSharedKeyAccess:\s*false`/,
    /`defaultToOAuthAuthentication:\s*true`/,
  ])
  assert.doesNotMatch(recovery, /accesso privato/i)
})

test("article snapshot and restore preserve complete metadata-to-blob semantics", () => {
  assert.match(articleModel, /type BodyRef struct[\s\S]*BlobName[\s\S]*Version/)
  assert.match(
    articleModel,
    /DraftBody\s+\*BodyRef[\s\S]*PublishedBody\s+\*BodyRef/,
  )
  assert.match(articleBodies, /articleBlobName\(articleID, version\)/)
  assert.match(
    storageSchema,
    /return "articles\/" \+ articleID \+ "\/" \+ version \+ "\.json"/,
  )
  assert.match(articleRepository, /slugRecords\(article\)/)
  assert.match(
    articleMigration,
    /articleMigrationMarkerEntityType\s*=\s*"schemaMigration"/,
  )

  requireTerms(recovery, [
    /esportazione completa[^.]*Table `articles`/i,
    /entit[aà][^.]*articol[^.]*slug[^.]*schema|articol[^.]*slug[^.]*schema[^.]*entit[aà]/i,
    /`articles\/{articleID\}\/\{version\}\.json`/,
    /ogni[^.]*oggetto[^.]*riferit/i,
    /non[^.]*cronologia[^.]*versioni Blob/i,
    /Blob[^.]*prima[^.]*Table[^.]*second/i,
    /Table[^.]*vuot/i,
    /non[^.]*merge[^.]*PITR/i,
    /ETag[^.]*rigener/i,
    /riferimenti[^.]*route[^.]*slug/i,
  ])
})

test("contact, session, and public cache recovery match runtime behavior", () => {
  assert.match(contactService, /ReviewDueAt:\s*now\.AddDate\(2, 0, 0\)/)
  assert.match(contactService, /due := now\.AddDate\(0, 0, 30\)/)
  assert.match(contactService, /CancelDeletion[\s\S]*DeletionDueAt = nil/)
  assert.match(contactService, /PurgeDue[\s\S]*repository\.Delete/)
  assert.match(
    contactModel,
    /review due timestamp must be 24 months after creation/,
  )
  assert.match(sessionModel, /CreatedAt\.Add\(8\*time\.Hour\)/)
  assert.match(sessionService, /sessionLifetime\s*=\s*8 \* time\.Hour/)
  assert.match(publicCache, /publicCacheTTL\s*=\s*15 \* time\.Minute/)

  requireTerms(recovery, [
    /contatti[^.]*24 mesi/i,
    /cancellazione[^.]*30 giorni/i,
    /annull[^.]*prima[^.]*purge/i,
    /purge[^.]*fisic/i,
    /nessun[^.]*ledger[^.]*cancellazion/i,
    /flusso[^.]*separat[^.]*privacy/i,
    /policy[^.]*approvat[^.]*fail closed/i,
    /riconciliazione[^.]*esterna[^.]*non[^.]*PII/i,
    /non[^.]*resuscit/i,
    /`sessions`[^.]*mai[^.]*backup[^.]*ripristin/i,
    /otto ore|8 ore/i,
    /cookie[^.]*rifiutat/i,
    /nuovo login/i,
    /cache[^.]*15 minuti/i,
    /revisione[^.]*pulita|processo[^.]*pulit/i,
  ])
})

test("recovery matrix states exact present-day boundaries and Blob nuances", () => {
  requireTerms(recovery, [
    /## Matrice[^\n]*recuperabilit[aà]/i,
    /versione precedente[^|]*\|[^|]*(?:S[iì]|recuperabile)/i,
    /corrente[^|]*eliminat[^|]*\|[^|]*undelete/i,
    /undelete[^.]*versioni[^.]*promuov|promuov[^.]*undelete[^.]*versioni/i,
    /copi[^.]*versione[^.]*corrente/i,
    /versione[^|]*soft-delete|soft-delete[^|]*versione/i,
    /container[^|]*30 giorni/i,
    /`article-bodies`[^.]*nome originale[^.]*disponibil/i,
    /nome[^.]*non[^.]*riutilizz/i,
    /non[^.]*ricreare[^.]*`article-bodies`[^.]*redeploy[^.]*inventario[^.]*decisione/i,
    /annull[^|]*contatt[^|]*prima[^|]*purge/i,
    /storico[^|]*Table[^|]*non/i,
    /contatt[^|]*purge[^|]*non/i,
    /account[^|]*eliminat[^|]*non/i,
    /region[^|]*non/i,
    /scadut[^|]*Blob[^|]*\|[^|]*No/i,
    /disaster recovery[^.]*non[^.]*disponibil/i,
  ])
})

test("manual snapshot manifest is coherent, typed, bounded, and integrity checked", () => {
  requireTerms(recovery, [
    /snapshot[^.]*manuale[^.]*point-in-time/i,
    /downtime/i,
    /JSON[^.]*tipi[^.]*preserv/i,
    /manifest/i,
    /intervallo[^.]*UTC/i,
    /resource ID/i,
    /commit[^.]*image[^.]*schema/i,
    /versione[^.]*strument/i,
    /conteggi[^.]*byte/i,
    /SHA-256/i,
    /inventario/i,
  ])
})

test("maintenance window quiesces exact revisions and restores named traffic", () => {
  requireTerms(recovery, [
    /finestra[^.]*manutenzione[^.]*autorizzat/i,
    /quiesc[^.]*tutti[^.]*writer/i,
    /nomi[^.]*revisione[^.]*restituit[^.]*Azure/i,
    /traffico[^.]*precedente/i,
    /riattiv[^.]*solo[^.]*compatibil/i,
    /ripristin[^.]*traffico[^.]*nominat/i,
    /non[^.]*consistenza online/i,
  ])
})

test("operator access and staging handling remain least-privilege and secret-free", () => {
  requireTerms(recovery, [
    /Storage Explorer[^.]*Entra[^.]*MFA/i,
    /import[^.]*export[^.]*Table[^.]*tipi/i,
    /esportazione[^.]*sola lettura[^.]*risors/i,
    /`Storage Table Data Reader`[^.]*`articles`[^.]*`contacts`/i,
    /`Storage Blob Data Reader`[^.]*`article-bodies`/i,
    /assegnazion[^.]*temporane[^.]*revocat[^.]*subito[^.]*export/i,
    /JIT[^.]*Table Data Contributor[^.]*Blob Data Contributor/i,
    /Container Apps[^.]*approvazione[^.]*separat/i,
    /no[n]?[^.]*Owner/i,
    /no[n]?[^.]*chiavi[^.]*account/i,
    /no[n]?[^.]*connection string/i,
    /no[n]?[^.]*SAS/i,
    /volume[^.]*cifrat[^.]*approvat/i,
    /fuori[^.]*repository[^.]*sync[^.]*email[^.]*chat[^.]*ticket/i,
    /permessi[^.]*restrittiv/i,
    /directory[^.]*temporanea[^.]*esatta/i,
    /tracing[^.]*shell/i,
    /checksum[^.]*senza[^.]*contenuto/i,
    /copia[^.]*durevole[^.]*cifrat[^.]*verificat/i,
    /rimozione[^.]*crypto-erasure/i,
    /non[^.]*cancellazione sicura[^.]*SSD/i,
  ])
})

test("synthetic rehearsal covers failure-sensitive paths without claiming execution", () => {
  requireTerms(recovery, [
    /account[^.]*usa e getta/i,
    /solo[^.]*dati sintetici/i,
    /controlli[^.]*account[^.]*corrispond/i,
    /crea[^.]*pubblic[^.]*articolo[^.]*contatto/i,
    /sovrascritt[^.]*promozione[^.]*version/i,
    /corrente[^.]*eliminat[^.]*promozion/i,
    /versione[^.]*soft-delete[^.]*undelete/i,
    /programm[^.]*annull[^.]*contatt/i,
    /Blob[^.]*prima[^.]*Table[^.]*second/i,
    /session[^.]*vuot/i,
    /riavvio[^.]*pulit/i,
    /pubblic[^.]*admin[^.]*slug[^.]*contatt[^.]*session/i,
    /evidenz[^.]*senza[^.]*contenut[^.]*segret/i,
    /cleanup[^.]*esatt[^.]*limitat/i,
    /non[^.]*eseguit[oa][^.]*qui/i,
  ])
})

test("runbook fails closed on unsafe or unsupported recovery paths", () => {
  requireTerms(recovery, [
    /fail closed/i,
    /autorizzazione[^.]*evidenza[^.]*mancant/i,
    /Table[^.]*non vuot[^.]*ricostruzione[^.]*distruttiv/i,
    /merge[^.]*in-place[^.]*PITR/i,
    /post-purge[^.]*senza[^.]*riconciliazion/i,
    /account[^.]*regione[^.]*disastro/i,
    /cutover[^.]*non approvat|azione distruttiva[^.]*non approvat/i,
  ])
})

test("only official primary references are linked for storage recovery", () => {
  const links = [...recovery.matchAll(/\]\((https?:\/\/[^)]+)\)/g)].map(
    (match) => match[1],
  )
  assert.ok(links.length >= 8)
  for (const link of links) {
    assert.match(link, /^https:\/\/learn\.microsoft\.com\//)
  }
  requireTerms(recovery, [
    /storage-redundancy/,
    /soft-delete-blob-overview/,
    /soft-delete-container-overview/,
    /versioning-overview/,
    /storage-explorer-security/,
    /vs-azure-tools-storage-explorer-relnotes/,
    /container-apps\/revisions-manage/,
    /application-lifecycle-management/,
    /assign-azure-role-data-access/,
  ])
})

test("README, operations, and launch checklist make approval and rehearsal production blockers", () => {
  assert.match(
    readme,
    /\[[^\]]*recuper[^\]]*storage[^\]]*\]\(docs\/storage-recovery\.md\)/i,
  )
  assert.match(
    operations,
    /\[[^\]]*recuper[^\]]*storage[^\]]*\]\(storage-recovery\.md\)/i,
  )
  assert.match(
    launchReadiness,
    /\[[^\]]*recuper[^\]]*storage[^\]]*\]\(storage-recovery\.md\)/i,
  )

  for (const document of [readme, operations, launchReadiness]) {
    requireTerms(document, [
      /storage-recovery\.md/,
      /approvazion|approval/i,
      /prova[^.]*account[^.]*usa\s+e\s+getta|rehearsal[^.]*account[^.]*usa\s+e\s+getta/i,
      /produzione[^.]*BLOCKED|BLOCKED[^.]*produzione|production[^.]*BLOCKED|BLOCKED[^.]*production/i,
    ])
  }
})
