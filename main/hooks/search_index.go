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

func indexPodcastAsync(e *core.RecordEvent) error {
	rec := e.Record
	app := e.App
	go func() {
		doc := BuildPodcastDoc(app, rec)
		if err := search.Upsert(doc); err != nil {
			app.Logger().Error("search index upsert failed", "type", "podcast", "id", rec.Id, "err", err)
		}
	}()
	return e.Next()
}

func indexPodcastEpisodeAsync(e *core.RecordEvent) error {
	rec := e.Record
	app := e.App
	go func() {
		doc := BuildPodcastEpisodeDoc(app, rec)
		if err := search.Upsert(doc); err != nil {
			app.Logger().Error("search index upsert failed", "type", "podcast_episode", "id", rec.Id, "err", err)
		}
	}()
	return e.Next()
}

// indexLinktreeLinkAsync indicizza/deindicizza un link del linktree di un
// gruppo locale. Le sezioni (kind="section") e i link disattivati
// (active=false) vengono rimossi dall'indice: l'utente finale non se ne fa
// nulla, e per i link reattivati ripopoleremo al successivo update.
func indexLocalOfficeAsync(e *core.RecordEvent) error {
	rec := e.Record
	app := e.App
	go func() {
		doc := BuildLocalOfficeDoc(app, rec)
		if err := search.Upsert(doc); err != nil {
			app.Logger().Error("search index upsert failed", "type", "local_office", "id", rec.Id, "err", err)
		}
	}()
	return e.Next()
}

func indexLocalOfficeAdminAsync(e *core.RecordEvent) error {
	rec := e.Record
	app := e.App
	go func() {
		doc := BuildLocalOfficeAdminDoc(app, rec)
		if err := search.Upsert(doc); err != nil {
			app.Logger().Error("search index upsert failed", "type", "local_office_admin", "id", rec.Id, "err", err)
		}
	}()
	return e.Next()
}

func indexLocalOfficeTestAssistantAsync(e *core.RecordEvent) error {
	rec := e.Record
	app := e.App
	go func() {
		doc := BuildLocalOfficeTestAssistantDoc(app, rec)
		if err := search.Upsert(doc); err != nil {
			app.Logger().Error("search index upsert failed", "type", "local_office_test_assistant", "id", rec.Id, "err", err)
		}
	}()
	return e.Next()
}

func indexLinktreeLinkAsync(e *core.RecordEvent) error {
	rec := e.Record
	app := e.App
	go func() {
		if rec.GetString("kind") != "link" || !rec.GetBool("active") {
			if err := search.Delete(rec.Id); err != nil {
				app.Logger().Error("search index delete failed", "type", "linktree_link", "id", rec.Id, "err", err)
			}
			return
		}
		doc := BuildLinktreeLinkDoc(app, rec)
		if err := search.Upsert(doc); err != nil {
			app.Logger().Error("search index upsert failed", "type", "linktree_link", "id", rec.Id, "err", err)
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

// reindexParentEpisode: handler per podcast_episodes_transcript che, dopo
// ogni write della trascrizione, re-indicizza l'episodio padre cosi` il
// nuovo body (description + transcript) entra subito in Bleve.
func reindexParentEpisode(e *core.RecordEvent) error {
	rec := e.Record
	app := e.App
	go func() {
		episodeID := rec.GetString("episode")
		if episodeID == "" {
			return
		}
		ep, err := app.FindRecordById("podcast_episodes", episodeID)
		if err != nil || ep == nil {
			return
		}
		doc := BuildPodcastEpisodeDoc(app, ep)
		if err := search.Upsert(doc); err != nil {
			app.Logger().Error("search index reindex podcast_episode failed",
				"id", ep.Id, "err", err)
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
