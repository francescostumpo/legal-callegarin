# Operazioni

## Confine di autorità

> **Importante:** tutti i comandi Azure, DNS e di deployment riportati qui sono
> esempi **operator-only** e non sono stati eseguiti dal Task 11. Ogni mutazione
> futura richiede autorizzazione esplicita e una revisione preventiva
> dell'ambito, dell'identità operatore e di tutti i parametri. Le verifiche live
> sono anch'esse riservate a un operatore autorizzato.

## Governance del budget Azure

Il parametro `monthlyBudgetAmount` è facoltativo e vale `0` per impostazione
predefinita. Con questo valore il deployment non crea nessuna risorsa budget e
non richiede il permesso `Microsoft.Consumption/budgets/write` per tale
risorsa. Un valore positivo è espresso nella valuta di fatturazione della
sottoscrizione Azure.

L'attivazione con un valore positivo è un'operazione mutativa riservata a un
operatore dei costi autorizzato. L'identità che esegue il deployment deve avere
`Microsoft.Consumption/budgets/write` sul gruppo di risorse; **Cost Management
Contributor** è il ruolo predefinito pertinente con privilegi minimi per
gestire il budget. Nessuna assegnazione di ruolo viene creata da questa
infrastruttura.

Il budget invia un avviso tramite l'action group quando il costo effettivo
raggiunge almeno l'80% della soglia mensile. È soltanto un avviso: non arresta
o ferma risorse, non scala l'applicazione e non elimina risorse. I dati di costo
normalmente hanno un ritardo di 8–24 ore; per una sottoscrizione PAYG il ritardo
può arrivare a 72 ore. Per una nuova sottoscrizione possono essere necessarie
fino a 48 ore prima che le funzioni di budget siano disponibili.

Se il permesso per un budget a livello di gruppo di risorse non è disponibile,
il fallback è **operator-only**: un operatore autorizzato crea manualmente un
budget a subscription scope e lo limita con un filtro di dimensione
`ResourceGroupName`, operator `In`, il cui unico valore è il nome esatto del
gruppo di risorse di produzione. Il fallback non fa parte di `main.bicep`.

Queste attività mutative sono descritte per l'operatore ma non sono state
eseguite dal Task 11C2. Questa sezione non contiene comandi che modifichino
implicitamente Azure; ogni eventuale esecuzione futura richiede autorizzazione
esplicita e una revisione preventiva dell'ambito e dei parametri.

## Dominio personalizzato Azure Container Apps

Questa procedura separa tre operazioni diverse:

1. il bootstrap iniziale dell'infrastruttura con `main.bicep` e senza binding di
   dominio;
2. la configurazione DNS e il deployment una tantum di
   `custom-domain.bicep`;
3. i rollout applicativi ordinari della CI, che distribuiscono un digest
   immutabile e non sono deployment completi dell'infrastruttura.

L'infrastruttura iniziale richiede permessi più ampi rispetto ai
rollout applicativi successivi. Non assegnare alla CI ordinaria le credenziali o
il compito di eseguire il bootstrap, creare il gruppo di risorse, modificare il
DNS o riconciliare tutta l'infrastruttura.

### 1. Preparazione locale sicura

L'operatore lavora da una checkout revisionata e assegna soltanto identificatori
non segreti a variabili di shell. I valori seguenti sono segnaposto e devono
essere sostituiti dopo aver verificato sottoscrizione e ambito:

```sh
SUBSCRIPTION_ID='<subscription-id>'
RESOURCE_GROUP='<resource-group-name>'
LOCATION='italynorth'
MAIN_DEPLOYMENT='<main-deployment-name>'
DOMAIN_DEPLOYMENT='<custom-domain-deployment-name>'
APP_NAME='<container-app-name>'
APEX_DOMAIN='<apex-domain>'
MAIN_PARAMETERS='infra/main.local.bicepparam'
DOMAIN_PARAMETERS='infra/custom-domain.local.bicepparam'
```

