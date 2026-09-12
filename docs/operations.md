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

#### 2.1. Normalizzazione obbligatoria del traffico dopo il Bicep

Dopo ogni deployment completo di `main.bicep`, iniziale o successivo, e dopo
ogni deployment completo di `custom-domain.bicep`, eseguire questa
normalizzazione prima di abilitare deploy e retention. I template Bicep
impostano temporaneamente `latestRevision: true`, mentre entrambi gli engine
operativi rifiutano intenzionalmente routing implicito a latest.

Leggere il traffico e ispezionare **tutte** le revisioni restituite da Azure:

```sh
az containerapp show \
  --subscription "$SUBSCRIPTION_ID" \
  --resource-group "$RESOURCE_GROUP" \
  --name "$APP_NAME" \
  --query '{activeRevisionsMode:properties.configuration.activeRevisionsMode,traffic:properties.configuration.ingress.traffic}' \
  --output json

az containerapp revision list \
  --subscription "$SUBSCRIPTION_ID" \
  --resource-group "$RESOURCE_GROUP" \
  --name "$APP_NAME" \
  --all \
  --query '[].{name:name,active:properties.active,health:properties.healthState,image:properties.template.containers[0].image,mode:properties.template.containers[0].env[?name==`ARTICLE_STORAGE_SCHEMA_MODE`].value|[0]}' \
  --output json
```

Selezionare soltanto una revisione con nome opaco copiato dall'output Azure,
attiva, `Healthy`, con digest immutabile e modalità schema compatibile. Dopo la
verifica impostare il segnaposto non segreto ed eliminare esplicitamente la
regola latest mentre si assegna il 100% al nome esatto:

```sh
NORMALIZED_REVISION='<azure-returned-active-healthy-compatible-revision-name>'

az containerapp ingress traffic set \
  --subscription "$SUBSCRIPTION_ID" \
  --resource-group "$RESOURCE_GROUP" \
  --name "$APP_NAME" \
  --revision-weight "latest=0" "$NORMALIZED_REVISION=100" \
  --output none
```

Rileggere immediatamente app, traffico e revisione e verificare che non esista
alcuna regola `latestRevision: true`, che l'unica regola con peso positivo sia
la revisione nominata con peso 100 e che questa sia ancora attiva, sana e
compatibile. Solo dopo questa verifica esatta abilitare deploy e retention.

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

Questo è un deployment completo: prima di proseguire ripetere integralmente la
normalizzazione del traffico della sezione 2.1 e verificarne nuovamente routing
nominato, revisione, digest, health e modalità schema.

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

## Ambiente GitHub `production`

Creare e proteggere l'environment `production` di GitHub **prima**
di eseguire qualsiasi workflow. Il semplice riferimento a un environment
inesistente può auto-crearlo senza protezioni: questa preparazione è quindi una
precondizione fail-closed, non un miglioramento facoltativo.

Nelle regole di deployment impostare `Selected branches and tags` e consentire
soltanto `main`.
Proteggere inoltre `main` con branch protection e status check obbligatori. Se
il piano e la visibilità del repository privato supportano i required reviewer,
configurarne uno e disabilitare il bypass amministrativo solo quando esiste un
secondo operatore fidato. Con un singolo operatore non abilitare "prevent
self-review": provocherebbe un deadlock anche durante il recupero manuale. Se
queste regole non sono disponibili, registrare il rischio residuo e fare
affidamento congiunto su selezione di `main`, branch protection, guardie rigide
dei workflow, subject OIDC immutabile e concurrency condivisa. Quando un
required reviewer è attivo, anche il job retention schedulato può attendere
l'approvazione.

Inserire nell'environment esattamente queste sei variabili; sono identificatori
o configurazioni non segrete:

| Variabile | Significato |
| --- | --- |
| `AZURE_CLIENT_ID` | Client ID dell'applicazione Microsoft Entra federata |
| `AZURE_TENANT_ID` | Tenant ID Microsoft Entra |
| `AZURE_SUBSCRIPTION_ID` | Sottoscrizione che contiene il resource group |
| `AZURE_RESOURCE_GROUP` | Resource group di produzione |
| `AZURE_CONTAINER_APP_NAME` | Nome della Container App esistente |
| `PUBLIC_BASE_URL` | Origine HTTPS canonica, usata solo dal deploy |

I workflow correnti usano **zero GitHub environment secrets**. Non salvare in
GitHub un Azure client secret, il PAT runtime, `ADMIN_PASSWORD_HASH` o
`SESSION_KEY_BASE64`. `GHCR_TOKEN`, `ADMIN_PASSWORD_HASH` e
`SESSION_KEY_BASE64` sono input runtime protetti gestiti in Azure; non sono
input dei workflow correnti. `${{ github.token }}` (`GITHUB_TOKEN`) è invece il
token automatico ed effimero del singolo job Actions.

