package podcastsync

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/filesystem"
)

const httpThumbTimeout = 30 * time.Second

// PopulatePodcastMetadata legge la playlist YT, valorizza title/description/
// image sul record podcasts e salva. Idempotente: ri-chiamarla aggiorna i
// campi se cambiati lato YouTube.
func PopulatePodcastMetadata(app core.App, podcast *core.Record) error {
	playlistID := podcast.GetString("youtube_playlist_id")
	if playlistID == "" {
		return fmt.Errorf("youtube_playlist_id mancante")
	}

	meta, err := FetchPlaylistFlat(playlistID)
	if err != nil {
		return fmt.Errorf("fetch playlist: %w", err)
	}

	podcast.Set("title", meta.Title)
	podcast.Set("description", meta.Description)

	if podcast.GetString("image") == "" {
		thumbURL := pickBestThumbnail(meta)
		if thumbURL != "" {
			if file, err := downloadAsPBFile(thumbURL, "podcast_cover_"+playlistID); err == nil {
				podcast.Set("image", file)
			}
		}
	}

	return app.Save(podcast)
}

func pickBestThumbnail(meta *PlaylistMetadata) string {
	// Playlist-level: prendi la thumb piu` grande disponibile.
	bestW := 0
	bestURL := ""
	for _, t := range meta.Thumbnails {
		if t.Width > bestW {
			bestW = t.Width
			bestURL = t.URL
		}
	}
	if bestURL != "" {
		return bestURL
	}
	// Fallback: thumbnail del primo episodio.
	if len(meta.Entries) > 0 && meta.Entries[0].Thumbnail != "" {
		return meta.Entries[0].Thumbnail
	}
	return ""
}

