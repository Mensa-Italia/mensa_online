package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// Due modifiche a quid_articles_audio:
//
//   1. list/view filtrati su duration_seconds > 0: gli articoli falliti
//      (-1) e quelli non adatti a TTS (0) restano in tabella per ispezione
//      dall'admin, ma non finiscono nelle API pubbliche.
//   2. content_hash esteso da 64 a 500 caratteri: sul percorso successo
//      ospita la sha256 (64 hex), sul percorso errore lo riusiamo per
//      memorizzare il messaggio d'errore tronco — cosi` dall'admin si vede
//      a colpo d'occhio cosa non e` andato senza scavare nei log.
func init() {
	m.Register(func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("quid_articles_audio")
		if err != nil {
			return err
		}
		rule := "duration_seconds > 0"
		col.ListRule = &rule
		col.ViewRule = &rule

		// Aumenta capienza content_hash per ospitare messaggi d'errore.
		if f := col.Fields.GetByName("content_hash"); f != nil {
			if tf, ok := f.(*core.TextField); ok {
				tf.Max = 500
			}
		}
		return app.Save(col)
	}, func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("quid_articles_audio")
		if err != nil {
			return nil
		}
		empty := ""
		col.ListRule = &empty
		col.ViewRule = &empty
		if f := col.Fields.GetByName("content_hash"); f != nil {
			if tf, ok := f.(*core.TextField); ok {
				tf.Max = 64
			}
		}
		return app.Save(col)
	})
}
