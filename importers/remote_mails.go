package importers

import (
	"encoding/json"
	"encoding/xml"
	"io"
	"log"
	"mensadb/tools/env"
	"net/http"
	"os"
	"slices"
	"strings"
)

// Forwarding rappresenta la sezione "forwarding" restituita dall'API remota.
//
// Enabled: tipicamente "true"/"false" (stringa perché arriva così dall'XML).
// Address: elenco di indirizzi verso cui l'account inoltra la posta.
//
// Nota: i tag xml/json permettono di riusare la stessa struct sia per Unmarshal XML
// sia per Marshal/Unmarshal JSON (cache locale su file).
type Forwarding struct {
	Enabled string   `xml:"enabled" json:"enabled"`
	Address []string `xml:"address" json:"address"`
}

// MailName descrive un account di posta ("mailname") e le relative impostazioni.
//
// Name è l'alias locale (es: "segreteria"), senza dominio.
// Forwarding contiene la regola di inoltro e gli eventuali indirizzi di destinazione.
type MailName struct {
	Id         string     `xml:"id" json:"id"`
	Name       string     `xml:"name" json:"name"`
	Forwarding Forwarding `xml:"forwarding" json:"forwarding"`
}

// MailEntry è un singolo elemento della lista restituita da get_info.
// Status indica lo stato dell'entry lato pannello (es: "ok").
type MailEntry struct {
	Status   string   `xml:"status" json:"status"`
	MailName MailName `xml:"mailname" json:"mailname"`
}

// MailInfo contiene la lista dei risultati ("result").
type MailInfo struct {
	Result []MailEntry `xml:"result" json:"result"`
}

// Mail è il wrapper XML/JSON per la risposta dell'endpoint relativamente all'area "mail".
// Il tag xml:"get_info" mappa la sezione omonima dell'XML.
type Mail struct {
	MailInfo MailInfo `xml:"get_info" json:"get_info"`
}

// Container è il root object della risposta.
// Serve perché l'XML torna annidato come <packet><mail><get_info>...</get_info></mail></packet>
// e qui interessa soprattutto <mail>.
type Container struct {
	Mail Mail `xml:"mail" json:"mail"`
}

// GetFullMailList interroga l'endpoint remoto (Plesk/Enterprise Agent) per ottenere
// la lista completa degli account e della configurazione di forwarding.
//
// Il risultato viene salvato su disco in formato JSON come cache locale ("mails.json"),
// così le successive chiamate possono leggere dal file invece di fare sempre una chiamata HTTP.
//
// Importante:
// - Usa credenziali hard-coded via header HTTP_AUTH_* (come richiesto dall'endpoint).
// - In caso di errori durante la request, termina il processo con log.Fatal.
func GetFullMailList() {

	const myurl = "https://michael.mensaitalia.it:8443/enterprise/control/agent.php"
	// Richiesta XML per get_info, filtrata per site-id=2 e con inclusione del blocco <forwarding />.
	const xmlbody = `<?xml version="1.0" encoding="UTF-8"?><packet><mail><get_info><filter><site-id>2</site-id></filter><forwarding /></get_info></mail></packet>`

	request, err := http.NewRequest("POST", myurl, strings.NewReader(xmlbody))
	if err != nil {
		log.Println("remote_mails: create request:", err)
		return
	}

	// L'endpoint si aspetta XML in POST e autenticazione via header custom.
	request.Header.Set("Content-Type", "text/xml; charset=UTF-8")
	request.Header.Set("HTTP_AUTH_LOGIN", "dev")
	request.Header.Set("HTTP_AUTH_PASSWD", env.GetArea32InternalEmail())

	client := &http.Client{}
	resp, err := client.Do(request)
	if err != nil {
		log.Println("remote_mails: do request:", err)
		return
	}
	defer func() {
		// Non terminiamo il processo per errori di chiusura della response body,
		// ma li logghiamo perché possono indicare problemi di rete/transport.
		if cerr := resp.Body.Close(); cerr != nil {
			log.Printf("remote_mails: close response body: %v", cerr)
		}
	}()

	// Leggo e converto: XML -> struct -> JSON (per cache locale).
	xmlResult, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Println("remote_mails: read response body:", err)
		return
	}
	var container Container
	err = xml.Unmarshal([]byte(xmlResult), &container)
	if err != nil {
		log.Println("remote_mails: unmarshal xml:", err)
		return
	}
	bt, err := json.Marshal(container)
	if err != nil {
		log.Println("remote_mails: marshal to json:", err)
		return
	}
	// Salva su file in working directory.
	// Permessi 0644: owner read/write, group/others read.
	if err := os.WriteFile("mails.json", bt, 0644); err != nil {
		log.Println("remote_mails: write mails.json:", err)
		return
	}
}