// SyncEpisodes itera sui video della playlist e scarica/upserta quelli non
// ancora presenti come record podcast_episodes. Aggiorna le statistiche
// (last_synced_at, episodes_count, last_sync_error) sul record podcast.
func SyncEpisodes(app core.App, podcast *core.Record) error {
	playlistID := podcast.GetString("youtube_playlist_id")
	if playlistID == "" {
		return fmt.Errorf("youtube_playlist_id mancante")
	}

	meta, err := FetchPlaylistFlat(playlistID)
	if err != nil {
		podcast.Set("last_sync_error", err.Error())
		_ = app.Save(podcast)
		return err
	}

	epCol, err := app.FindCollectionByNameOrId("podcast_episodes")
	if err != nil {
		return fmt.Errorf("find podcast_episodes: %w", err)
	}

	tmpRoot, err := os.MkdirTemp("", "podcast_dl_*")
	if err != nil {
		return fmt.Errorf("mktemp: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpRoot) }()

	added := 0
	skippedShorts := 0
	for _, entry := range meta.Entries {
		if entry.ID == "" {
			continue
		}
		if isYouTubeShort(entry) {
			// I shorts non sono "puntate" del podcast: tipicamente sono clip
			// promozionali o trailer brevi. Skip senza scaricare.
			app.Logger().Info("[podcastsync] skip shorts",
				"podcast", podcast.Id, "video", entry.ID, "title", entry.Title, "duration_s", entry.Duration)
			skippedShorts++
			continue
		}
		existing, _ := app.FindFirstRecordByData(epCol.Id, "youtube_video_id", entry.ID)
		if existing != nil {
			continue
		}

		dl, err := DownloadEpisode(entry.ID, tmpRoot)
		if err != nil {
			app.Logger().Warn("[podcastsync] download episodio fallito, skip",
				"podcast", podcast.Id, "video", entry.ID, "err", err)
			continue
		}

		// Taglia silenzio in testa e in coda. In caso di errore lasciamo
		// il file originale: meglio un episodio con silenzio che nessuno.
		if newDur, err := TrimSilence(dl.AudioPath); err != nil {
			app.Logger().Warn("[podcastsync] trim silenzio fallito, uso originale",
				"video", entry.ID, "err", err)
		} else if newDur > 0 {
			dl.DurationSeconds = newDur
		}

		rec := core.NewRecord(epCol)
		rec.Set("podcast", podcast.Id)
		rec.Set("youtube_video_id", dl.VideoID)
		rec.Set("title", dl.Title)
		rec.Set("description", dl.Description)
		rec.Set("duration_seconds", dl.DurationSeconds)
		if t, err := ParseUploadDate(dl.UploadDate); err == nil {
			rec.Set("published_at", t)
		}

		if audio, err := fileFromPath(dl.AudioPath); err == nil {
			rec.Set("audio", audio)
		} else {
			app.Logger().Warn("[podcastsync] audio file wrap fallito", "video", entry.ID, "err", err)
			continue
		}

		// Thumbnail: prova prima il file scritto da yt-dlp, altrimenti la URL.
		if thumb, err := fileFromPath(dl.ThumbnailPath); err == nil {
			rec.Set("image", thumb)
		} else if dl.ThumbnailURL != "" {
			if thumb, err := downloadAsPBFile(dl.ThumbnailURL, "thumb_"+dl.VideoID); err == nil {
				rec.Set("image", thumb)
			}
		}

		if err := app.Save(rec); err != nil {
			app.Logger().Error("[podcastsync] save episode fallito",
				"podcast", podcast.Id, "video", entry.ID, "err", err)
			continue
		}

		// Trascrizione: prima via i sottotitoli auto YouTube (gratis,
		// inline). Se YouTube non ne ha, fallback async su whisper.cpp
		// locale (serializzato uno alla volta, non blocca il sync).
		if dl.SubtitlePath != "" {
			if err := saveTranscriptFromVTT(app, rec.Id, dl.SubtitlePath); err != nil {
				app.Logger().Warn("[podcastsync] parse VTT fallito, episodio senza transcript",
					"video", entry.ID, "err", err)
			}
		} else {
			app.Logger().Info("[podcastsync] nessun sottotitolo YouTube, fallback whisper async",
				"video", entry.ID)
			// Copia l'audio in un path stabile (il tmpRoot viene cancellato
			// alla fine di SyncEpisodes, ma la goroutine vive piu` a lungo).
			queueWhisperFallback(app, rec.Id, dl.AudioPath)
		}
		added++
	}

	// Refresh contatori sul podcast.
	count, _ := app.CountRecords("podcast_episodes", nil)
	_ = count // total su tutti: non quello che vogliamo.
	allOfThis, err := app.FindRecordsByFilter("podcast_episodes",
		"podcast = '"+podcast.Id+"'", "", 0, 0, nil)
	if err == nil {
		podcast.Set("episodes_count", len(allOfThis))
	}
	podcast.Set("last_synced_at", time.Now())
	podcast.Set("last_sync_error", "")
	if err := app.Save(podcast); err != nil {
		app.Logger().Error("[podcastsync] update podcast stats fallito",
			"podcast", podcast.Id, "err", err)
	}

	app.Logger().Info("[podcastsync] sync completato",
		"podcast", podcast.Id,
		"added", added,
		"shorts_skipped", skippedShorts,
		"playlist_total", len(meta.Entries))
	return nil
}

// isYouTubeShort decide se un video va trattato come "short" e quindi
// escluso dal sync podcast. Euristica:
//   - titolo o descrizione contengono "#shorts" / "#short" (case insensitive)
//   - durata strettamente positiva e <= 60s (i shorts hanno durata <=60s)
// Il flat-playlist di yt-dlp non espone description per ogni entry, quindi
// in pratica controlliamo titolo + durata.
func isYouTubeShort(entry PlaylistEntry) bool {
	t := strings.ToLower(entry.Title)
	if strings.Contains(t, "#shorts") || strings.Contains(t, "#short") {
		return true
	}
	if entry.Duration > 0 && entry.Duration <= 60 {
		return true
	}
	return false
}

// SyncAll itera su tutte le serie podcast registrate e fa il sync episodi.
// Usato dal cron giornaliero e dal pulsante "Search index backfill (manual)".
func SyncAll(app core.App) (perPodcast map[string]int, err error) {
	col, err := app.FindCollectionByNameOrId("podcasts")
	if err != nil {
		return nil, fmt.Errorf("find podcasts: %w", err)
	}
	podcasts, err := app.FindAllRecords(col)
	if err != nil {
		return nil, fmt.Errorf("list podcasts: %w", err)
	}
	perPodcast = make(map[string]int, len(podcasts))
	for _, p := range podcasts {
		before := p.GetInt("episodes_count")
		if err := SyncEpisodes(app, p); err != nil {
			app.Logger().Error("[podcastsync] sync podcast fallito",
				"podcast", p.Id, "err", err)
			continue
		}
		after := p.GetInt("episodes_count")
		perPodcast[p.Id] = after - before
	}
	return perPodcast, nil
}

// saveTranscriptFromVTT parsa il file VTT scaricato da yt-dlp e crea/aggiorna
// la riga in podcast_episodes_transcript. Upserts per episode (1:1).
func saveTranscriptFromVTT(app core.App, episodeID, vttPath string) error {
	parsed, err := ParseVTT(vttPath)
	if err != nil {
		return err
	}
	col, err := app.FindCollectionByNameOrId("podcast_episodes_transcript")
	if err != nil {
		return err
	}
	rec, _ := app.FindFirstRecordByData(col.Id, "episode", episodeID)
	if rec == nil {
		rec = core.NewRecord(col)
		rec.Set("episode", episodeID)
	}
	if parsed.Transcript == "" || len(parsed.Segments) == 0 {
		// VTT presente ma vuoto: marker "non adatto" per non riprovare.
		rec.Set("transcript", "")
		rec.Set("segments", nil)
		rec.Set("duration_seconds", 0)
		rec.Set("model", "youtube-auto-subs")
		rec.Set("language", "it-IT")
		rec.Set("content_hash", "vtt_empty")
		return app.Save(rec)
	}
	segJSON, err := json.Marshal(parsed.Segments)
	if err != nil {
		return fmt.Errorf("marshal segments: %w", err)
	}
	rec.Set("transcript", parsed.Transcript)
	rec.Set("segments", string(segJSON))
	rec.Set("duration_seconds", parsed.DurationSeconds)
	rec.Set("model", "youtube-auto-subs")
	rec.Set("language", "it-IT")
	rec.Set("content_hash", "youtube_vtt") // marker: sorgente sub YT
	return app.Save(rec)
}

// fileFromPath crea un *filesystem.File da un path locale, leggendo il
// contenuto. PB lo userà per metterlo su Minio via il file field.
func fileFromPath(path string) (*filesystem.File, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, err
	}
	return filesystem.NewFileFromPath(path)
}

// downloadAsPBFile scarica HTTP un'immagine e la incarta in *filesystem.File
// con un nome leggibile.
func downloadAsPBFile(url, nameHint string) (*filesystem.File, error) {
	client := &http.Client{Timeout: httpThumbTimeout}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	ext := filepath.Ext(url)
	if ext == "" {
		ext = ".jpg"
	}
	return filesystem.NewFileFromBytes(body, nameHint+ext)
}
