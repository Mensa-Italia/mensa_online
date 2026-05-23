package dbtools

import (
	"sort"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// ResolveUserLocalOffice ritorna l'id del local_office associato all'utente.
// Priorita`: admins (segretari/co-segretari) > test_assistants. Se l'utente
// ha piu` cariche nello stesso ruolo (es. segretario di due gruppi locali
// dopo una fusione), torna quello il cui nome ordina prima alfabeticamente:
// scelta deterministica e consistente con cio` che vedrebbe l'utente in lista.
//
// Ritorna "" se l'utente non e` linkato a nessun gruppo.
func ResolveUserLocalOffice(app core.App, userID string) string {
	if userID == "" {
		return ""
	}
	if id := firstAlphabeticOffice(app, "local_offices_admins", userID); id != "" {
		return id
	}
	return firstAlphabeticOffice(app, "local_offices_test_assistants", userID)
}

// firstAlphabeticOffice carica tutti i link (collection passata) per
// l'utente, risale ai relativi local_office, ordina per name e ritorna il
// primo. Se non ci sono link, ritorna "".
func firstAlphabeticOffice(app core.App, linkCollection, userID string) string {
	links, err := app.FindRecordsByFilter(linkCollection,
		"user = {:u}", "", -1, 0, dbx.Params{"u": userID},
	)
	if err != nil || len(links) == 0 {
		return ""
	}
	if len(links) == 1 {
		return links[0].GetString("local_office")
	}
	type officeRef struct {
		id   string
		name string
	}
	offices := make([]officeRef, 0, len(links))
	for _, l := range links {
		oid := l.GetString("local_office")
		if oid == "" {
			continue
		}
		o, err := app.FindRecordById("local_offices", oid)
		if err != nil {
			continue
		}
		offices = append(offices, officeRef{id: oid, name: o.GetString("name")})
	}
	if len(offices) == 0 {
		return ""
	}
	sort.SliceStable(offices, func(i, j int) bool {
		return offices[i].name < offices[j].name
	})
	return offices[0].id
}