Creare i file locali partendo dagli esempi versionati; il pattern
`infra/*.local.bicepparam` è ignorato da Git:

```sh
cp infra/main.example.bicepparam "$MAIN_PARAMETERS"
cp infra/custom-domain.example.bicepparam "$DOMAIN_PARAMETERS"
```

I file locali contengono i valori non segreti reali, ma continuano a ottenere
`GHCR_TOKEN`, `ADMIN_PASSWORD_HASH` e `SESSION_KEY_BASE64` esclusivamente con le
tre chiamate `readEnvironmentVariable()`. I tre valori sicuri vanno iniettati
nel processo da un meccanismo protetto e non devono comparire mai in file,
argomenti della shell, output, log, ticket o chat. Non abilitare debug della
shell o della CLI e non conservare il JSON compilato dei parametri.

Prima di qualsiasi chiamata live, eseguire i controlli locali warning-free. Lo
stdout di `build-params` contiene i parametri compilati e deve essere sempre
scartato:

```sh
az bicep build --file infra/main.bicep --stdout >/dev/null
az bicep build-params --file "$MAIN_PARAMETERS" --stdout >/dev/null
```

### 2. Bootstrap iniziale senza dominio

Nel file privato `infra/main.local.bicepparam`, mantenere
`customDomainApex = ''` soltanto per questo bootstrap. Impostare valori non
segreti reali e verificati, compreso il digest GHCR immutabile. Mantenere
`monthlyBudgetAmount = 0`, salvo autorizzazione separata di un operatore dei
costi secondo la sezione precedente.

#### OPERATORE — MUTATIVO — NON ESEGUITO DAL TASK 11

Un operatore autorizzato crea esattamente un gruppo di risorse in Italy North:

```sh
az group create \
  --subscription "$SUBSCRIPTION_ID" \
  --name "$RESOURCE_GROUP" \
  --location "$LOCATION" \
  --output none
```

Il what-if seguente interroga Azure ma non applica la modifica. È comunque
operator-only, non è stato eseguito dal Task 11 e deve essere revisionato senza
salvarne l'output. Con un file `.bicepparam` dotato di `using`, non aggiungere
`--template-file`:

```sh
az deployment group what-if \
  --subscription "$SUBSCRIPTION_ID" \
  --resource-group "$RESOURCE_GROUP" \
  --name "$MAIN_DEPLOYMENT" \
  --parameters "$MAIN_PARAMETERS" \
  --result-format ResourceIdOnly
```

#### OPERATORE — MUTATIVO — NON ESEGUITO DAL TASK 11

Dopo l'approvazione del what-if, l'operatore può creare o aggiornare il
deployment iniziale:

```sh
az deployment group create \
  --subscription "$SUBSCRIPTION_ID" \
  --resource-group "$RESOURCE_GROUP" \
  --name "$MAIN_DEPLOYMENT" \
  --parameters "$MAIN_PARAMETERS" \
  --output none
```

Registrare soltanto questi output non segreti del deployment principale:
`managedEnvironmentName`, `containerAppName`, `containerAppFqdn`,
`managedEnvironmentStaticIp`,
`managedEnvironmentCustomDomainVerificationId`, `dnsApexAHost`,
`dnsApexAValue`, `dnsApexTxtHost`, `dnsApexTxtValue`,
`dnsWwwCnameHost`, `dnsWwwCnameValue`, `dnsWwwTxtHost` e
`dnsWwwTxtValue`. Questa query è limitata a tali output dichiarati:

