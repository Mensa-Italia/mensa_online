package podcasttranscribe

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/pocketbase/pocketbase/core"

	"mensadb/tools/env"
)

// Generate orchestra la trascrizione di un singolo episodio podcast:
//
//  1. hash sha256 sull'audio
//  2. se esiste podcast_episodes_transcript con stesso hash → noop
//  3. scarica l'audio dal filesystem PB (Minio) in un tmpfile
//  4. invoca Transcribe(Gemini)
//  5. salva il record o marker errore (duration_seconds = -1)
//
// Conv. duration_seconds (uguale a quidaudio):
//   > 0  → trascrizione ok
//   = 0  → non adatto a indicizzazione (musica, audio rotto, troppo breve)
//   = -1 → errore (retry al prossimo trigger)
func Generate(app core.App, episode *core.Record) error {
	audioField := episode.GetString("audio")
	if audioField == "" {
		return nil // niente audio, niente da fare
	}

	transcriptCol, err := app.FindCollectionByNameOrId("podcast_episodes_transcript")
	if err != nil {
		return fmt.Errorf("find podcast_episodes_transcript: %w", err)
	}

	// Hash + skip-if-unchanged.
	audioBytes, err := readEpisodeAudio(app, episode, audioField)
	if err != nil {
		return saveErrorMarker(app, transcriptCol, episode, fmt.Errorf("read audio: %w", err))
	}
	hash := sha256.Sum256(audioBytes)
	hashStr := hex.EncodeToString(hash[:])

	existing, _ := app.FindFirstRecordByData(transcriptCol.Id, "episode", episode.Id)
	if existing != nil && existing.GetString("content_hash") == hashStr {
		return nil
	}

	// Scrivi su tmp file: Files API accetta un reader, ma vuole un file
	// "vero" stream-friendly e mime affidabile.
	tmp, err := os.CreateTemp("", "podcast_ep_*.mp3")
	if err != nil {
		return saveErrorMarker(app, transcriptCol, episode, fmt.Errorf("tmp file: %w", err))
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	if _, err := tmp.Write(audioBytes); err != nil {
		_ = tmp.Close()
		return saveErrorMarker(app, transcriptCol, episode, fmt.Errorf("tmp write: %w", err))
	}
	_ = tmp.Close()

	ctx := context.Background()
	res, err := Transcribe(ctx, tmpPath, "audio/mpeg")
	if err != nil {
		return saveErrorMarker(app, transcriptCol, episode, fmt.Errorf("transcribe: %w", err))
	}

	rec := existing
	if rec == nil {
		rec = core.NewRecord(transcriptCol)
		rec.Set("episode", episode.Id)
	}
	rec.Set("content_hash", hashStr)
	rec.Set("model", env.GetGoogleSTTModel())
	rec.Set("language", res.Language)

	if !res.Suitable || res.Transcript == "" {
		rec.Set("duration_seconds", 0)
		rec.Set("transcript", "")
		rec.Set("segments", nil)
		app.Logger().Info("[podcasttranscribe] episodio non adatto, marker",
			"episode", episode.Id, "reason", res.Reason)
		return app.Save(rec)
	}

	segJSON, err := json.Marshal(res.Segments)
	if err != nil {
		return saveErrorMarker(app, transcriptCol, episode, fmt.Errorf("marshal segments: %w", err))
	}

	dur := res.DurationSeconds
	if dur <= 0 {
		// Fallback: usa duration dell'episodio se Gemini non ne deduce.
		dur = episode.GetInt("duration_seconds")
		if dur <= 0 {
			dur = 1 // qualcosa di positivo per non far scattare il marker errore
		}
	}
	rec.Set("transcript", res.Transcript)
	rec.Set("segments", string(segJSON))
	rec.Set("duration_seconds", dur)
	if err := app.Save(rec); err != nil {
		return fmt.Errorf("save transcript: %w", err)
	}
	app.Logger().Info("[podcasttranscribe] transcript salvato",
		"episode", episode.Id, "duration_s", dur,
		"transcript_chars", len(res.Transcript),
		"segments", len(res.Segments))
	return nil
}

// readEpisodeAudio scarica il file audio dell'episodio dal filesystem PB.
// L'episode passa il nome file della prima slot (file field con maxSelect=1).
func readEpisodeAudio(app core.App, episode *core.Record, filename string) ([]byte, error) {
	fsys, err := app.NewFilesystem()
	if err != nil {
		return nil, err
	}
	defer func() { _ = fsys.Close() }()
	key := episode.BaseFilesPath() + "/" + filename
	r, err := fsys.GetReader(key)
	if err != nil {
		return nil, err
	}
	defer func() { _ = r.Close() }()
	return io.ReadAll(r)
}

// saveErrorMarker scrive duration_seconds = -1 + content_hash = errore (max
// 500 chars). Verra` riprovato al prossimo hook trigger perche` l'hash non
// matchera` mai una sha256 fresca.
func saveErrorMarker(app core.App, col *core.Collection, episode *core.Record, origErr error) error {
	existing, _ := app.FindFirstRecordByData(col.Id, "episode", episode.Id)
	rec := existing
	if rec == nil {
		rec = core.NewRecord(col)
		rec.Set("episode", episode.Id)
	}
	rec.Set("content_hash", truncate(origErr.Error(), 500))
	rec.Set("duration_seconds", -1)
	rec.Set("transcript", "")
	rec.Set("segments", nil)
	rec.Set("model", env.GetGoogleSTTModel())
	if err := app.Save(rec); err != nil {
		app.Logger().Error("[podcasttranscribe] save marker errore fallito",
			"episode", episode.Id, "save_err", err, "orig_err", origErr)
	}
	return origErr
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}
