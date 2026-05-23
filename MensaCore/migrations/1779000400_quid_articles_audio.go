package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// Tabella che ospita l'audio TTS generato per ogni articolo Quid.
// Pattern speculare a documents_elaborated: relazione 1:1 con la tabella
// principale, file servito da PB (backend Minio), nessuna scrittura dal
// client. Lettura pubblica come quid_articles.
func init() {
	m.Register(func(app core.App) error {
		articles, err := app.FindCollectionByNameOrId("quid_articles")
		if err != nil {
			return err
		}

		col := core.NewBaseCollection("quid_articles_audio")

		col.Fields.Add(&core.RelationField{
			Name:          "article",
			Required:      true,
			MaxSelect:     1,
			CollectionId:  articles.Id,
			CascadeDelete: true,
		})
		col.Fields.Add(&core.FileField{
			Name:      "audio",
			MaxSelect: 1,
			MaxSize:   50 * 1024 * 1024, // 50MB cap per articolo
			MimeTypes: []string{"audio/mpeg", "audio/mp3"},
		})
		// Voce TTS usata (es. "it-IT-Chirp3-HD-..."): utile sia per debug sia
		// per rigenerare quando si cambia voce.
		col.Fields.Add(&core.TextField{Name: "voice", Max: 100})
		// Hash sha256 del testo da cui e` stato generato l'audio. Quando il
		// body dell'articolo cambia, l'hash diverge → rigeneriamo.
		col.Fields.Add(&core.TextField{Name: "content_hash", Max: 64})
		// Durata in secondi, comoda per la UI ("12 min di ascolto").
		col.Fields.Add(&core.NumberField{Name: "duration_seconds"})
		col.Fields.Add(&core.AutodateField{Name: "created", OnCreate: true})
		col.Fields.Add(&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true})

		empty := ""
		col.ListRule = &empty
		col.ViewRule = &empty

		col.AddIndex("idx_quid_audio_article", true, "article", "")

		return app.Save(col)
	}, func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("quid_articles_audio")
		if err != nil {
			return nil
		}
		return app.Delete(col)
	})
}
