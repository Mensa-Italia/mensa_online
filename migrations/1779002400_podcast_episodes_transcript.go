package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// Cache delle trascrizioni Gemini per ogni episodio podcast. Pattern
// speculare a documents_elaborated / quid_articles_audio:
//   - 1:1 relation con podcast_episodes
//   - transcript: testo piatto, indicizzato in Bleve nel doc podcast_episode
//   - segments: JSON [{start, end, text}] per i chapter marker / seek
//   - duration_seconds marker:
//        > 0  → trascrizione ok (= durata copertura)
//        = 0  → audio non adatto (musica strumentale, audio assente, ecc.)
//        = -1 → errore (retry al prossimo trigger)
//   - content_hash: sha256 dell'audio O messaggio errore (max 500 char)
func init() {
	m.Register(func(app core.App) error {
		episodes, err := app.FindCollectionByNameOrId("podcast_episodes")
		if err != nil {
			return err
		}
		col := core.NewBaseCollection("podcast_episodes_transcript")

		col.Fields.Add(&core.RelationField{
			Name:          "episode",
			Required:      true,
			MaxSelect:     1,
			CollectionId:  episodes.Id,
			CascadeDelete: true,
		})
		col.Fields.Add(&core.TextField{Name: "language", Max: 16})
		col.Fields.Add(&core.TextField{Name: "model", Max: 100})
		col.Fields.Add(&core.TextField{Name: "content_hash", Max: 500})
		col.Fields.Add(&core.NumberField{Name: "duration_seconds"})
		// transcript: testo piatto, fino a 5MB (max ~750k caratteri).
		col.Fields.Add(&core.TextField{Name: "transcript", Max: 5_000_000})
		// segments: JSON di array [{start_seconds, end_seconds, text}].
		col.Fields.Add(&core.JSONField{Name: "segments", MaxSize: 5_000_000})
		col.Fields.Add(&core.AutodateField{Name: "created", OnCreate: true})
		col.Fields.Add(&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true})

		// API pubblica: episodi nascondono trascrizioni fallite/non adatte.
		rule := "duration_seconds > 0"
		col.ListRule = &rule
		col.ViewRule = &rule

		col.AddIndex("idx_podcast_transcript_episode", true, "episode", "")
		col.AddIndex("idx_podcast_transcript_duration", false, "duration_seconds", "")
		return app.Save(col)
	}, func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("podcast_episodes_transcript")
		if err != nil {
			return nil
		}
		return app.Delete(col)
	})
}
