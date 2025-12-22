package cs

import (
	"mensadb/main/hooks"
	"mensadb/tools/dbtools"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/tidwall/gjson"
)

// MembersHashedHandler espone un endpoint che restituisce un elenco di record della collezione
// `members_registry` (solo quelli attivi) trasformando *tutti* i valori in hash MD5.
//
// Obiettivo pratico:
// - consentire ad un client di sincronizzare/validare dati sensibili senza ricevere i valori in chiaro.
// - mantenere una struttura JSON simile all'originale, ma con chiavi normalizzate e valori hashed.
//
// Meccanismo di sicurezza:
// - richiede una chiave di autorizzazione (Authorization header o query param `authKey`).
// - la chiave viene validata tramite hooks.CheckKey con permesso "GET_MEMBERS_HASH".
//
// Forma della risposta:
// - array di oggetti; ogni oggetto rappresenta un membro.
// - ogni oggetto contiene i campi originali (con chiavi normalizzate) ma con valori MD5(salt+value).
// - viene aggiunto un campo `salt` per record, derivato dall'ID del record.
func MembersHashedHandler(e *core.RequestEvent) error {
	// Recupero della chiave di autorizzazione.
	// 1) Preferiamo l'header standard `Authorization`.
	// 2) In fallback accettiamo anche `authKey` in query string (utile per debug/integrazioni semplici).
	authKey := e.Request.Header.Get("Authorization")
	if authKey == "" {
		authKey = e.Request.URL.Query().Get("authKey")
	}

	// Blocco immediato se la chiave non è valida o non ha la capability richiesta.
	// Nota: "GET_MEMBERS_HASH" rappresenta la specifica azione/permesso controllato nel progetto.
	if !hooks.CheckKey(e.App, authKey, "GET_MEMBERS_HASH") {
		return e.String(401, "Unauthorized")
	}

	app := e.App

	// Recupera tutti i record attivi dalla collezione `members_registry`.
	// dbx.NewExp("is_active = true") traduce la condizione SQL/DB sottostante.
	records, err := app.FindAllRecords("members_registry", dbx.NewExp("is_active = true"))
	if err != nil {
		return err
	}

	// Prepariamo la risposta come array di map generiche.
	// Usare `any` permette di costruire dinamicamente oggetti e sotto-oggetti.
	var finalData []map[string]any = make([]map[string]any, 0)

	for _, record := range records {
		// Convertiamo il record PocketBase in JSON per poterlo attraversare in modo generico.
		// Questo approccio evita di dover conoscere preventivamente lo schema.
		json, err := record.MarshalJSON()
		if err != nil {
			return err
		}

		// Parsing JSON con gjson: fornisce accesso rapido a mappe/oggetti annidati.
		elems := gjson.ParseBytes(json)

		// Salt per-record:
		// - viene derivato dall'ID del record.
		// - usato come "chiave"/sale per hashare i valori.
		// Importante: non è un salt segreto (viene infatti *restituito* al client).
		// Serve principalmente a differenziare gli hash tra record diversi e rendere meno utili
		// confronti diretti tra dataset differenti.
		salt := dbtools.GetMD5Hash(record.Id, "")

		// Ricorsivamente:
		// - normalizziamo le chiavi (es. case/accents/spazi) per avere una forma stabile.
		// - hashiamo ogni valore scalare con MD5(value, salt).
		data := recurseMap(elems.Map(), salt)

		// Aggiungiamo il salt in chiaro per questo record.
		// Così un client può ricalcolare gli hash dei valori noti localmente e confrontarli.
		data["salt"] = salt

		finalData = append(finalData, data)
	}

	return e.JSON(200, finalData)

}

// recurseMap attraversa ricorsivamente un oggetto JSON rappresentato come map[string]gjson.Result.
//
// Regole di trasformazione:
// - Per ogni chiave:
//   - la chiave viene normalizzata con dbtools.NormalizeTextForHash(key)
//     (tipicamente per ridurre differenze di casing/punteggiatura/spazi).
//
// - Per ogni valore:
//   - se è un oggetto JSON annidato, si ricorre su quell'oggetto.
//   - altrimenti si converte in stringa e si calcola l'hash MD5 usando `salt`.
//
// Nota sui tipi JSON:
// - gjson.Result può rappresentare stringhe, numeri, booleani, null, array e oggetti.
// - in questa implementazione:
//   - gli oggetti vengono gestiti con ricorsione.
//   - tutto ciò che non è oggetto viene trattato come scalare e trasformato tramite value.String().
//   - gli array NON vengono gestiti esplicitamente: value.IsObject() sarà false e quindi l'array
//     verrà hashato come stringa (es. "[...]"), comportamento che potrebbe essere voluto o meno.
//     Se si volesse preservare la struttura degli array, andrebbe aggiunta una gestione dedicata.
func recurseMap(data map[string]gjson.Result, salt string) map[string]any {
	finalData := make(map[string]any)

	for key, value := range data {
		normalizedKey := dbtools.NormalizeTextForHashAndRemoveSpecialCharsOrAccents(key)

		// Caso: sotto-oggetto JSON => ricorsione.
		if value.IsObject() {
			finalData[normalizedKey] = recurseMap(value.Map(), salt)
			continue
		}

		// Caso: valore scalare (o array/non-oggetto) => hash.
		// GetMD5Hash(value, salt) tipicamente concatena/combina valore e salt prima di hashare.
		finalData[normalizedKey] = dbtools.GetMD5Hash(value.String(), salt)
	}

	return finalData
}