I permessi restano minimi e distinti:

- publish: `contents: read`, `packages: write`;
- deploy: `contents: read`, `id-token: write`;
- retention: `contents: read`, `id-token: write`, `packages: write`.

Deploy e retention usano entrambi il gruppo concurrency `production-deploy`
con cancellazione disabilitata. Riferimenti ufficiali: [GitHub environments e
protection rules](https://docs.github.com/en/actions/how-tos/deploy/configure-and-manage-deployments/manage-environments)
e [permessi di
`GITHUB_TOKEN`](https://docs.github.com/en/actions/security-for-github-actions/security-guides/automatic-token-authentication).

## Bootstrap OIDC immutabile

Per repository creati dopo il 15 luglio 2026 GitHub usa per impostazione
predefinita il subject immutabile adottato qui. Il subject esatto è:

```text
repo:francescostumpo@55147498/legal-callegarin@1365534753:environment:production
```

L'audience è `api://AzureADTokenExchange`. Il ruolo è **Container Apps
Contributor**, ID `358470bc-b998-42bd-ab17-a7e34c199c0f`, assegnato soltanto al
resource group di produzione. Non assegnare un ruolo ampio né un ruolo a scope
subscription. `scripts/bootstrap-github-oidc.sh` verifica il default OIDC
immutabile di GitHub, ma non configura l'impostazione OIDC: non modificarla e
non sostituirla con un subject personalizzato.

Sul resource group è assegnato soltanto Container Apps Contributor.

Precondizioni per l'operatore: login Azure sulla sottoscrizione e tenant
corretti; autorità Microsoft Entra per creare/verificare applicazione, service
principal e federated credential; autorità
`Microsoft.Authorization/roleAssignments/read|write` sul resource group;
accesso autenticato e protetto alle API GitHub. Il live bootstrap richiede
`GITHUB_API_TOKEN`: deve essere un PAT fine-grained selezionato soltanto per
`francescostumpo/legal-callegarin` con repository permission `Actions: read`;
l'accesso metadata è implicito. È una credenziale operatore effimera e
`GITHUB_API_TOKEN` non è un GitHub environment secret, non è
`${{ github.token }}`/`GITHUB_TOKEN`, non è il PAT classic runtime ACA e non è
un Azure client secret. Non salvarlo in argv, file, log, ticket o chat.

L'operatore deve prima eseguire il dry-run, che non richiede il token né Azure
CLI e non modifica alcun sistema:

```sh
./scripts/bootstrap-github-oidc.sh \
  --subscription-id '<subscription-uuid>' \
  --tenant-id '<tenant-uuid>' \
  --resource-group '<resource-group-name>' \
  --github-owner-id 55147498 \
  --github-repository-id 1365534753 \
  --application-display-name '<application-display-name>' \
  --federated-credential-name '<federated-credential-name>' \
  --dry-run
```

### OPERATORE — MUTATIVO — NON ESEGUITO DA CODEX

Solo dopo aver revisionato dry-run, account e autorità, l'operatore può
eseguire separatamente la forma mutativa. Questo esempio acquisisce il token
con prompt nascosto dentro un sottoprocesso; il parent shell lo rimuove prima e
dopo e non riceve mai il valore. `set +x` deve precedere l'acquisizione:

```bash
set +x
unset GITHUB_API_TOKEN
(
  set +x
  set -e
  read -r -s -p 'GitHub bootstrap token: ' GITHUB_API_TOKEN
  printf '\n' >&2
  export GITHUB_API_TOKEN
  trap 'unset GITHUB_API_TOKEN' EXIT HUP INT TERM

  ./scripts/bootstrap-github-oidc.sh \
    --subscription-id '<subscription-uuid>' \
    --tenant-id '<tenant-uuid>' \
    --resource-group '<resource-group-name>' \
    --github-owner-id 55147498 \
    --github-repository-id 1365534753 \
    --application-display-name '<application-display-name>' \
    --federated-credential-name '<federated-credential-name>'
)
unset GITHUB_API_TOKEN
```

In alternativa il token può essere iniettato nel medesimo sottoprocesso da un
secret manager protetto, mantenendo trap, assenza di xtrace e `unset` finale.
Lo script copia e fa subito `unset` della variabile ricevuta, autentica entrambi
i GET GitHub passando l'header a curl su stdin e cancella la copia privata prima
di avviare Node o Azure.

Mappare l'output finale `AZURE_CLIENT_ID=<client_id>` alla variabile GitHub
`AZURE_CLIENT_ID`; mappare gli argomenti verificati alle variabili
`AZURE_TENANT_ID`, `AZURE_SUBSCRIPTION_ID` e `AZURE_RESOURCE_GROUP`. Il client
ID non è segreto. `AZURE_CONTAINER_APP_NAME` proviene dall'output Bicep
`containerAppName`; `PUBLIC_BASE_URL` è l'origine HTTPS canonica già verificata
nel runbook dominio. Lo script conclude con un audit completo di applicazione,
service principal, credenziali federate e unica assegnazione RBAC. Nessun
application password o password del service principal viene creato. Vedere
[OpenID Connect da GitHub Actions verso
Azure](https://docs.github.com/en/actions/how-tos/secure-your-work/security-harden-deployments/oidc-in-azure)
e [federazione delle identità workload di Microsoft
Entra](https://learn.microsoft.com/en-us/entra/workload-id/workload-identity-federation).

## Deploy applicativo e rollback

Il workflow di produzione parte da un push a `main` o da
`workflow_dispatch`. Pubblica l'immagine privata con il solo tag SHA del commit,
verifica il digest prodotto contro quello risolto dal registry e passa al deploy
soltanto il riferimento immutabile `@sha256`. Il job protetto accede ad Azure
con OIDC, crea una revisione candidata e verifica liveness, readiness, pagina
pubblica e assenza di cookie pubblici. Solo dopo gli smoke check assegna il 100%
alla revisione candidata per nome; un fallimento successivo tenta il rollback
automatico alla revisione precedente verificata.

Il bootstrap iniziale Bicep resta un'operazione revisionata dall'operatore e
avvia l'applicazione in modalità `compat`. Soltanto un successivo rollout
approvato tramite il deploy engine crea una revisione `migrate`. Seguire il
[runbook dello schema articoli](article-storage-rollout.md) e rispettarne il
confine logico di compatibilità.

Per un rollback manuale, leggere prima il traffic routing esatto dell'app e per
ogni revisione candidata verificare insieme traffic, image, mode e health. Il
nome revisione restituito da Azure è opaco: non ricostruirlo. Non usare
`latest`, label/tag, nomi dedotti, artefatti precedenti al confine di
compatibilità o PUT parziali della Container App.

```sh
az containerapp show \
  --subscription '<subscription-uuid>' \
  --resource-group '<resource-group-name>' \
  --name '<container-app-name>' \
  --query '{activeRevisionsMode:properties.configuration.activeRevisionsMode,traffic:properties.configuration.ingress.traffic}' \
  --output json

az containerapp revision list \
  --subscription '<subscription-uuid>' \
  --resource-group '<resource-group-name>' \
  --name '<container-app-name>' \
  --all \
  --query '[].{name:name,active:properties.active,health:properties.healthState,image:properties.template.containers[0].image,mode:properties.template.containers[0].env[?name==`ARTICLE_STORAGE_SCHEMA_MODE`].value|[0]}' \
  --output table

az containerapp revision show \
  --subscription '<subscription-uuid>' \
  --resource-group '<resource-group-name>' \
  --name '<container-app-name>' \
  --revision '<azure-returned-verified-revision-name>' \
  --query '{active:properties.active,health:properties.healthState,image:properties.template.containers[0].image,mode:properties.template.containers[0].env[?name==`ARTICLE_STORAGE_SCHEMA_MODE`].value|[0]}' \
  --output json
```

Procedere soltanto se `activeRevisionsMode` è esattamente `Multiple`. Muovere
traffico soltanto verso una revisione sana, realmente trattenuta e non
precedente al confine di compatibilità, usando il nome opaco restituito da
Azure. Impostare `CURRENT_ROUTED_REVISION` sull'unico nome esplicito che riceve
traffico e `ROLLBACK_REVISION` sul target verificato:

```sh
CURRENT_ROUTED_REVISION='<azure-returned-current-routed-revision-name>'
ROLLBACK_REVISION='<azure-returned-verified-rollback-revision-name>'

az containerapp ingress traffic set \
  --subscription '<subscription-uuid>' \
  --resource-group '<resource-group-name>' \
  --name '<container-app-name>' \
  --revision-weight "$CURRENT_ROUTED_REVISION=0" "$ROLLBACK_REVISION=100"
```

Il rollback non annulla le scritture già effettuate. Dopo il rollback rileggere
app, traffico e revisione esatta, poi ripetere gli smoke check e il controllo
di assenza di `Set-Cookie` sulle pagine pubbliche. Confermare che la revisione
precedentemente instradata sia a 0 e il target verificato a 100. Per revisioni
e traffico vedere [Azure Container Apps
revisions](https://learn.microsoft.com/en-us/azure/container-apps/revisions)
e [traffic
splitting](https://learn.microsoft.com/en-us/azure/container-apps/traffic-splitting).

## GHCR privato, credenziale runtime e retention

La pull privata della Container App richiede un **personal access token
(classic)** con il solo scope `read:packages`; non è un fine-grained token.
Creare prima il nuovo PAT nelle impostazioni GitHub, inserirlo soltanto nel
secret app-scoped ACA `ghcr-token` mediante input protetto, quindi creare una
nuova revisione e verificarne la pull privata e la salute. Revocare il vecchio
PAT solo dopo la promozione riuscita. La modifica del secret ACA da sola non
crea una revisione e non aggiorna quelle in esecuzione.

La Container App conserva il PAT soltanto nel secret `ghcr-token`.
Creazione, smoke check e promozione della revisione usano il deploy engine
revisionato con soli argomenti non segreti.

Non inserire mai il PAT nel repository, nell'environment GitHub, in file
parametri, argv, log, ticket o chat. I file Bicep locali ignorati lo leggono
dal solo ambiente operatore e Azure lo conserva come secret della Container
App. Consultare [autenticazione del Container registry
GitHub](https://docs.github.com/en/packages/working-with-a-github-packages-registry/working-with-the-container-registry)
e [gestione dei secret di Azure Container
Apps](https://learn.microsoft.com/en-us/azure/container-apps/manage-secrets).

Publish e retention usano `GITHUB_TOKEN`, automatico ed effimero; il
`GITHUB_TOKEN` non è il PAT runtime. Il package deve essere collegato al
repository e concedere ad Actions accesso `admin`: la cancellazione REST con
`GITHUB_TOKEN` è attualmente in public preview. Verificare queste condizioni
prima del primo apply; vedere [accesso dei workflow ai
package](https://docs.github.com/en/packages/learn-github-packages/configuring-a-packages-access-control-and-visibility)
e [eliminazione/ripristino dei
package](https://docs.github.com/en/packages/learn-github-packages/deleting-and-restoring-a-package).

La retention gira ogni domenica alle 03:17 UTC e supporta anche apply manuale.
La policy conserva l'unione del digest instradato, dei 10 package più nuovi e
di quelli creati negli ultimi 30 giorni; nessun tag costituisce un pin o una
allowlist. I limiti rigidi sono 100 pagine, 10.000 versioni, 100 eliminazioni e
1 MiB di output. Il job condivide `production-deploy`, verifica nuovamente lo
stato prima della prima cancellazione e fallisce chiuso su drift o risposta
ambigua. Dopo un'eliminazione parziale interrompe il lavoro e richiede review
operatore. Un package version eliminato può essere ripristinabile per 30 giorni
se il namespace resta libero, ma il ripristino è deliberato e non sostituisce
la verifica fail-closed.

Il ruolo operativo **Operatore Pacchetti** controlla i workflow falliti e
schedulati, e rivede mensilmente GHCR usage e lista package. Configura inoltre
gli avvisi GitHub Packages al 90% e 100% dell'uso incluso di storage/bandwidth.
L'**Operatore Costi Azure** resta responsabile di action group, budget e alert
Azure: un avviso non ferma né limita automaticamente la spesa.

## Secret applicativi e recupero credenziali

`ADMIN_USERNAME` è configurazione non segreta. `ADMIN_PASSWORD_HASH` è un PHC
Argon2id segreto: sostituirlo cambia la credential version e rende non valide
le sessioni precedenti. `SESSION_KEY_BASE64` è un secret con almeno 32 byte
dopo la decodifica; la sua rotazione invalida tutte le sessioni e richiede la
stessa disciplina secret protetto/nuova revisione. Per password dimenticata o
compromessa seguire esclusivamente [recupero password
amministratore](password-recovery.md).

## Gate esterni e responsabilità

Nessuna attività esterna di questa checklist è stata eseguita da Codex o dai
task di implementazione. Prima della produzione un operatore deve registrare:

- disponibilità del GitHub plan e delle protection rule richieste;
- collegamento del package, accesso package `admin` e rischio public preview;
- esito reale di Actions per CI, publish, deploy e retention;
- login OIDC live e audit dell'unico ruolo sul resource group;
- private image pull e prova completa di rotazione/revoca PAT;
- Azure what-if e deployment Bicep revisionati;
- rollback drill su una revisione trattenuta compatibile;
- propagazione DNS/TLS e rinnovo certificati;
- alert delivery Azure/GitHub e comportamento reale di cost/usage;
- storage recovery rehearsal, incluse copie e ripristino;
- contenuti approvati dall'avvocato prima della pubblicazione.