```sh
az deployment group show \
  --subscription "$SUBSCRIPTION_ID" \
  --resource-group "$RESOURCE_GROUP" \
  --name "$MAIN_DEPLOYMENT" \
  --query "properties.outputs.{managedEnvironmentName:managedEnvironmentName.value,containerAppName:containerAppName.value,containerAppFqdn:containerAppFqdn.value,managedEnvironmentStaticIp:managedEnvironmentStaticIp.value,managedEnvironmentCustomDomainVerificationId:managedEnvironmentCustomDomainVerificationId.value,dnsApexAHost:dnsApexAHost.value,dnsApexAValue:dnsApexAValue.value,dnsApexTxtHost:dnsApexTxtHost.value,dnsApexTxtValue:dnsApexTxtValue.value,dnsWwwCnameHost:dnsWwwCnameHost.value,dnsWwwCnameValue:dnsWwwCnameValue.value,dnsWwwTxtHost:dnsWwwTxtHost.value,dnsWwwTxtValue:dnsWwwTxtValue.value}" \
  --output json
```

### 3. Record DNS presso il registrar

La creazione o modifica dei record presso il registrar è una mutazione
**operator-only**, non eseguita dal Task 11. Dopo aver confrontato gli output,
configurare esattamente:

| Tipo  | Host        | Valore                                                    |
| ----- | ----------- | --------------------------------------------------------- |
| A     | `@`         | `dnsApexAValue`, lo static IP del Managed Environment     |
| TXT   | `asuid`     | `dnsApexTxtValue`, il custom-domain verification ID       |
| CNAME | `www`       | `dnsWwwCnameValue`, il Container App FQDN generato        |
| TXT   | `asuid.www` | `dnsWwwTxtValue`, lo stesso custom-domain verification ID |

Il CNAME `www` deve puntare direttamente al Container App FQDN generato, senza
proxy o CNAME intermediario. Se nella radice esiste almeno un record CAA,
autorizzare DigiCert aggiungendo il valore esatto `0 issue "digicert.com"`.
Mantenere nel tempo i record A, CNAME, TXT e CAA di routing e verifica: sono
necessari anche per il rinnovo dei certificati gestiti. Non usare proxy davanti
al CNAME e non sostituire il target con il dominio predefinito dell'ambiente.

Il DNS deve puntare a Azure Container Apps prima della richiesta dei
certificati gestiti. Attendere la propagazione e verificare da più resolver
prima del deployment custom-domain, confrontando ogni risposta con gli output
registrati:

```sh
dig @1.1.1.1 +short A "$APEX_DOMAIN"
dig @8.8.8.8 +short A "$APEX_DOMAIN"
dig @1.1.1.1 +short TXT "asuid.$APEX_DOMAIN"
dig @8.8.8.8 +short TXT "asuid.$APEX_DOMAIN"
dig @1.1.1.1 +short CNAME "www.$APEX_DOMAIN"
dig @8.8.8.8 +short CNAME "www.$APEX_DOMAIN"
dig @1.1.1.1 +short TXT "asuid.www.$APEX_DOMAIN"
dig @8.8.8.8 +short TXT "asuid.www.$APEX_DOMAIN"
dig @1.1.1.1 +short CAA "$APEX_DOMAIN"
dig @8.8.8.8 +short CAA "$APEX_DOMAIN"
```

Non procedere finché resolver indipendenti non concordano. Anche con DNS
corretto, il provisioning può rimanere `Pending` per diversi minuti mentre
Azure Container Apps completa l'issuance.

### 4. Deployment dei due certificati e binding

Nel file privato `infra/custom-domain.local.bicepparam`, sostituire i segnaposto
con l'apex reale e l'origine canonica HTTPS scelta. Verificare che digest
immutabile, credenziale GHCR, username e hash admin, chiave di sessione, nomi
di progetto/ambiente/app e origine canonica coincidano con il deployment base
revisionato. Questo entry point trasporta il contratto completo dell'app:
valori obsoleti possono alterare la configurazione runtime, anche se lo scopo
dell'operazione è il dominio.

Ripetere i build warning-free; lo stdout dei parametri rimane scartato:

```sh
az bicep build --file infra/custom-domain.bicep --stdout >/dev/null
az bicep build-params --file "$DOMAIN_PARAMETERS" --stdout >/dev/null
```

