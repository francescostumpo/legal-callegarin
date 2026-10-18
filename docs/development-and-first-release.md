# Sviluppo e primo rilascio

Questa è la guida ordinata per preparare un candidato locale e interrompere il
circolo vizioso del primo rilascio: `main.bicep` richiede già un'immagine GHCR
privata con digest immutabile, mentre il workflow ordinario pubblica e poi
distribuisce assumendo che la Container App esista. Il solo cycle breaker
approvato è una pubblicazione locale una tantum, eseguita da un operatore.

> **Stato produzione: BLOCKED.** Tutte le mutazioni GitHub, GHCR, Azure e DNS
> descritte qui sono operator-only, non sono state eseguite e richiedono
> autorizzazione ed evidenza reali. Nessuna chiamata live è stata effettuata.
> Restano inoltre obbligatori i gate separati di
> [`launch-readiness.md`](launch-readiness.md) e
> [`storage-recovery.md`](storage-recovery.md).

## Prerequisiti

La parità con build e CI richiede esattamente:

- Go `1.27.1`;
- Node.js `24.21.0`, con npm proveniente dallo stesso toolchain Node;
- Azure CLI capace di installare la versione esatta di Bicep `0.45.15`.

Servono inoltre Git e Make; Docker Engine e Docker CLI con le capacità Buildx e
Compose; bash e strumenti POSIX; curl, dig e OpenSSL. Nessun minimo numerico
ulteriore viene inventato per Docker o Azure CLI: contano le capacità indicate
e le verifiche effettive. I primi download di dipendenze e browser e i primi
pull di container possono richiedere accesso di rete e privilegi della
piattaforma.

## Cosa copre `make check`

`make check` esegue, nell'ordine, la workflow policy, `npm ci`, il controllo
Prettier, TypeScript, i test Vitest e il build frontend di produzione. Solo dopo
la generazione degli asset esegue i contratti Node, il controllo gofmt
fail-on-difference, `go vet`, Staticcheck, i test Go con i tag predefiniti e il
build Go pulito.

Non include actionlint, i test Go con race detector, Playwright, l'integrazione
Azurite, il container smoke, la generazione SBOM o lo scan delle vulnerabilità.
Questi gate distinti fanno parte della matrice seguente.

## Matrice locale del release candidate

### 1. Vincolo al commit e controlli statici

Eseguire tutto dalla radice del repository. Prima dei gate, l'output del primo
comando deve essere vuoto; file ignorati o generati non dimostrano da soli la
pulizia del worktree.

```bash
set -euo pipefail
test -z "$(git status --porcelain --untracked-files=all)"
RELEASE_COMMIT="$(git rev-parse HEAD)"
test "${#RELEASE_COMMIT}" -eq 40

npm ci
make check
make actionlint
go test -race -count=1 ./...
```

### 2. Browser locali

Usare sempre il Playwright repository-local, non un'installazione globale. Il
primo install può richiedere rete e privilegi per i pacchetti di sistema.

```bash
./node_modules/.bin/playwright install --with-deps chromium firefox webkit
npm run e2e -- --project=chromium
npm run e2e:smoke
```

Il primo comando di test esegue la suite completa su Chromium; il secondo
esegue gli smoke sulle tre engine.

### 3. Integrazione Azurite con teardown garantito

Eseguire questo blocco in un sottoprocesso dedicato. Il trap garantisce il
teardown `down --volumes --remove-orphans` anche in caso di errore. La
connection string è esclusivamente il fixture sintetico pubblico di Azurite
versionato nella CI.

```bash
(
  set -euo pipefail
  azurite_cleanup() {
    docker compose -f compose.test.yaml down --volumes --remove-orphans
  }
  trap azurite_cleanup EXIT HUP INT TERM

  docker compose -f compose.test.yaml up --detach azurite
  if ! timeout 60s bash -c '
    until
      curl --silent --show-error --output /dev/null --max-time 2 \
        "http://127.0.0.1:11000/devstoreaccount1?comp=list" &&
      curl --silent --show-error --output /dev/null --max-time 2 \
        "http://127.0.0.1:11002/devstoreaccount1/Tables"
    do
      sleep 1
    done
  '; then
    docker compose -f compose.test.yaml ps
    docker compose -f compose.test.yaml logs --no-color azurite
    exit 1
  fi

  export AZURITE_CONNECTION_STRING='DefaultEndpointsProtocol=http;AccountName=devstoreaccount1;AccountKey=Eby8vdM02xNOcqFlqUwJPLlmEtlCDXJ1OUzFT50uSRZ6IFsuFq2UVErCz4I6tq/K1SZFPTOtr/KBHBeksoGMGw==;BlobEndpoint=http://127.0.0.1:11000/devstoreaccount1;TableEndpoint=http://127.0.0.1:11002/devstoreaccount1;'
  go test -race -count=1 -tags=integration \
    ./internal/storage/azure ./internal/storage/contracttest
)
```

