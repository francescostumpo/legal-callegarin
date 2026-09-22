# Checklist di idoneità al lancio

> **Stato: BLOCKED**

Questa checklist è il gate obbligatorio prima di rendere il sito disponibile in
produzione. Lo stato iniziale di ogni gate è `BLOCKED`: un campo di registrazione
vuoto, un dato ancora sintetico oppure un'evidenza non verificabile blocca il
rilascio e non implica mai approvazione. Cambiare uno stato richiede una verifica
reale e una registrazione completa; questo documento non attesta che le verifiche
esterne siano già avvenute.

I dati e le approvazioni ancora da acquisire sono raccolti nel
[registro Release 2](release-2.md); la loro registrazione separata non approva
alcun gate.

Per ogni gate sono obbligatori questi quattro campi:

- **Stato del gate:** `BLOCKED` oppure `APPROVED`
- **Revisore/approvatore:**
- **Data:**
- **Evidenza/riferimento:**

## 1. Identità e presentazione professionale

Verificare con l'avvocato identità, ordine e iscrizione all'albo, foro di
riferimento, qualifiche, dati professionali e fiscali del footer. Approvare
separatamente le affermazioni territoriali, la biografia e il profilo,
l'approccio e la citazione autoriale. Rileggere ogni area di attività, la sua
descrizione e la terminologia giuridica: nessuna informazione deve essere
dedotta o completata senza conferma del professionista.

- **Stato del gate:** `BLOCKED`
- **Revisore/approvatore:**
- **Data:**
- **Evidenza/riferimento:**

## 2. Recapiti e canali di contatto

Confermare telefono, email, PEC, indirizzo e orari. Revisionare anche il testo
di fallback dei recapiti e tutte le copie di errore del canale di contatto,
inclusi indisponibilità, mancata conferma e avvertenza sul mancato conferimento
dell'incarico. L'evidenza deve includere una prova dei percorsi pubblico,
errore e successo con i dati approvati.

- **Stato del gate:** `BLOCKED`
- **Revisore/approvatore:**
- **Data:**
- **Evidenza/riferimento:**

## 3. Articoli, disclaimer e responsabilità editoriale

Approvare disclaimer, responsabilità editoriale, attribuzione dell'autore e
processo di revisione. Nessun contenuto può inventare caso, risultato,
testimonianza o garanzia. Registrare la pubblicazione iniziale e l'approvazione
dell'autore per ciascun articolo inizialmente pubblicato, comprese copertina,
titolo, sommario e corpo.

- **Stato del gate:** `BLOCKED`
- **Revisore/approvatore:**
- **Data:**
- **Evidenza/riferimento:**

## 4. Informativa privacy e comportamento dei dati di contatto

La revisione legale deve approvare titolare e contatto privacy, finalità e base
giuridica, responsabili e destinatari, fatti relativi a Italy North e agli
eventuali trasferimenti, diritti e reclamo all'autorità di controllo, nonché le
formulazioni sulla sicurezza. Le dichiarazioni devono descrivere soltanto
misure e fornitori realmente verificati.

Il comportamento operativo da approvare e comunicare è esattamente questo:

- la data di revisione è calcolata dalla creazione più 24 mesi; è una revisione
  e non una cancellazione automatica;
- una richiesta di cancellazione manuale programma la cancellazione e apre una
  finestra di recupero di 30 giorni; la richiesta può essere annullata durante
  quei 30 giorni;
- dopo l'idoneità alla rimozione, il purge è opportunistico: viene eseguito da
  una successiva operazione nella console di amministrazione e non è garantito
  esattamente alla scadenza del trentesimo giorno.

Il testo pubblico non deve promettere cancellazione automatica, un istante di
purge garantito o capacità di recupero diverse da quelle effettive.

- **Stato del gate:** `BLOCKED`
- **Revisore/approvatore:**
- **Data:**
- **Evidenza/riferimento:**

## 5. Versione dell’informativa presa in visione

La versione implementata per l'informativa presa in visione dal modulo è
`privacy-v2-2026-09-22`. Se il testo privacy differisce materialmente da questa
versione, prima del rilascio occorre cambiare nuovamente la versione e ottenere
una nuova approvazione legale. Registrare qui la versione distribuita e
collegarla al testo approvato e all'artefatto immutabile verificato.