Il what-if live è operator-only, non eseguito qui, e deve mostrare le due
scritture complete della stessa app, i due certificati stabili e nessun'altra
modifica inattesa:

```sh
az deployment group what-if \
  --subscription "$SUBSCRIPTION_ID" \
  --resource-group "$RESOURCE_GROUP" \
  --name "$DOMAIN_DEPLOYMENT" \
  --parameters "$DOMAIN_PARAMETERS" \
  --result-format ResourceIdOnly
```

#### OPERATORE — MUTATIVO — NON ESEGUITO DAL TASK 11

Soltanto dopo approvazione di parametri e what-if, creare o aggiornare il
deployment di dominio:

```sh
az deployment group create \
  --subscription "$SUBSCRIPTION_ID" \
  --resource-group "$RESOURCE_GROUP" \
  --name "$DOMAIN_DEPLOYMENT" \
  --parameters "$DOMAIN_PARAMETERS" \
  --output none
```

L'operazione è idempotente: prima registra esattamente due binding `Disabled`,
poi richiede il certificato apex con validazione HTTP e quello `www` con
validazione CNAME, infine applica entrambi come `SniEnabled`. L'issuance può
restare `Pending` per diversi minuti.

### 5. Verifica successiva al deployment

Registrare gli output non segreti `apexCertificateId`,
`apexCertificateName`, `wwwCertificateId`, `wwwCertificateName`,
`containerAppName` e `containerAppFqdn`. Usare i due ID registrati come valori
non segreti per `APEX_CERTIFICATE_ID` e `WWW_CERTIFICATE_ID`.

Le query seguenti sono read-only, operator-only e non sono state eseguite dal
Task 11:

```sh
az resource show \
  --ids "$APEX_CERTIFICATE_ID" \
  --api-version 2026-01-01 \
  --query properties.provisioningState \
  --output tsv

az resource show \
  --ids "$WWW_CERTIFICATE_ID" \
  --api-version 2026-01-01 \
  --query properties.provisioningState \
  --output tsv

az containerapp show \
  --subscription "$SUBSCRIPTION_ID" \
  --resource-group "$RESOURCE_GROUP" \
  --name "$APP_NAME" \
  --query properties.configuration.ingress.customDomains \
  --output json
```

Il `provisioningState` del certificato apex e del certificato `www` deve essere
`Succeeded`; entrambi i nomi, apex e `www`, devono apparire con binding
`SniEnabled` e con il rispettivo certificate ID deterministico.

Verificare TLS sia sull'apex sia su `www`; un errore di certificato deve far
fallire il controllo. Richiedere inoltre all'host non canonico un percorso con
query e controllare una risposta `308` il cui `Location` conserva esattamente
percorso e query verso l'origine canonica. Per esempio, predisporre una
directory temporanea privata e impostare `CANONICAL_ORIGIN`,
`NONCANONICAL_ORIGIN`, `VERIFY_PATH` e `VERIFY_DIR` con soli valori non segreti:

```sh
curl --fail --silent --show-error \
  --output "$VERIFY_DIR/apex.body" \
  "https://$APEX_DOMAIN/"

curl --fail --silent --show-error \
  --output "$VERIFY_DIR/www.body" \
  "https://www.$APEX_DOMAIN/"

curl --silent --show-error --max-redirs 0 \
  --dump-header "$VERIFY_DIR/canonical.headers" \
  --output /dev/null \
  "$NONCANONICAL_ORIGIN$VERIFY_PATH"

grep -q '^HTTP/.* 308' "$VERIFY_DIR/canonical.headers"
sed 's/\r$//' "$VERIFY_DIR/canonical.headers" \
  | grep -Fqxi "location: $CANONICAL_ORIGIN$VERIFY_PATH"
```

Scaricare una pagina pubblica con header separati e verificare che sia
visualizzabile e non contenga alcun header pubblico `Set-Cookie`:

