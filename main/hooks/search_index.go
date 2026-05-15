package hooks

import (
	"github.com/pocketbase/pocketbase/core"

	"mensadb/tools/quidaudio"
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

func indexMemberAsync(e *core.RecordEvent) error {
	rec := e.Record
	app := e.App
	go func() {
		// Soci disattivi / scaduti: rimuovi dall'indice se presenti.
		if !rec.GetBool("is_active") {
			if err := search.Delete(rec.Id); err != nil {
				app.Logger().Error("search index delete failed", "type", "member", "id", rec.Id, "err", err)
			}
			return
		}
		doc := BuildMemberDoc(app, rec)
		if err := search.Upsert(doc); err != nil {
			app.Logger().Error("search index upsert failed", "type", "member", "id", rec.Id, "err", err)
		}
	}()
	return e.Next()
}

func indexQuidIssueAsync(e *core.RecordEvent) error {
	rec := e.Record
	app := e.App
	go func() {
		doc := BuildQuidIssueDoc(app, rec)
		if err := search.Upsert(doc); err != nil {
			app.Logger().Error("search index upsert failed", "type", "quid_issue", "id", rec.Id, "err", err)
		}
	}()
	return e.Next()
}

func indexQuidArticleAsync(e *core.RecordEvent) error {
	rec := e.Record
	app := e.App
	go func() {
		doc := BuildQuidArticleDoc(app, rec)
		if err := search.Upsert(doc); err != nil {
			app.Logger().Error("search index upsert failed", "type", "quid_article", "id", rec.Id, "err", err)
		}
	}()
	go func() {
		if err := quidaudio.Generate(app, rec); err != nil {
			app.Logger().Error("[quidaudio] generate fallito", "article", rec.Id, "err", err)
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