Se la readiness non arriva entro 60 secondi, raccogliere `docker compose ps` e
i log di Azurite prima di lasciare eseguire il trap; non proseguire con gli
altri gate.

### 4. Immagine locale, SBOM e scan

`make container-smoke` è ammesso soltanto sullo stesso HEAD esatto e con
worktree pulito: lo script rifiuta qualsiasi differenza, inclusi gli untracked.
L'immagine verificata dal smoke ha il nome predefinito
`legal-callegarin:local`; usare proprio quella, senza ricostruirla tra i gate.

```bash
make container-smoke
RC_IMAGE='legal-callegarin:local'
make sbom IMAGE="$RC_IMAGE"
make scan IMAGE="$RC_IMAGE" SEVERITY=HIGH,CRITICAL
```

Un fallimento blocca il candidato. L'SBOM generato è un artefatto locale: non
costituisce da solo prova di worktree pulito né autorizza la pubblicazione.

### 5. Bicep esatto e warning-free

Usare una configurazione Azure CLI temporanea, disabilitare soltanto il check
remoto della versione e verificare la versione installata. Questo gate compila
gli esempi versionati prima che esistano secret reali: assegna alle tre
variabili valori sintetici, effimeri e destinati solo alla compilazione. Non
sono credenziali valide, non vanno mai riutilizzati in produzione e scompaiono
con il sottoprocesso.

Il wrapper conserva lo stderr di ogni comando soltanto in un file temporaneo
protetto: fallisce sia se il comando restituisce un errore, sia se quel file
non è vuoto, senza stamparne il contenuto. Per `build-params`, lo stdout viene
scartato direttamente e né stdout né stderr dei parametri compilati persistono
dopo il gate.

```bash
(
  set -euo pipefail
  umask 077
  export AZURE_BICEP_CHECK_VERSION=false
  export AZURE_CONFIG_DIR="$(mktemp -d)"
  export DOTNET_BUNDLE_EXTRACT_BASE_DIR="$(mktemp -d)"
  BICEP_STDERR="$AZURE_CONFIG_DIR/bicep-stderr"

  bicep_cleanup() {
    status=$?
    trap - EXIT HUP INT TERM
    unset GHCR_TOKEN ADMIN_PASSWORD_HASH SESSION_KEY_BASE64 BICEP_VERSION_OUTPUT
    rm -f -- "$BICEP_STDERR"
    chmod -R u+rwX "$AZURE_CONFIG_DIR" "$DOTNET_BUNDLE_EXTRACT_BASE_DIR" \
      2>/dev/null || true
    rm -rf -- "$AZURE_CONFIG_DIR" "$DOTNET_BUNDLE_EXTRACT_BASE_DIR"
    exit "$status"
  }
  trap bicep_cleanup EXIT HUP INT TERM

  run_bicep_warning_free() {
    : > "$BICEP_STDERR"
    chmod 0600 "$BICEP_STDERR"
    if ! "$@" 2>"$BICEP_STDERR"; then
      printf 'comando Bicep non riuscito; diagnostica non mostrata\n' >&2
      return 1
    fi
    if [ -s "$BICEP_STDERR" ]; then
      printf 'comando Bicep con stderr non vuoto; diagnostica non mostrata\n' >&2
      return 1
    fi
  }

  GHCR_TOKEN='synthetic-compile-only-ghcr-token'
  ADMIN_PASSWORD_HASH='synthetic-compile-only-argon2id-phc'
  SESSION_KEY_BASE64='synthetic-compile-only-session-key'
  export GHCR_TOKEN ADMIN_PASSWORD_HASH SESSION_KEY_BASE64

  run_bicep_warning_free az config set bicep.use_binary_from_path=false \
    >/dev/null
  run_bicep_warning_free az bicep install --version v0.45.15 >/dev/null
  BICEP_VERSION_OUTPUT="$(run_bicep_warning_free az bicep version)"
  printf '%s\n' "$BICEP_VERSION_OUTPUT" |
    grep --fixed-strings 'Bicep CLI version 0.45.15 ' >/dev/null
  run_bicep_warning_free az bicep build --file infra/main.bicep --stdout \
    >/dev/null
  run_bicep_warning_free az bicep build --file infra/custom-domain.bicep --stdout \
    >/dev/null
  run_bicep_warning_free az bicep build-params --file infra/main.example.bicepparam --stdout >/dev/null
  run_bicep_warning_free az bicep build-params --file infra/custom-domain.example.bicepparam --stdout >/dev/null
)
```