```sh
curl --fail --silent --show-error \
  --dump-header "$VERIFY_DIR/public.headers" \
  --output "$VERIFY_DIR/public.body" \
  "$CANONICAL_ORIGIN/"

! grep -qi '^set-cookie:' "$VERIFY_DIR/public.headers"
```

Controllare sia `/health/live` sia `/health/ready` sull'hostname ACA generato,
che costituisce il percorso operativo di recupero:

```sh
curl --fail --silent --show-error \
  --output "$VERIFY_DIR/live.body" \
  "$ACA_RECOVERY_ORIGIN/health/live"

curl --fail --silent --show-error \
  --output "$VERIFY_DIR/ready.body" \
  "$ACA_RECOVERY_ORIGIN/health/ready"
```

Infine aprire un browser privato, raggiungere il login admin attraverso
l'origine canonica, autenticarsi e fare logout. Non mettere credenziali o
cookie in comandi di shell, screenshot, ticket o chat.

### 6. Rendere il binding durevole in main

Subito dopo il successo, copiare l'apex reale nel file durevole ignorato
`infra/main.local.bicepparam` e mantenere coerente `publicBaseUrl`. Ogni futuro
deployment completo di `main.bicep` deve usare il dominio non vuoto: in questo
modo riproduce entrambi i binding SNI e i due certificate ID deterministici.

Non riusare mai il valore di bootstrap `customDomainApex = ''` dopo il binding,
salvo la procedura di emergenza esplicita descritta sotto. Prima del successivo
deployment principale ripetere build, build-params con stdout scartato e
what-if revisionato.

### 7. Errori, recupero e rollback del dominio

Se l'issuance fallisce o rimane `Pending`, ispezionare i record diretti A,
CNAME, TXT e CAA, correggerli presso il registrar, attendere la propagazione e
rieseguire il deployment idempotente. I primi binding `Disabled` possono
rimanere mentre il binding finale è bloccato: non sostituirli con modifiche
manuali incomplete.

Mai usare un Container App PUT scheletrico o parziale: omettere proprietà
da un PUT può eliminare configurazione runtime, secret reference, identity,
probe, scala o ingress. Usare sempre il modulo app completo e la procedura
revisionata. Mai rimuovere o reindirizzare il DNS prima che il TLS
sostitutivo sia pronto e verificato.

Un rollback di emergenza del dominio richiede un deployment completo e
revisionato dell'app con entrambi i binding vuoti; è l'unica eccezione all'uso
del dominio non vuoto in main e il what-if deve escludere ogni altra modifica.
L'eliminazione dei certificati è una pulizia facoltativa e separata, eseguibile
soltanto dopo che i binding sono stati rimossi. Non cancellare certificati come
prima azione di recupero.

L'hostname generato ACA e gli endpoint health restano il percorso operativo di
recupero. I redirect verso l'host canonico possono invece influenzare le
normali rotte pubbliche raggiunte tramite hostname generato; questo non va
confuso con un errore di liveness o readiness.

### 8. Rotazioni e rollback applicativo

- La credenziale GHCR deve avere soltanto lo scope PAT `read:packages`, senza
  scope di scrittura o amministrazione. La sua rotazione usa lo stesso percorso
  di deployment sicuro e revisionato, senza valori inline o output del token.
- La rotazione dell'hash della password admin segue
  [`docs/password-recovery.md`](password-recovery.md). Non inserire password o
  PHC in parametri, argomenti o log.
- La rotazione della session-key invalida immediatamente tutte le sessioni e
  richiede una nuova autenticazione amministrativa.
- Usare lo stesso digest immutabile durante tutta l'operazione di dominio e
  mantenere disponibile la precedente revisione sana per il rollback
  applicativo.

### 9. Verifiche live differite

La compilazione locale non dimostra il comportamento live. Disponibilità del
provider, risultato del what-if, certificate issuance e renewal, propagazione
DNS, TLS, alert e cost behavior restano non verificati finché un operatore
autorizzato non esegue e registra la procedura in un'attività separata.
