# Recupero occasionale dello storage

> **Stato: BLOCKED**

Questo runbook descrive un modello proposto di recupero tecnico occasionale per
lo storage attuale. Non attesta l'esistenza di un backup completo, di un
ripristino point-in-time (PITR) né di una procedura di disaster recovery pronta
per la produzione. Nessuna approvazione è compilata e nessuna prova di
ripristino è stata eseguita in questo task. La produzione non è pronta finché
le decisioni, le autorizzazioni e una prova reale con evidenza verificabile non
sono completate.

## Quattro capacità da non confondere

- **Disponibilità:** l'account StorageV2 usa `Standard_ZRS`. ZRS replica nella
  regione e aumenta disponibilità e durabilità, ma non è un backup: una
  eliminazione o sovrascrittura viene applicata alle repliche. Non protegge da
  ogni perdita dell'intera regione.
- **Recupero Blob nello stesso account:** `article-bodies` ha versioning e soft
  delete di Blob e container configurati a 30 giorni. Sono difese temporanee
  nello stesso account, non una copia indipendente. Il soft delete non protegge
  dall'eliminazione dello storage account.
- **Snapshot logico manuale proposto:** una copia coerente e cifrata di Table e
  Blob, presa offline durante downtime. È soltanto una proposta bloccata dalle
  approvazioni elencate sotto.
- **Disaster recovery:** non è disponibile nell'infrastruttura attuale. Non
  esistono qui replica geografica, backup in un altro account, cutover o restore
  validato per eliminazione dell'account o perdita della regione.

L'account contiene le Table `articles`, `contacts` e `sessions` e il container
privato `article-bodies`. Per le Table non esiste nessun soft delete o versioning e
nel repository non esistono Azure Backup vault, PITR, deletion lock, export
automatici o cutover verso un account alternativo.

Il controllo di rete reale è `publicNetworkAccess: Enabled`: il Bicep non
configura un private endpoint o network ACL. Questo non rende pubblici i dati:
l'accesso pubblico anonimo ai Blob è disabilitato con
`allowBlobPublicAccess: false`, Shared Key è disabilitato con
`allowSharedKeyAccess: false` e OAuth è il default con
`defaultToOAuthAuthentication: true`.

## Matrice della recuperabilità attuale

| Evento                                                                    | Recuperabile oggi?                                                                       | Confine reale                                                                                                                                                                              |
| ------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Blob sovrascritto, versione precedente ancora presente                    | Sì, nello stesso account                                                                 | Selezionare la versione desiderata e copiarla/promuoverla per creare una nuova versione corrente.                                                                                          |
| Blob corrente eliminato con versioning attivo                             | Solo tramite `undelete` quando necessario e se una versione desiderata è ancora presente | L'`undelete` ripristina versioni o snapshot soft-deleted ma non ricrea la versione corrente; occorre poi copiare/promuovere la versione desiderata a corrente.                             |
| Singola versione Blob realmente soft-deleted entro la retention           | Sì, nello stesso account                                                                 | L'`undelete` ripristina tutte le versioni soft-deleted ancora trattenute; dopo la verifica, copiare/promuovere quella desiderata a corrente.                                               |
| Container `article-bodies` eliminato entro 30 giorni                      | Sì, nello stesso account, solo se il nome originale resta disponibile                    | Il nome `article-bodies` non deve essere riutilizzato; recuperare il container col nome originale, inventariare il risultato e applicare l'eventuale promozione della versione desiderata. |
| Cancellazione di un contatto programmata ma non ancora sottoposta a purge | Sì                                                                                       | La console può annullare la cancellazione prima del purge e azzerare la relativa scadenza.                                                                                                 |
| Storico di una Table o entità Table eliminata                             | No                                                                                       | Azure Tables non fornisce qui versioning, soft delete o PITR.                                                                                                                              |
| Contatto già rimosso dal purge                                            | No                                                                                       | Il purge elimina fisicamente la riga e non esiste nessun ledger durevole delle cancellazioni.                                                                                              |
| Storage account eliminato                                                 | No                                                                                       | Soft delete e versioning dei Blob non recuperano un account eliminato.                                                                                                                     |
| Perdita dell'intera regione                                               | No                                                                                       | `Standard_ZRS` resta nella regione e non configura una copia geografica.                                                                                                                   |
| Cronologia scaduta dei Blob o rimossa dalla lifecycle policy              | No                                                                                       | Una versione Blob scaduta non è recuperabile dall'infrastruttura corrente.                                                                                                                 |

