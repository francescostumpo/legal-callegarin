# Aggiornamento dei contenuti pubblici — Design

**Data:** 22 settembre 2026
**Stato:** approvato dall'utente

## Obiettivo

Sostituire i contenuti segnaposto del sito con le informazioni professionali e
di contatto confermate per lo Studio Legale Alessandro Callegarin, mantenendo
un tono equilibrato: professionale, comprensibile e vicino alle persone.

L'aggiornamento non deve introdurre qualifiche, risultati, anzianità o dati
amministrativi non verificati.

## Dati confermati

- Nome: Alessandro Callegarin.
- Sede: Via Borghi 8, Gallarate (VA).
- Telefono visualizzato: `0331 792529`.
- Collegamento telefonico: `tel:+390331792529`.
- Email: `callegarinale@gmail.com`.
- PEC: `alessandro.callegarin@busto.pecavvocati.it`.
- Orari: dal lunedì al venerdì, `09:00–12:30` e `15:00–19:00`.
- Formazione: laurea in Giurisprudenza presso l'Università degli Studi di
  Milano nel 2018.
- Attività professionale: esercita come avvocato a Gallarate dal 2022.

Il voto di laurea non deve essere pubblicato. Non devono essere pubblicati il
volontariato presso il servizio 118 né la dicitura «avvocato associato».

## Testi approvati

### Presentazione principale

> Assistenza legale chiara e rigorosa, vicina alle persone e alle loro
> esigenze.

> Lo Studio Legale Alessandro Callegarin offre consulenza e assistenza a
> Gallarate e nel territorio della provincia di Varese, con un approccio
> fondato sull'ascolto, sulla chiarezza e sulla valutazione concreta di ogni
> situazione.

### Profilo

> Alessandro Callegarin si è laureato in Giurisprudenza presso l'Università
> degli Studi di Milano nel 2018. Svolge l'attività di avvocato a Gallarate dal
> 2022.

### Approccio

> Ogni questione richiede attenzione, metodo e una valutazione costruita sulle
> reali esigenze della persona.

> L'obiettivo è offrire indicazioni comprensibili, illustrare con trasparenza
> le possibili strade e individuare la tutela più appropriata per il caso
> concreto.

### Ambiti di attività

> Lo Studio assiste privati, famiglie e realtà del territorio in materia di
> diritto civile, penale e tributario. L'attività comprende, in particolare,
> separazioni e divorzi, tutela delle persone e dei minori, successioni e
> donazioni, contratti e locazioni, recupero crediti, risarcimento dei danni,
> diritti reali, procedimenti penali e contenzioso tributario.

### Frase distintiva

> Comprendere il problema, chiarire le possibilità, costruire una tutela
> concreta.

## Collocazione e comportamento

- La home page usa la presentazione principale, la frase distintiva e un
  riepilogo dei contatti confermati.
- La pagina «Chi sono» usa il profilo, l'approccio e un richiamo sintetico agli
  ambiti di attività.
- La pagina «Aree di attività» mantiene la tassonomia già progettata e usa il
  testo approvato come introduzione, senza dichiarare specializzazioni.
- La pagina «Contatti», il footer e le pagine di errore espongono dati coerenti.
- Telefono, email e PEC sono collegamenti utilizzabili; l'indirizzo resta testo
  finché non viene approvata una destinazione cartografica esterna.
- Gli orari sono espressi in forma accessibile e leggibile anche su mobile.

## Vincoli e contenuti ancora mancanti

Questo aggiornamento non autorizza a inventare o completare:

- Ordine professionale, foro, numero e data di iscrizione;
- partita IVA, codice fiscale o altri dati fiscali;
- domicilio digitale o qualifiche ulteriori;
- testi legali definitivi della privacy policy e relativi estremi del titolare;
- dominio pubblico definitivo.

Gli eventuali segnaposto relativi a questi dati restano esplicitamente
tracciati nella checklist di lancio. Il sito non è considerato pronto per la
produzione finché i gate legali e amministrativi non sono chiusi.

## Verifica

- Aggiornare i test di rendering con i nuovi contenuti e verificare l'assenza
  dei vecchi segnaposto nei punti coperti.
- Eseguire la suite Go e i controlli frontend già definiti dal progetto.
- Verificare con Playwright la home, «Chi sono», «Aree di attività», contatti,
  footer e pagina di errore alle dimensioni mobile, tablet e desktop.
- Controllare che `tel:` e `mailto:` contengano i valori normalizzati.
- Confermare che voto di laurea, volontariato e «avvocato associato» non siano
  presenti nell'output pubblico.
