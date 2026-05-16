package podcastsync

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/pocketbase/pocketbase/core"

	"mensadb/tools/whisper"
)

// whisperQueueMu serializza le chiamate a whisper.cpp: una alla volta per
// non saturare il container. Le richieste in fila attendono il proprio
// turno senza bloccare il caller di SyncEpisodes (che spawna goroutine e
// ritorna subito).
var whisperQueueMu sync.Mutex

// queueWhisperFallback copia l'audio in un path persistente di lavoro,
// poi lancia una goroutine che fa la trascrizione locale via whisper.cpp e
// salva il record podcast_episodes_transcript. Serializzata dal mutex globale.
//
// Se whisper.cpp non e` disponibile (binary mancante / modello non
// scaricabile / errore) salva un marker di errore standard (duration_seconds
// = -1) cosi` la condizione e` visibile lato admin senza ricerche.
func queueWhisperFallback(app core.App, episodeID, srcAudioPath string) {
	// Copia subito l'audio fuori dalla tmpDir del sync (che viene
	// distrutta al return di SyncEpisodes).
	persistDir := filepath.Join(os.TempDir(), "whisper_pending")
	if err := os.MkdirAll(persistDir, 0o755); err != nil {
		app.Logger().Error("[whisper-fallback] mkdir pending dir", "err", err)
		return
	}
	stableAudio := filepath.Join(persistDir, episodeID+".mp3")
	if err := copyFile(srcAudioPath, stableAudio); err != nil {
		app.Logger().Error("[whisper-fallback] copia audio fallita",
			"episode", episodeID, "err", err)
		return
	}

	go func() {
		defer func() { _ = os.Remove(stableAudio) }()
		whisperQueueMu.Lock()
		defer whisperQueueMu.Unlock()

		app.Logger().Info("[whisper-fallback] start", "episode", episodeID)
		res, err := whisper.Transcribe(context.Background(), stableAudio)
		if err != nil {
			saveWhisperErrorMarker(app, episodeID, err)
			app.Logger().Error("[whisper-fallback] transcribe fallita",
				"episode", episodeID, "err", err)
			return
		}
		if res.Transcript == "" || len(res.Segments) == 0 {
			saveWhisperEmptyMarker(app, episodeID)
			app.Logger().Info("[whisper-fallback] transcript vuoto, marker", "episode", episodeID)
			return
		}
		if err := saveWhisperTranscript(app, episodeID, res); err != nil {
			app.Logger().Error("[whisper-fallback] save transcript fallita",
				"episode", episodeID, "err", err)
			return
		}
		app.Logger().Info("[whisper-fallback] done",
			"episode", episodeID,
			"transcript_chars", len(res.Transcript),
			"segments", len(res.Segments),
			"duration_s", res.DurationSeconds)
	}()
}

func saveWhisperTranscript(app core.App, episodeID string, res *whisper.Result) error {
	col, err := app.FindCollectionByNameOrId("podcast_episodes_transcript")
	if err != nil {
		return err
	}
	rec, _ := app.FindFirstRecordByData(col.Id, "episode", episodeID)
	if rec == nil {
		rec = core.NewRecord(col)
		rec.Set("episode", episodeID)
	}
	segJSON, err := json.Marshal(res.Segments)
	if err != nil {
		return fmt.Errorf("marshal segments: %w", err)
	}
	lang := res.Language
	if lang == "" {
		lang = "it"
	}
	rec.Set("transcript", res.Transcript)
	rec.Set("segments", string(segJSON))
	rec.Set("duration_seconds", res.DurationSeconds)
	rec.Set("model", "whisper-medium-q5")
	rec.Set("language", lang)
	rec.Set("content_hash", "whisper_local")
	return app.Save(rec)
}

func saveWhisperEmptyMarker(app core.App, episodeID string) {
	col, err := app.FindCollectionByNameOrId("podcast_episodes_transcript")
	if err != nil {
		return
	}
	rec, _ := app.FindFirstRecordByData(col.Id, "episode", episodeID)
	if rec == nil {
		rec = core.NewRecord(col)
		rec.Set("episode", episodeID)
	}
	rec.Set("transcript", "")
	rec.Set("segments", nil)
	rec.Set("duration_seconds", 0)
	rec.Set("model", "whisper-medium-q5")
	rec.Set("language", "it")
	rec.Set("content_hash", "whisper_empty")
	_ = app.Save(rec)
}

func saveWhisperErrorMarker(app core.App, episodeID string, origErr error) {
	col, err := app.FindCollectionByNameOrId("podcast_episodes_transcript")
	if err != nil {
		return
	}
	rec, _ := app.FindFirstRecordByData(col.Id, "episode", episodeID)
	if rec == nil {
		rec = core.NewRecord(col)
		rec.Set("episode", episodeID)
	}
	errMsg := origErr.Error()
	if len(errMsg) > 500 {
		errMsg = errMsg[:500]
	}
	rec.Set("transcript", "")
	rec.Set("segments", nil)
	rec.Set("duration_seconds", -1)
	rec.Set("model", "whisper-medium-q5")
	rec.Set("content_hash", errMsg)
	_ = app.Save(rec)
}

func copyFile(src, dst string) error {
	s, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = s.Close() }()
	d, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(d, s); err != nil {
		_ = d.Close()
		_ = os.Remove(dst)
		return err
	}
	return d.Close()
}