Questa matrice riguarda soltanto le capacità presenti oggi. Non autorizza un
ripristino e non trasforma il percorso proposto in PITR o disaster recovery.

Se `article-bodies` risulta eliminato, fare fail closed: non ricreare
`article-bodies` e non avviare alcun redeploy che possa ricrearlo prima di aver
completato inventario e decisione di recupero. Il nome originale deve rimanere
disponibile; riutilizzarlo impedisce il restore del container soft-deleted.

## Decisioni e approvazioni obbligatorie

Ogni campo deve restare vuoto finché la persona autorizzata non registra una
decisione reale con data ed evidenza. Un campo vuoto mantiene lo stato
`BLOCKED`; non sono ammessi valori dedotti o predefiniti.

- **RPO approvato:**
- **RTO approvato:**
- **Frequenza o trigger manuale:**
- **Retenzione degli snapshot:**
- **Destinazione cifrata:**
- **Custode della chiave:**
- **Revisore degli accessi al backup:**
- **Autorità di ripristino:**
- **Policy approvata dall'avvocato per backup ed erasure dei contatti:**

## Snapshot logico manuale proposto

Lo snapshot manuale point-in-time richiede downtime e una finestra di
manutenzione autorizzata. Non garantisce consistenza online: prima
dell'esportazione si
devono quiescere tutti i writer, inclusi invio contatti e mutazioni della
console, usando esclusivamente i nomi di revisione restituiti da Azure.
L'operatore registra preventivamente revisioni attive e distribuzione
del traffico, attende l'arresto dei writer e delimita nel manifest l'intervallo
UTC effettivo.

Con una versione corrente di Azure Storage Explorer, autenticata tramite
Microsoft Entra con MFA e capace di import/export delle Table con tipi
preservati, l'operatore autorizzato prepara:

1. l'esportazione completa in JSON con tipi preservati della Table `articles`,
   comprese tutte le entità articolo, slug e schema/migrazione;
2. l'esportazione completa e separata in JSON con tipi preservati della Table
   `contacts`;
3. ogni oggetto corrente del container `article-bodies`, conservandone il path
   esatto, inclusi ogni oggetto riferito da `DraftBody` o `PublishedBody`, e una
   verifica che ogni riferimento esportato dalla Table punti a un oggetto
   presente;
4. un manifest senza contenuto applicativo che registri intervallo UTC, resource
   ID sorgente, commit applicativo, digest image, schema mode, versione dello
   strumento, conteggi di righe e oggetti, byte e inventario SHA-256.

I corpi hanno path `articles/{articleID}/{version}.json`. La copia degli oggetti
correnti non è un'esportazione della cronologia delle versioni Blob nello
storage account e non deve essere descritta come tale.

### Gestione dell'area di staging

La staging area deve essere una directory temporanea esatta, registrata nel
manifest, su un volume cifrato approvato dall'operatore e fuori da repository,
sync/cartelle sincronizzate, email, chat e ticket. Prima di operare si applicano
permessi restrittivi e si disabilita il tracing della shell. Log ed evidenze
possono contenere conteggi e checksum, mai contenuto grezzo o PII.

La copia durevole cifrata deve essere verificata integralmente prima della
rimozione della staging area. La rimozione o crypto-erasure deve seguire la
policy approvata e può distruggere la chiave della copia temporanea; non si deve
mai dichiarare una cancellazione sicura dei blocchi fisici di un SSD che non sia
stata tecnicamente dimostrata.