Ogni warning, errore o output di versione diverso blocca il candidato. La
compilazione successiva dei file `.local.bicepparam` con parametri reali resta
un'attività operator-only della procedura Azure: usa secret protetti e non i
fixture sintetici di questo gate.

### 6. Chiusura delle evidenze

Dopo tutti i gate, confermare di essere ancora sullo stesso commit e che il
worktree sia pulito:

```bash
test "$(git rev-parse HEAD)" = "$RELEASE_COMMIT"
test -z "$(git status --porcelain --untracked-files=all)"
```

Registrare `git rev-parse HEAD` prima e dopo i gate e richiedere la CI green per
lo stesso esatto commit. Un risultato locale appartenente a un altro commit
non è trasferibile.

## Pubblicazione una tantum della prima immagine

Questa procedura operator-only è il solo cycle breaker; non è un workflow
publish-only. Creare un PAT classic separato ed effimero con il solo scope
`write:packages`. Non riutilizzare il PAT runtime ACA. Acquisirlo con input
nascosto e non esporlo mai in tracing, argv, log, ticket, chat o file. Eseguire
il blocco seguente soltanto dopo autorizzazione, su una workstation fidata e
sullo stesso commit già verificato.

```bash
set +x
unset BOOTSTRAP_GHCR_PAT
(
  set +x
  set -euo pipefail
  umask 077

  BOOTSTRAP_COMMIT="$(git rev-parse HEAD)"
  test "$BOOTSTRAP_COMMIT" = "$RELEASE_COMMIT"
  test -z "$(git status --porcelain --untracked-files=all)"
  IMAGE_REPOSITORY='ghcr.io/francescostumpo/legal-callegarin'
  IMAGE_TAG="$IMAGE_REPOSITORY:bootstrap-$BOOTSTRAP_COMMIT"

  BOOTSTRAP_DOCKER_CONFIG="$(mktemp -d)"
  chmod 0700 "$BOOTSTRAP_DOCKER_CONFIG"
  export DOCKER_CONFIG="$BOOTSTRAP_DOCKER_CONFIG"
  BOOTSTRAP_METADATA="$BOOTSTRAP_DOCKER_CONFIG/build-metadata.json"
  bootstrap_cleanup() {
    status=$?
    trap - EXIT HUP INT TERM
    docker logout ghcr.io >/dev/null 2>&1 || true
    unset BOOTSTRAP_GHCR_PAT BUILD_DIGEST REGISTRY_DIGEST REGISTRY_DIGEST_JSON
    rm -f -- "$BOOTSTRAP_METADATA"
    chmod -R u+rwX "$BOOTSTRAP_DOCKER_CONFIG" 2>/dev/null || true
    rm -rf -- "$BOOTSTRAP_DOCKER_CONFIG"
    unset DOCKER_CONFIG
    exit "$status"
  }
  trap bootstrap_cleanup EXIT HUP INT TERM

  read -r -s -p 'PAT GHCR bootstrap: ' BOOTSTRAP_GHCR_PAT
  printf '\n' >&2
  printf '%s' "$BOOTSTRAP_GHCR_PAT" |
    docker login ghcr.io \
      --username '<verified-github-operator>' --password-stdin
  unset BOOTSTRAP_GHCR_PAT

  docker buildx build \
    --pull \
    --platform linux/amd64 \
    --build-arg "VERSION=bootstrap-$BOOTSTRAP_COMMIT" \
    --build-arg "COMMIT=$BOOTSTRAP_COMMIT" \
    --label 'org.opencontainers.image.source=https://github.com/francescostumpo/legal-callegarin' \
    --label "org.opencontainers.image.revision=$BOOTSTRAP_COMMIT" \
    --metadata-file "$BOOTSTRAP_METADATA" \
    --tag "$IMAGE_TAG" \
    --push \
    .

  chmod 0600 "$BOOTSTRAP_METADATA"
  BUILD_DIGEST="$(
    node -e '
      const fs = require("node:fs");
      const metadata = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
      const digest = metadata["containerimage.digest"];
      if (typeof digest !== "string") process.exit(1);
      process.stdout.write(digest);
    ' "$BOOTSTRAP_METADATA"
  )"
  if [[ ! "$BUILD_DIGEST" =~ ^sha256:[0-9a-f]{64}$ ]]; then
    printf 'digest prodotto dal build non canonico\n' >&2
    exit 1
  fi

  REGISTRY_DIGEST_JSON="$(
    docker buildx imagetools inspect \
      --format '{{json .Manifest.Digest}}' "$IMAGE_TAG"
  )"
  if [[ ! "$REGISTRY_DIGEST_JSON" =~ ^\"sha256:[0-9a-f]{64}\"$ ]]; then
    printf 'digest GHCR non canonico\n' >&2
    exit 1
  fi
  REGISTRY_DIGEST="${REGISTRY_DIGEST_JSON#\"}"
  REGISTRY_DIGEST="${REGISTRY_DIGEST%\"}"
  if [[ ! "$REGISTRY_DIGEST" =~ ^sha256:[0-9a-f]{64}$ ]]; then
    printf 'digest GHCR non canonico\n' >&2
    exit 1
  fi
  if [[ "$REGISTRY_DIGEST" != "$BUILD_DIGEST" ]]; then
    printf 'digest prodotto e digest GHCR diversi\n' >&2
    exit 1
  fi
  IMAGE_REFERENCE="$IMAGE_REPOSITORY@$BUILD_DIGEST"
  printf 'Riferimento immutabile verificato: %s\n' "$IMAGE_REFERENCE"
)
unset BOOTSTRAP_GHCR_PAT
```

