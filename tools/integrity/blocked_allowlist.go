package integrity

import (
	"net/http"
	"net/url"
	"path"
	"strings"
)

// Allowlist degli utenti "blocked".
//
// Un utente sospeso resta un socio valido: deve continuare a poter portare a
// termine il flusso di conferma delle app esterne (un partner, ad esempio il bot
// Telegram, chiama /api/cs/exapp/request con la sua chiave ex_keys; il socio
// riceve una push, l'app apre il foglio di conferma, scrive i permessi in
// ex_granted_permissions e fa la POST di callback all'URL del partner, non a
// noi). Tutto il resto resta 403 account_blocked.
//
// QUESTA E` L'UNICA LISTA: se una route serve a un utente sospeso, si aggiunge
// qui, con il motivo, e in nessun altro posto. Le route delle app esterne
// (ExternalAppRoute) non ci stanno perche' non hanno un utente: passano prima.
//
// L'allowlist vale SOLO per i "blocked". Per i "watched" queste route
// richiedono la prova come tutte le altre.
//
// Pattern: segmenti separati da "/"; "{id}" = un segmento qualsiasi non vuoto,
// "{self}" = un segmento uguale all'id dell'utente che fa la richiesta (cosi' di
// users si legge solo il PROPRIO record). Il confronto e` esatto e
// case-sensitive: una variante che PocketBase risolverebbe lo stesso (maiuscole,
// id al posto del nome della collection) non e` nell'elenco, quindi resta
// bloccata. Si fallisce chiusi.
type allowRule struct {
	Methods []string // vuoto = qualsiasi metodo
	Path    string
	Why     string
}

var blockedAllowlist = []allowRule{
	// --- sessione e utente corrente -----------------------------------
	{
		Methods: []string{http.MethodPost},
		Path:    "/api/collections/users/auth-refresh",
		Why:     "refresh della sessione PocketBase (client che usa i token PB nativi)",
	},
	{
		Methods: []string{http.MethodGet},
		Path:    "/api/collections/users/records/{self}",
		Why:     "lettura del PROPRIO record users",
	},
	{
		Methods: []string{http.MethodGet},
		Path:    "/api/cs/me",
		Why: "e` la chiamata con cui l'app KMP carica l'utente corrente a ogni avvio e " +
			"ritorno in foreground (AuthRepository.fetchMeOrWipe); ritorna solo il record " +
			"di chi chiama. Senza, un utente sospeso non avrebbe nemmeno l'utente su cui " +
			"scrivere ex_granted_permissions",
	},

	// --- push ---------------------------------------------------------
	{
		Path: "/api/collections/users_devices/records",
		Why:  "registrazione/lista dei device: la push di richiesta conferma deve continuare ad arrivare",
	},
	{
		Path: "/api/collections/users_devices/records/{id}",
		Why:  "aggiornamento/rimozione di un device (le rule limitano al proprietario)",
	},

	// --- flusso di conferma delle app esterne -------------------------
	{
		Methods: []string{http.MethodGet},
		Path:    "/api/collections/ex_apps/records",
		Why:     "elenco app esterne (pubblico per rule)",
	},
	{
		Methods: []string{http.MethodGet},
		Path:    "/api/collections/ex_apps/records/{id}",
		Why:     "il foglio di conferma legge l'app che chiede l'accesso (ExAppsApi.getExApp)",
	},
	{
		Methods: []string{http.MethodPost},
		Path:    "/api/cs/exapp/export/decide",
		Why: "l'utente conferma o nega l'export dei propri dati richiesto da un'app esterna " +
			"(consenso monouso); la richiesta deve essere sua, lo verifica l'handler",
	},
	{
		Methods: []string{http.MethodGet},
		Path:    "/api/collections/user_notifications/records",
		Why: "elenco notifiche: se la push di conferma sfugge, il sospeso deve poter " +
			"riaprire la richiesta dall'elenco; le rule limitano al proprietario",
	},
	{
		Methods: []string{http.MethodGet, http.MethodPatch, http.MethodDelete},
		Path:    "/api/collections/user_notifications/records/{id}",
		Why:     "dettaglio, segna come letta (seen) e rimozione di una notifica; le rule limitano al proprietario",
	},
	{
		Path: "/api/collections/ex_granted_permissions/records",
		Why:  "lista (getGrantedPermissions) e creazione del consenso; le rule limitano al proprietario",
	},
	{
		Path: "/api/collections/ex_granted_permissions/records/{id}",
		Why:  "view/modifica/revoca del consenso; le rule limitano al proprietario",
	},
}

// BlockedAllowed dice se un utente "blocked" puo` fare questa richiesta.
//
// Oltre al match del pattern, su queste route un `expand` e` sempre rifiutato:
// altrimenti il record permesso (es. il proprio users) farebbe da cavallo di
// Troia verso collection che all'utente sospeso non spettano (timbri, medaglie)
// via relazioni e back-relations. Il client dell'app non usa expand su queste
// route.
func BlockedAllowed(userID, method, p string, query url.Values) bool {
	if userID == "" || path.Clean(p) != p {
		return false
	}
	if query.Get("expand") != "" {
		return false
	}
	method = strings.ToUpper(method)
	if method == http.MethodHead {
		method = http.MethodGet
	}
	for _, r := range blockedAllowlist {
		if len(r.Methods) > 0 && !containsString(r.Methods, method) {
			continue
		}
		if matchPattern(r.Path, p, userID) {
			return true
		}
	}
	return false
}

func matchPattern(pattern, p, userID string) bool {
	want := strings.Split(pattern, "/")
	got := strings.Split(p, "/")
	if len(want) != len(got) {
		return false
	}
	for i := range want {
		switch want[i] {
		case "{id}":
			if got[i] == "" {
				return false
			}
		case "{self}":
			if got[i] == "" || got[i] != userID {
				return false
			}
		default:
			if want[i] != got[i] {
				return false
			}
		}
	}
	return true
}

func containsString(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
