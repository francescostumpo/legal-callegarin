# Recupero della password amministratore

Il recupero è una procedura occasionale riservata a un operatore. Non esistono
email di reset, un workflow CI per cambiare secret o una password in chiaro
nella configurazione.

## Credenziali applicative

- `ADMIN_USERNAME` è il nome del solo amministratore ed è un valore non
  segreto.
- `ADMIN_PASSWORD_HASH` è un hash Argon2id in formato PHC segreto. Sostituirlo
  cambia la credential version: tutte le sessioni create con il precedente hash
  vengono rifiutate immediatamente, anche prima della scadenza di otto ore.
- `SESSION_KEY_BASE64` deve decodificare in almeno 32 byte ed è un valore
  segreto. La sua
  rotazione invalida tutte le sessioni e segue la stessa disciplina descritta
  qui: input protetto, nuovo secret ACA e nuova revisione verificata.

Il PHC va trattato come una password: non inserirlo in repository, file di
parametri, argomenti CLI, cronologia shell, log, screenshot, ticket o chat.

## 1. Generazione locale

Su una workstation fidata e dalla revisione verificata del repository,
eseguire `go run ./cmd/adminhash` senza argomenti. Inserire la nuova password
due volte nei prompt nascosti. Il programma emette su standard output una sola
riga PHC: acquisirla direttamente con un meccanismo protetto, senza copiarla in
un comando o in un file persistente. Non passare mai password o PHC sulla riga
di comando.

## 2. Finestra di manutenzione e secret Azure

Aprire una finestra di manutenzione verificata nella quale non siano in corso
né deploy né retention. Leggere prima il traffico dell'app e verificare che
esista una sola regola positiva, con `revisionName` esplicito, peso 100 e senza
`latestRevision: true`. Copiare il nome opaco restituito da Azure, senza
ricostruirlo:

```sh
az containerapp show \
  --subscription '<subscription-uuid>' \
  --resource-group '<resource-group-name>' \
  --name '<container-app-name>' \
  --query '{activeRevisionsMode:properties.configuration.activeRevisionsMode,traffic:properties.configuration.ingress.traffic}' \
  --output json
```

Solo dopo questa verifica impostare il nome non segreto e ispezionare la
revisione esatta. Verificare insieme `active: true`, `health: Healthy`, modalità
schema compatibile e image GHCR con digest immutabile canonico; l'image di
questa revisione, non il template dell'app, è il digest instradato da riusare:

```sh
ROUTED_REVISION='<sole-positive-azure-returned-revision-name>'

az containerapp revision show \
  --subscription '<subscription-uuid>' \
  --resource-group '<resource-group-name>' \
  --name '<container-app-name>' \
  --revision "$ROUTED_REVISION" \
  --query '{active:properties.active,health:properties.healthState,image:properties.template.containers[0].image,mode:properties.template.containers[0].env[?name==`ARTICLE_STORAGE_SCHEMA_MODE`].value|[0]}' \
  --output json

ROUTED_IMAGE='<verified-immutable-image-from-routed-revision>'
```

Nel Portale Azure modificare **soltanto** il secret già esistente
`admin-password-hash` della Container App, usando il campo di input protetto.
Non usare `az containerapp secret set`: metterebbe il PHC negli argomenti del
processo. La modifica del secret da sola non è sufficiente, perché i secret ACA
sono app-scoped e non creano né aggiornano automaticamente una revisione.

Anche il successivo input Bicep protetto che alimenta
`ADMIN_PASSWORD_HASH` deve essere aggiornato al nuovo PHC; altrimenti un futuro
deployment completo potrebbe reintrodurre il valore precedente.

## 3. Nuova revisione e promozione

Usare il digest immutabile letto dalla revisione instradata e il deploy engine
revisionato. Tutti gli argomenti seguenti sono non segreti; il suffix di
recovery deve essere unico e rispettare la sintassi validata dallo script:

```sh
./scripts/deploy-container-app.sh \
  --subscription-id '<subscription-uuid>' \
  --resource-group '<resource-group-name>' \
  --container-app-name '<container-app-name>' \
  --image-digest "$ROUTED_IMAGE" \
  --revision-suffix '<unique-recovery-suffix>' \
  --public-base-url 'https://<canonical-host>'
```

Non aggiungere il PHC agli argomenti del deploy. Lo script crea una nuova
revisione, verifica digest, modalità schema, health e smoke check, poi promuove
il nome opaco restituito da Azure. Una semplice modifica del secret senza
questa nuova revisione lascia le revisioni in esecuzione con il valore già
caricato.

## 4. Verifica e gestione del rollback

In una finestra privata verificare nella stessa sessione operativa che la
sessione precedente sia rifiutata, che il nuovo login e la sessione funzionino
e che il logout revochi la sessione. Controllare health endpoint e log senza
registrare password, PHC, cookie o token.

La creazione di ogni nuova sessione esegue una pulizia opportunistica: un login
riuscito elimina dal database le sessioni scadute, con scadenza minore o uguale
all'istante corrente prima di generare il nuovo token. Non esiste un cleanup
job generale. Una sessione revocata ma non ancora scaduta rimane memorizzata
fino alla scadenza e a un successivo login riuscito; sessioni revocate,
scadute o con credential version precedente sono comunque rifiutate.

Se il recupero dipende da una compromissione, e non soltanto da una password
dimenticata, disattivare la vecchia revisione dopo la promozione e non
instradarvi mai più traffico. Un rollback per codice o health deve scegliere
soltanto una revisione sana che carichi la nuova credenziale: il rollback non
deve ripristinare l'hash compromesso.

Per ruotare `SESSION_KEY_BASE64`, aggiornare analogamente il secret ACA
`session-key-base64` tramite input protetto, creare e verificare una nuova
revisione e poi promuoverla. La session-key nuova invalida tutte le sessioni;
verificare nuovamente login, sessione e logout.

Riferimenti: [secret di Azure Container
Apps](https://learn.microsoft.com/en-us/azure/container-apps/manage-secrets) e
[revisioni di Azure Container
Apps](https://learn.microsoft.com/en-us/azure/container-apps/revisions).