Il digest prodotto dal build e la risoluzione indipendente del digest dal
registry devono essere entrambi canonici `sha256:<64 lowercase hex>` e
coincidere esattamente. Il riferimento finale canonico
`@sha256:<64 lowercase hex>` usa quel valore verificato e l'immagine del commit
esatto. Se qualsiasi controllo fallisce, il trap esegue
logout e la rimozione sia del metadata file sia del `DOCKER_CONFIG` temporaneo
restrittivo; la procedura si arresta.

Nel pannello GitHub verificare poi, con evidenza separata, che il package sia
privato, collegato al repository corretto e che GitHub Actions abbia accesso
`admin`. Revocare immediatamente il PAT di bootstrap al termine della verifica,
anche se un passo successivo è bloccato. Riferimenti ufficiali:
[autenticazione GHCR](https://docs.github.com/en/packages/working-with-a-github-packages-registry/working-with-the-container-registry)
e [controllo di accesso dei package](https://docs.github.com/en/packages/learn-github-packages/configuring-a-packages-access-control-and-visibility).

## Tre credenziali distinte

Non sostituire né riutilizzare una credenziale con un'altra:

1. il bootstrap usa il PAT classic temporaneo `write:packages`, revocato subito;
2. il runtime ACA usa un PAT classic separato con solo `read:packages`, salvato
   esclusivamente nel secret app-scoped ACA `ghcr-token`;
3. il bootstrap OIDC usa `GITHUB_API_TOKEN`, un PAT fine-grained effimero,
   limitato al repository e al solo permesso `Actions: read`; eseguire `unset`
   subito dopo la verifica.

Il PAT runtime non va mai salvato in GitHub e il PAT OIDC non autorizza la
pubblicazione. Per i secret ACA vedere la
[documentazione Microsoft](https://learn.microsoft.com/en-us/azure/container-apps/manage-secrets);
per OIDC seguire la sezione dedicata di [`operations.md`](operations.md).

## Hash amministratore e chiave di sessione stabile

Su una workstation fidata, con tracing disabilitato, usare un sottoprocesso
protetto e una umask restrittiva. `go run ./cmd/adminhash` va eseguito senza
argomenti: legge la password da prompt nascosti e genera un PHC Argon2id. Il PHC
è un secret; catturarlo direttamente nel meccanismo protetto destinato ad
alimentare l'ambiente, senza persistenza di password o secret in argv, output,
file o cronologia.

Generare la session key una sola volta per il bootstrap e mantenerla stabile:

```bash
set +x
(
  set +x
  set -euo pipefail
  umask 077
  unset SESSION_KEY_BASE64 ADMIN_PASSWORD_HASH GHCR_TOKEN
  trap 'unset SESSION_KEY_BASE64 ADMIN_PASSWORD_HASH GHCR_TOKEN' EXIT HUP INT TERM

  SESSION_KEY_BASE64="$(openssl rand -base64 32 | tr -d '\n')"
  test "$(
    printf '%s' "$SESSION_KEY_BASE64" | openssl base64 -d -A | wc -c | tr -d ' '
  )" -eq 32
  read -r -s -p 'PAT GHCR runtime read-only: ' GHCR_TOKEN
  printf '\n' >&2
  ADMIN_PASSWORD_HASH="$(go run ./cmd/adminhash)"
  case "$ADMIN_PASSWORD_HASH" in
    '$argon2id$'*) ;;
    *) printf 'PHC Argon2id non valido\n' >&2; exit 1 ;;
  esac
  export SESSION_KEY_BASE64 ADMIN_PASSWORD_HASH GHCR_TOKEN
  # Eseguire qui build-params/what-if/deploy autorizzati; i file .bicepparam
  # usano readEnvironmentVariable e non contengono i valori.
)
```

La verifica conferma che il valore decodificato è lungo 32 byte senza stamparlo.
Iniettare
chiave, PHC e PAT runtime solo tramite i parametri environment-backed di Bicep
`SESSION_KEY_BASE64`, `ADMIN_PASSWORD_HASH` e `GHCR_TOKEN`; usare sempre un
input protetto per valorizzarli. Rigenerare o ruotare la session key invalida
tutte le sessioni amministrative. La procedura Argon2id completa è in
[`password-recovery.md`](password-recovery.md).

## Ordine obbligatorio del primo rilascio

Non saltare, scambiare o accorpare questi gate:

1. partire da un commit revisionato, worktree pulito, gate locali completi e CI
   green sullo stesso SHA;
2. l'environment GitHub `production` deve essere protetto prima che un workflow
   possa crearlo implicitamente;
3. predisporre con autorità verificata il resource group, OIDC e tutte le
   relative precondizioni, limitando il ruolo al resource group;
4. pubblicare la prima immagine una tantum e verificare il digest immutabile
   risolto da GHCR;
5. preparare il PAT runtime separato, l'hash Argon2id e la chiave di sessione
   stabile tramite input protetti;
6. eseguire il bootstrap `main.bicep` con `customDomainApex = ''` e modalità
   schema `compat`;
7. eseguire la normalizzazione obbligatoria del traffico: `latest=0` e 100%
   alla singola revisione sana nominata esattamente;
8. completare il rollout iniziale `migrate`, obbligatorio e non opzionale,
   quindi verificare il marker durevole e il repository current-only secondo
   [`article-storage-rollout.md`](article-storage-rollout.md); i workflow
   ordinari non vanno abilitati prima di `migrate`;
9. attendere la propagazione DNS e creare i certificati e binding del dominio
   personalizzato secondo [`operations.md`](operations.md);
10. ripetere la normalizzazione del traffico nominato dopo il deployment
    completo del dominio;
11. configurare e verificare le sei variabili di produzione, completare l'audit
    OIDC e solo allora abilitare il workflow di deploy ordinario;
12. abilitare la retention soltanto dopo aver provato traffico nominato e
    permessi package.

Il deploy ordinario forza revisioni `migrate`, ma non sostituisce il rollout
iniziale controllato: prima vanno provati compatibilità, marker durevole e
lettura current-only. La procedura Azure e DNS dettagliata, incluse le verifiche
live e i rollback, resta in [`operations.md`](operations.md).