// ReadFromJson carica la cache locale "mails.json" e la deserializza in Container.
//
// Se il file non esiste:
//  1. scarica i dati con GetFullMailList()
//  2. riprova a leggere ricorsivamente dal file appena creato.
//
// Nota: altri errori di apertura file (permessi, I/O, ecc.) non vengono gestiti in modo
// esplicito e possono causare panics/valori vuoti a seconda dei casi.
func ReadFromJson() *Container {
	jsonFile, err := os.Open("mails.json")
	if err != nil {
		if os.IsNotExist(err) {
			// Cache mancante: la creo scaricando dal server e poi rileggo.
			GetFullMailList()
			return ReadFromJson()
		}
		// Per errori diversi da "file non esiste" falliamo esplicitamente.
		log.Println("remote_mails: open mails.json:", err)
		return nil
	}
	defer func() {
		if cerr := jsonFile.Close(); cerr != nil {
			log.Printf("remote_mails: close mails.json: %v", cerr)
		}
	}()

	var container Container
	jsonParser := json.NewDecoder(jsonFile)
	if err := jsonParser.Decode(&container); err != nil {
		log.Println("remote_mails: decode mails.json:", err)
		return nil
	}

	return &container
}

// RetrieveForwardedMail risolve ricorsivamente la catena di forwarding per un alias.
//
// Input:
//   - name: nome dell'account senza dominio (es: "segreteria" per "segreteria@mensa.it")
//   - alreadyChecked: lista di alias già visitati durante la ricorsione, per evitare loop
//     (es: A inoltra a B e B inoltra ad A).
//
// Output:
//   - una lista di indirizzi (solo @mensa.it) trovati come destinazioni di inoltro.
//
// Esempio:
//
//	segreteria -> presidente@mensa.it -> board@mensa.it
//	ritorna ["presidente@mensa.it", "board@mensa.it"]
func RetrieveForwardedMail(name string, alreadyChecked ...string) (res []string) {
	container := ReadFromJson()
	if container == nil {
		log.Println("remote_mails: RetrieveForwardedMail: failed to read mails.json")
		return
	}

	for _, mailEntry := range container.Mail.MailInfo.Result {
		// Cerco l'account richiesto.
		if mailEntry.MailName.Name == name {
			// Anti-loop: se l'ho già visitato in questa chain, interrompo.
			if slices.Contains(alreadyChecked, name) {
				return
			}

			for _, address := range mailEntry.MailName.Forwarding.Address {
				// Filtra solo indirizzi interni @mensa.it.
				// Gli inoltri verso domini esterni vengono ignorati.
				if !strings.Contains(address, "@mensa.it") {
					continue
				}

				// Aggiungo la destinazione diretta...
				res = append(res, address)

				// ...e continuo a risolvere eventuali forward dell'alias di destinazione (parte prima della @).
				res = append(res, RetrieveForwardedMail(
					strings.Split(address, "@")[0],
					append(alreadyChecked, name)...,
				)...)
			}
		}
	}

	return res
}

// RetrieveAliasFromMail trova l'alias @mensa.it che inoltra verso un determinato indirizzo.
//
// In pratica: scorre tutte le regole di forwarding e, se una destinazione coincide con "mail"
// (case-insensitive e whitespace-trim), restituisce "<alias>@mensa.it".
//
// Se nessun alias inoltra verso quell'indirizzo, ritorna stringa vuota.
func RetrieveAliasFromMail(mail string) (res string) {
	container := ReadFromJson()
	if container == nil {
		log.Println("remote_mails: RetrieveAliasFromMail: failed to read mails.json")
		return ""
	}

	for _, mailEntry := range container.Mail.MailInfo.Result {
		for _, address := range mailEntry.MailName.Forwarding.Address {
			// Confronto robusto: trim + lowercase.
			if strings.TrimSpace(strings.ToLower(address)) == strings.TrimSpace(strings.ToLower(mail)) {
				return mailEntry.MailName.Name + "@mensa.it"
			}
		}
	}

	return ""
}
