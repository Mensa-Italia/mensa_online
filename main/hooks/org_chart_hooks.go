package hooks

import (
	"github.com/pocketbase/pocketbase/core"
	"mensadb/tools/search"
)

// indexOrgRoleAsync indicizza un record org_chart_members come Bleve doc
// di tipo "org_role". Title = "<ruolo> — <gruppo>", Body = nome del socio.
// Cosi` cercando "Presidente" o "Mario Rossi" salta fuori il ruolo.
func indexOrgRoleAsync(e *core.RecordEvent) error {
	rec := e.Record
	app := e.App
	go func() {
		doc := BuildOrgRoleDoc(app, rec)
		if err := search.Upsert(doc); err != nil {
			app.Logger().Error("search index upsert failed", "type", "org_role", "id", rec.Id, "err", err)
		}
	}()
	return e.Next()
}

func unindexOrgRoleAsync(e *core.RecordEvent) error {
	id := e.Record.Id
	app := e.App
	go func() {
		if err := search.Delete(id); err != nil {
			app.Logger().Error("search index delete failed", "id", id, "err", err)
		}
	}()
	return e.Next()
}

// indexOrgGroupAsync indicizza il gruppo dell'organigramma come tipo
// "org_group". Title = nome del gruppo. Permette di cercare "consiglio"
// e tornare una singola tile sul gruppo invece delle N cariche dentro.
func indexOrgGroupAsync(e *core.RecordEvent) error {
	rec := e.Record
	app := e.App
	go func() {
		doc := BuildOrgGroupDoc(app, rec)
		if err := search.Upsert(doc); err != nil {
			app.Logger().Error("search index upsert failed", "type", "org_group", "id", rec.Id, "err", err)
		}
	}()
	return e.Next()
}