## Ripristino coerente

Il ripristino avviene soltanto verso Table vuote, in un account bersaglio
verificato e autorizzato. L'ordine è Blob prima e Table seconda. Per gli
articoli occorre importare e
verificare prima tutti i Blob riferiti, quindi importare come insieme completo
le entità articolo, slug e schema della Table `articles`. Un merge in una Table
non vuota non è PITR e non può essere dichiarato tale. Gli ETag possono essere
rigenerati: la validazione confronta valori di dominio e riferimenti semantici,
non l'uguaglianza con i vecchi ETag.

Dopo l'import, validare riferimenti, route e slug: ogni riferimento al corpo
deve essere risolvibile, ogni route canonica e storica deve puntare allo slug corretto e gli stati draft, published e
withdrawn corrispondano al manifest. Solo una revisione applicativa verificata
come compatibile con image e schema registrati può essere riattivata. Riattivare
solo una revisione compatibile e poi ripristinare
il traffico nominato precedente solo dopo i controlli; usare una revisione o un
processo pulito affinché la cache HTML pubblica in-memory di 15 minuti sia
vuota prima della verifica semantica.

### Contatti: flusso separato soggetto a privacy

Per i contatti, la revisione è fissata a creazione più 24 mesi; i record hanno
PII, consenso, stato e timestamp, e una cancellazione manuale è
programmata a 30 giorni e può essere annullata prima del purge. Il purge elimina
fisicamente la riga Table.

Il loro restore è un flusso separato con approvazione privacy. Senza una policy
di retention del backup approvata e un registro di riconciliazione esterno,
non-PII e durevole delle cancellazioni avvenute dopo lo snapshot, la procedura
deve fare fail closed. Non si deve resuscitare silenziosamente un contatto
eliminato dopo lo snapshot o già sottoposto a purge: prima dell'import occorre
riconciliare e omettere quei record secondo l'autorità di ripristino e la policy
approvata dall'avvocato.

La riconciliazione esterna deve quindi usare un record non-PII e non il contenuto
dei contatti.

### Sessioni escluse

La Table `sessions` non deve mai essere inclusa nel backup né ripristinata; i
suoi hash di token e il suo stato hanno durata esatta di otto ore. Il target deve lasciare
o ricreare `sessions` vuota; ogni vecchio cookie deve essere rifiutato e
l'amministratore deve eseguire un nuovo login.

## Ruoli e identità dell'operatore

Usare Azure Storage Explorer aggiornato, accesso Microsoft Entra con MFA e
ruoli a minimo privilegio sulle esatte risorse dati:

- per l'esportazione, sola lettura dati sulle esatte risorse:
  `Storage Table Data Reader` con scope soltanto sulle Table esatte `articles` e `contacts`, e
  `Storage Blob Data Reader` con scope soltanto sul container esatto
  `article-bodies`;
- per restore e prova, assegnazioni JIT `Storage Table Data Contributor` sulle
  esatte Table target e `Storage Blob Data Contributor` sull'esatto container
  target, revocate al termine;
- per quiesce, riattivazione e traffico di Container Apps, approvazione separata
  per il controllo limitato all'app interessata.

Le assegnazioni di export sono temporanee e devono essere revocate subito dopo
l'export, verificando e registrando la revoca senza esporre dati o identità.

Non usare il ruolo Owner, chiavi dell'account, connection string, SAS di lunga
durata, token o segreti in file, manifest, output o evidenze. La possibilità di
leggere dati non concede implicitamente autorità per cutover, cancellazioni o
altre mutazioni.

## Prova obbligatoria su account usa e getta

Prima della produzione è obbligatoria una prova autorizzata su un account usa e
getta con solo dati sintetici. La prova non è stata eseguita qui.
Il piano approvato deve delimitare in anticipo account, risorse e cleanup esatto
e limitato, senza includere dati, identificatori o segreti di produzione.

