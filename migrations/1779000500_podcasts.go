package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// Podcast = serie (= playlist YouTube). Admin la crea inserendo solo
// youtube_playlist_id; un hook post-create popola title/description/image
// fetchando da YouTube via yt-dlp. Il cron quotidiano fa il sync degli
// episodi nuovi.
func init() {
	m.Register(func(app core.App) error {
		podcasts := core.NewBaseCollection("podcasts")

		podcasts.Fields.Add(&core.TextField{Name: "youtube_playlist_id", Required: true, Max: 64})
		podcasts.Fields.Add(&core.TextField{Name: "title", Max: 500})
		podcasts.Fields.Add(&core.TextField{Name: "description", Max: 5000})
		podcasts.Fields.Add(&core.FileField{
			Name:      "image",
			MaxSelect: 1,
			MaxSize:   5 * 1024 * 1024,
			MimeTypes: []string{"image/jpeg", "image/png", "image/webp"},
		})
		// Stato sync: ultimo run, conteggio episodi, eventuale errore.
		// Comodo per il pannello admin senza dover andare nei log.
		podcasts.Fields.Add(&core.DateField{Name: "last_synced_at"})
		podcasts.Fields.Add(&core.NumberField{Name: "episodes_count"})
		podcasts.Fields.Add(&core.TextField{Name: "last_sync_error", Max: 1000})
		podcasts.Fields.Add(&core.AutodateField{Name: "created", OnCreate: true})
		podcasts.Fields.Add(&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true})

		empty := ""
		podcasts.ListRule = &empty
		podcasts.ViewRule = &empty

		podcasts.AddIndex("idx_podcasts_playlist", true, "youtube_playlist_id", "")
		if err := app.Save(podcasts); err != nil {
			return err
		}

		episodes := core.NewBaseCollection("podcast_episodes")
		episodes.Fields.Add(&core.RelationField{
			Name:          "podcast",
			Required:      true,
			MaxSelect:     1,
			CollectionId:  podcasts.Id,
			CascadeDelete: true,
		})
		episodes.Fields.Add(&core.TextField{Name: "youtube_video_id", Required: true, Max: 32})
		episodes.Fields.Add(&core.TextField{Name: "title", Max: 500})
		episodes.Fields.Add(&core.TextField{Name: "description", Max: 10000})
		episodes.Fields.Add(&core.FileField{
			Name:      "audio",
			MaxSelect: 1,
			MaxSize:   500 * 1024 * 1024, // 500MB: episodi lunghi a 128k passano
			MimeTypes: []string{"audio/mpeg", "audio/mp3"},
		})
		episodes.Fields.Add(&core.FileField{
			Name:      "image",
			MaxSelect: 1,
			MaxSize:   5 * 1024 * 1024,
			MimeTypes: []string{"image/jpeg", "image/png", "image/webp"},
		})
		episodes.Fields.Add(&core.NumberField{Name: "duration_seconds"})
		episodes.Fields.Add(&core.DateField{Name: "published_at"})
		episodes.Fields.Add(&core.AutodateField{Name: "created", OnCreate: true})
		episodes.Fields.Add(&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true})

		episodes.ListRule = &empty
		episodes.ViewRule = &empty

		episodes.AddIndex("idx_podcast_episodes_video", true, "youtube_video_id", "")
		episodes.AddIndex("idx_podcast_episodes_podcast", false, "podcast", "")

		return app.Save(episodes)
	}, func(app core.App) error {
		if col, err := app.FindCollectionByNameOrId("podcast_episodes"); err == nil {
			_ = app.Delete(col)
		}
		if col, err := app.FindCollectionByNameOrId("podcasts"); err == nil {
			_ = app.Delete(col)
		}
		return nil
	})
}