- **Stato del gate:** `BLOCKED`
- **Revisore/approvatore:**
- **Data:**
- **Evidenza/riferimento:**
- **Versione distribuita:**

## 6. Gate privacy-first e cookie banner

Verificare sul reale artefatto di rilascio: nessun cookie pubblico, nessun
analytics, nessun tracciamento, nessuna dipendenza runtime di terzi, nessuna
pubblicità e nessun contenuto incorporato di terzi. Il cookie banner non viene
mostrato solo finché tutte queste condizioni restano vere; non è una decisione
riutilizzabile dopo un cambiamento di funzionalità.

L'aggiunta di cookie pubblici, analytics, tracciamento, dipendenze runtime o
contenuti di terzi, pubblicità o embed richiede una nuova revisione legale e
tecnica prima del rilascio. Aggiornare quindi informativa, consenso e possibile
banner soltanto sulla base dell'esito documentato.

- **Stato del gate:** `BLOCKED`
- **Revisore/approvatore:**
- **Data:**
- **Evidenza/riferimento:**

## 7. Dominio, metadati e indicizzazione

Registrare il dominio di produzione canonico e la policy tra apex e `www`, con
redirect verificato. Approvare title, description, Open Graph e URL canonical
di ogni tipo di pagina. Verificare le pagine pubbliche di errore e contatto e,
se applicabili al rilascio, sitemap e robots; se non applicabili, registrarne la
motivazione come evidenza invece di ometterli.

- **Stato del gate:** `BLOCKED`
- **Revisore/approvatore:**
- **Data:**
- **Evidenza/riferimento:**

## 8. Immagini, accessibilità e revisione visuale

Usare esclusivamente la libreria editoriale incorporata, di prima parte e senza
figure umane. Verificare e registrare provenienza e licenza di ogni asset,
pertinenza editoriale e alt text significativo oppure corretta marcatura
decorativa. Completare una revisione visuale dell'intero sito su mobile, tablet
e desktop, includendo navigazione, form, stati di errore, articoli e console di
amministrazione.

- **Stato del gate:** `BLOCKED`
- **Revisore/approvatore:**
- **Data:**
- **Evidenza/riferimento:**

## 9. Eliminazione dei marker di sviluppo

Prima della build candidata cercare nelle sorgenti runtime almeno:

- `DATO DA CONFERMARE`;
- `DA VALIDARE CON IL PROFESSIONISTA`;
- la dichiarazione obsoleta `Il modulo di contatto sarà attivato in una fase
successiva`.

La ricerca sulle sorgenti runtime della release candidate deve produrre zero
marker. Registrare comando, commit e risultato; non è sufficiente ignorare un
match o nasconderlo con CSS. Questa verifica rende coerente la copia candidata,
ma l'assenza dei marker nell'esatto artefatto di rilascio richiede ancora
evidenza verificabile prima di approvare il gate.

- **Stato del gate:** `BLOCKED`
- **Revisore/approvatore:**
- **Data:**
- **Evidenza/riferimento:**

## 10. Approvazione finale dell'avvocato

L'approvazione finale dell'avvocato copre l'insieme esatto di testi, articoli,
recapiti, informativa privacy, immagini e dominio destinati alla produzione. È
distinta dalle singole revisioni precedenti e non può essere compilata
dall'operatore tecnico per delega implicita.

- **Stato del gate:** `BLOCKED`
- **Revisore/approvatore:**
- **Data:**
- **Evidenza/riferimento:**

## 11. Sign-off dell'operatore al rilascio

Il sign-off dell'operatore viene registrato soltanto dopo aver verificato che i
gate 1–10 siano `APPROVED`, completi di evidenza, e che l'artefatto controllato
sia quello effettivamente destinato al rilascio. Questo gate è distinto
dall'approvazione legale e professionale dell'avvocato. Devono inoltre essere
complete le approvazioni del [runbook di recupero dello
storage](storage-recovery.md) e l'evidenza reale della prova su account usa e
getta; la produzione resta `BLOCKED` finché una delle due manca.

- **Stato del gate:** `BLOCKED`
- **Revisore/approvatore:**
- **Data:**
- **Evidenza/riferimento:**