La prova deve:

1. verificare che i controlli dell'account usa e getta corrispondano a quelli di
   produzione: StorageV2, ZRS, `publicNetworkAccess: Enabled`,
   `allowBlobPublicAccess: false`, `allowSharedKeyAccess: false`,
   `defaultToOAuthAuthentication: true`, nomi di Table e container, versioning,
   soft delete e lifecycle;
2. creare e pubblicare tramite l'app un articolo sintetico e creare tramite
   l'app un contatto sintetico;
3. programmare e annullare la cancellazione del contatto, quindi quiescere i
   writer per nome esatto di revisione ed eseguire export e manifest;
4. provare una sovrascrittura Blob e la promozione della versione precedente;
5. verificare che un Blob corrente eliminato richieda la promozione per ricreare
   la corrente;
6. eliminare davvero una versione, eseguire l'undelete della versione
   soft-deleted e promuovere quella desiderata;
7. importare in un target vuoto tutti i Blob prima e le Table dopo, mantenendo
   `sessions` vuota;
8. effettuare un riavvio pulito e verificare pagine pubbliche, console admin,
   slug canonico e storico, contatto, rifiuto della sessione precedente e nuovo
   login;
9. registrare evidenza con approvazioni, tempi, resource ID, conteggi, checksum
   e risultati, senza contenuto o segreti;
10. riesaminare i target autorizzati e completare il cleanup esatto e limitato
    delle sole risorse usa e getta, registrandone l'esito.

Campi della prova reale, intenzionalmente vuoti:

- **Stato prova:** `BLOCKED`
- **Autorizzazione:**
- **Data e operatore:**
- **Ambito esatto:**
- **Evidenza:**
- **Esito cleanup:**

## Condizioni di arresto

La procedura fa fail closed e si arresta in presenza di:

- autorizzazione o evidenza mancante;
- una Table non vuota sottoposta a ricostruzione distruttiva;
- merge in-place presentato come PITR;
- restore post-purge di contatti senza riconciliazione approvata;
- account e regione coinvolti in un disastro, non coperto da questo runbook;
- nome `article-bodies` già riutilizzato o redeploy avviato prima della decisione
  sul container soft-deleted;
- cutover non approvato o qualunque azione distruttiva non approvata;
- conteggi, checksum, riferimenti Blob o compatibilità applicativa incoerenti.

In nessuno di questi casi l'operatore deve improvvisare una mutazione. Registra
il blocco, conserva solo evidenza non sensibile e richiede una nuova decisione
all'autorità competente.

## Riferimenti ufficiali

- [Ridondanza di Azure Storage](https://learn.microsoft.com/en-us/azure/storage/common/storage-redundancy)
- [Soft delete per Azure Blob Storage](https://learn.microsoft.com/en-us/azure/storage/blobs/soft-delete-blob-overview)
- [Soft delete per i container Blob](https://learn.microsoft.com/en-us/azure/storage/blobs/soft-delete-container-overview)
- [Versioning dei Blob](https://learn.microsoft.com/en-us/azure/storage/blobs/versioning-overview)
- [Guida di sicurezza di Azure Storage Explorer](https://learn.microsoft.com/en-us/azure/storage/common/storage-explorer-security)
- [Formato JSON per import/export Table in Storage Explorer](https://learn.microsoft.com/en-us/azure/vs-azure-tools-storage-explorer-relnotes)
- [Gestire le revisioni di Azure Container Apps](https://learn.microsoft.com/en-us/azure/container-apps/revisions-manage)
- [Lifecycle di Azure Container Apps](https://learn.microsoft.com/en-us/azure/container-apps/application-lifecycle-management)
- [Assegnare ruoli per l'accesso ai dati Table](https://learn.microsoft.com/en-us/azure/storage/tables/assign-azure-role-data-access)
- [Assegnare ruoli per l'accesso ai dati Blob](https://learn.microsoft.com/en-us/azure/storage/blobs/assign-azure-role-data-access)
