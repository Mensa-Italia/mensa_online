package hooks

import (
	"github.com/pocketbase/pocketbase/core"
	"mensadb/tools/search"
)

func indexEventAsync(e *core.RecordEvent) error {
	rec := e.Record
	app := e.App
	go func() {
		doc := BuildEventDoc(app, rec)
		if err := search.Upsert(doc); err != nil {
			app.Logger().Error("search index upsert failed", "type", "event", "id", rec.Id, "err", err)
		}
	}()
	return e.Next()
}

func indexSigAsync(e *core.RecordEvent) error {
	rec := e.Record
	app := e.App
	go func() {
		doc := BuildSigDoc(app, rec)
		if err := search.Upsert(doc); err != nil {
			app.Logger().Error("search index upsert failed", "type", "sig", "id", rec.Id, "err", err)
		}
	}()
	return e.Next()
}

func indexDealAsync(e *core.RecordEvent) error {
	rec := e.Record
	app := e.App
	go func() {
		doc := BuildDealDoc(app, rec)
		if err := search.Upsert(doc); err != nil {
			app.Logger().Error("search index upsert failed", "type", "deal", "id", rec.Id, "err", err)
		}
	}()
	return e.Next()
}

func indexDocumentAsync(e *core.RecordEvent) error {
	rec := e.Record
	app := e.App
	go func() {
		doc := BuildDocumentDoc(app, rec)
		if err := search.Upsert(doc); err != nil {
			app.Logger().Error("search index upsert failed", "type", "document", "id", rec.Id, "err", err)
		}
	}()
	return e.Next()
}

func indexUserAsync(e *core.RecordEvent) error {
	rec := e.Record
	app := e.App
	go func() {
		doc := BuildUserDoc(app, rec)
		if err := search.Upsert(doc); err != nil {
			app.Logger().Error("search index upsert failed", "type", "user", "id", rec.Id, "err", err)
		}
	}()
	return e.Next()
}

func unindexAsync(e *core.RecordEvent) error {
	id := e.Record.Id
	app := e.App
	go func() {
		if err := search.Delete(id); err != nil {
			app.Logger().Error("search index delete failed", "id", id, "err", err)
		}
	}()
	return e.Next()
}
