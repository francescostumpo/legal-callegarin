# Operazioni

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
