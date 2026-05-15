package quidaudio

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/filesystem"

	"mensadb/tools/env"
)

// Generate orchestra il flusso completo per un singolo articolo Quid:
//
//   1. calcola hash sha256 sul body
//   2. se esiste gia` un record quid_articles_audio con lo stesso hash → noop
//   3. chiama Gemini text per decidere suitable + cleaned_text
//   4. se !suitable → salva record marker (hash + audio vuoto + duration 0)
//   5. se suitable → genera PCM con Gemini TTS, lo encodea MP3, salva tutto
//
// In caso di errore (preprocess/synthesize/encode) salva un record con
// duration_seconds = -1 e content_hash vuoto: cosi` l'errore e` visibile
// dall'admin panel (cercando duration_seconds<0) e al prossimo hook trigger
// l'articolo viene riprovato (il content_hash vuoto impedisce lo skip).
//
// Convenzione duration_seconds:
//   >0  : audio generato correttamente
//    0  : articolo non adatto a TTS (quiz, troppo corto, ...). Non riprovare.
//   -1  : errore durante la generazione. Verra` riprovato.
func Generate(app core.App, article *core.Record) error {
	body := article.GetString("body")
	if body == "" {
		return nil
	}

	hash := sha256.Sum256([]byte(body))
	hashStr := hex.EncodeToString(hash[:])

	audioCol, err := app.FindCollectionByNameOrId("quid_articles_audio")
	if err != nil {
		return fmt.Errorf("find quid_articles_audio: %w", err)
	}

	existing, _ := app.FindFirstRecordByData(audioCol.Id, "article", article.Id)
	if existing != nil && existing.GetString("content_hash") == hashStr {
		return nil
	}

	pre, err := Preprocess(article.GetString("title"), body)
	if err != nil {
		return saveErrorMarker(app, audioCol, existing, article, fmt.Errorf("preprocess: %w", err))
	}

	// Selezione voce in base al gender dell'autore (decidi nel preprocess):
	// femminile -> GEMINI_TTS_VOICE_FEMALE (default Zephyr)
	// maschile o ignoto -> GEMINI_TTS_VOICE (default Charon)
	voice := env.GetGeminiTTSVoice()
	if pre.AuthorGender == "female" {
		voice = env.GetGeminiTTSVoiceFemale()
	}

	rec := existing
	if rec == nil {
		rec = core.NewRecord(audioCol)
		rec.Set("article", article.Id)
	}

	if !pre.Suitable || pre.CleanedText == "" {
		// Marker "non adatto": fissa l'hash perche` non vogliamo riprovare
		// finche` il body non cambia.
		rec.Set("content_hash", hashStr)
		rec.Set("voice", voice)
		rec.Set("audio", nil)
		rec.Set("duration_seconds", 0)
		app.Logger().Info("[quidaudio] articolo non adatto a TTS, salvato marker",
			"article", article.Id, "reason", pre.Reason)
		return app.Save(rec)
	}

	pcm, err := Synthesize(pre.CleanedText, voice)
	if err != nil {
		return saveErrorMarker(app, audioCol, existing, article, fmt.Errorf("synthesize: %w", err))
	}
	mp3, err := EncodePCMToMP3(pcm)
	if err != nil {
		return saveErrorMarker(app, audioCol, existing, article, fmt.Errorf("encode mp3: %w", err))
	}

	file, err := filesystem.NewFileFromBytes(mp3, fmt.Sprintf("quid_audio_%s.mp3", article.Id))
	if err != nil {
		return saveErrorMarker(app, audioCol, existing, article, fmt.Errorf("wrap file: %w", err))
	}
	rec.Set("content_hash", hashStr)
	rec.Set("voice", voice)
	rec.Set("audio", file)
	rec.Set("duration_seconds", pcmDurationSeconds(pcm))

	if err := app.Save(rec); err != nil {
		return fmt.Errorf("save audio record: %w", err)
	}
	app.Logger().Info("[quidaudio] audio generato",
		"article", article.Id, "duration_s", pcmDurationSeconds(pcm), "size_kb", len(mp3)/1024)
	return nil
}

// saveErrorMarker registra un record con duration_seconds = -1 e content_hash
// vuoto cosi` da:
//   1. rendere visibile l'errore dal pannello PB (filtro duration_seconds < 0)
//   2. lasciare campo libero al prossimo retry (hash mismatch → si rigenera)
//
// Ritorna l'errore originale (wrappato) cosi` il chiamante puo` loggarlo;
// non si propaga oltre nella catena di goroutine.
func saveErrorMarker(app core.App, audioCol *core.Collection, existing *core.Record, article *core.Record, origErr error) error {
	rec := existing
	if rec == nil {
		rec = core.NewRecord(audioCol)
		rec.Set("article", article.Id)
	}
	rec.Set("content_hash", "")
	rec.Set("voice", "")
	rec.Set("audio", nil)
	rec.Set("duration_seconds", -1)
	if err := app.Save(rec); err != nil {
		// Se persino salvare il marker fallisce, non possiamo fare di meglio.
		app.Logger().Error("[quidaudio] salvataggio marker errore fallito",
			"article", article.Id, "save_err", err, "orig_err", origErr)
	}
	return origErr
}
