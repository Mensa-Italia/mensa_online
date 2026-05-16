package hooks

import (
	"github.com/pocketbase/pocketbase/core"

	"mensadb/tools/podcastsync"
	"mensadb/tools/podcasttranscribe"
)

// PodcastEpisodeAfterWriteAsync: trascrive ogni episodio nuovo o aggiornato
// con Gemini, salva transcript + segments in podcast_episodes_transcript.
// Il transcript viene poi incluso dal builder Bleve nel body del doc
// podcast_episode, rendendo il contenuto parlato pienamente ricercabile.
func PodcastEpisodeAfterWriteAsync(e *core.RecordEvent) error {
	rec := e.Record
	app := e.App
	go func() {
		if err := podcasttranscribe.Generate(app, rec); err != nil {
			app.Logger().Error("[podcasttranscribe] generate fallito",
				"episode", rec.Id, "err", err)
		}
	}()
	return e.Next()
}

// PodcastAfterCreateAsync: quando l'admin crea una serie podcast inserendo
// solo lo youtube_playlist_id, popola in background metadata (title,
// description, image) e scarica tutti gli episodi.
func PodcastAfterCreateAsync(e *core.RecordEvent) error {
	rec := e.Record
	app := e.App
	go func() {
		if err := podcastsync.PopulatePodcastMetadata(app, rec); err != nil {
			app.Logger().Error("[podcasts] populate metadata fallito",
				"podcast", rec.Id, "err", err)
		}
		if err := podcastsync.SyncEpisodes(app, rec); err != nil {
			app.Logger().Error("[podcasts] sync episodi fallito",
				"podcast", rec.Id, "err", err)
		}
	}()
	return e.Next()
}
