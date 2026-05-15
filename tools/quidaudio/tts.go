package quidaudio

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"google.golang.org/genai"

	"mensadb/tools/env"
)

const (
	// Timeout end-to-end per l'intero stream TTS. Gli articoli lunghi possono
	// produrre audio di 8-12 minuti che richiede tempo proporzionale; 600s
	// copre il caso peggiore con margine.
	synthesizeTimeout = 600 * time.Second
	maxRetries429     = 4
	defaultBackoff    = 10 * time.Second
)

// ttsMu serializza le chiamate TTS: la quota Gemini TTS e` 10k token/min
// e la backfill in parallelo le sforava regolarmente. Una sola chiamata
// alla volta + retry sul 429 mantiene tutto sotto soglia con zero perdita.
var ttsMu sync.Mutex

// retryDelayRE estrae il "Please retry in 7.354591223s." dal messaggio di
// errore Gemini 429. La struttura del json error con RetryInfo non viene
// esposta dal SDK Go come tipo strutturato, quindi parsiamo dal testo.
var retryDelayRE = regexp.MustCompile(`retry in (\d+(?:\.\d+)?)s`)

// Synthesize chiama il modello TTS di Gemini con la voce indicata + stile
// di narrazione configurato. Ritorna l'audio in PCM 16-bit signed LE, 24kHz,
// mono. Serializza le chiamate (lock globale) per non sforare la quota e
// ritenta su 429 usando il retry_delay restituito da Gemini.
// Se voiceName e` vuoto, fallback su env GEMINI_TTS_VOICE.
func Synthesize(text, voiceName string) ([]byte, error) {
	client := getTTSClient()
	if client == nil {
		return nil, fmt.Errorf("gemini TTS client non disponibile")
	}

	ttsMu.Lock()
	defer ttsMu.Unlock()

	if voiceName == "" {
		voiceName = env.GetGeminiTTSVoice()
	}

	// Formato strutturato richiesto da Gemini TTS: header markdown distinti
	// per Audio Profile (descrizione voce), Director's note (stile narrazione)
	// e Transcript (contenuto da leggere). Solo il contenuto sotto Transcript
	// viene effettivamente letto; il resto modula la voce. Senza questa
	// struttura il modello legge anche le direttive.
	audioProfile := env.GetGeminiTTSStylePrompt()
	directorNote := env.GetGeminiTTSDirectorNote()
	prompt := buildTTSPrompt(audioProfile, directorNote, text)

	contents := []*genai.Content{{
		Role:  genai.RoleUser,
		Parts: []*genai.Part{genai.NewPartFromText(prompt)},
	}}

	config := &genai.GenerateContentConfig{
		ResponseModalities: []string{string(genai.ModalityAudio)},
		SpeechConfig: &genai.SpeechConfig{
			LanguageCode: "it-IT",
			VoiceConfig: &genai.VoiceConfig{
				PrebuiltVoiceConfig: &genai.PrebuiltVoiceConfig{
					VoiceName: voiceName,
				},
			},
		},
	}

	var lastErr error
	for attempt := 0; attempt < maxRetries429; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), synthesizeTimeout)
		audio, err := streamSynthesize(ctx, client, contents, config)
		cancel()

		if err == nil {
			return audio, nil
		}

		lastErr = err
		delay, isQuota := extractRetryDelay(err)
		if !isQuota {
			// errore non-quota: niente retry
			return nil, fmt.Errorf("TTS generate: %w", err)
		}
		if delay == 0 {
			delay = defaultBackoff * time.Duration(1<<attempt)
		}
		log.Printf("[quidaudio] 429 quota, retry in %s (tentativo %d/%d)", delay, attempt+1, maxRetries429)
		time.Sleep(delay)
	}
	return nil, fmt.Errorf("TTS generate: quota esaurita dopo %d retry: %w", maxRetries429, lastErr)
}

// streamSynthesize itera la sequenza di chunk PCM restituiti da Gemini TTS
// in streaming e li concatena. Lo streaming evita i 504 DEADLINE_EXCEEDED
// che colpivano gli articoli lunghi sul non-streaming endpoint.
func streamSynthesize(ctx context.Context, client *genai.Client, contents []*genai.Content, config *genai.GenerateContentConfig) ([]byte, error) {
	var buf []byte
	var streamErr error
	for resp, err := range client.Models.GenerateContentStream(ctx, env.GetGeminiTTSModel(), contents, config) {
		if err != nil {
			streamErr = err
			break
		}
		if resp == nil || len(resp.Candidates) == 0 || resp.Candidates[0].Content == nil {
			continue
		}
		for _, part := range resp.Candidates[0].Content.Parts {
			if part.InlineData != nil && len(part.InlineData.Data) > 0 {
				buf = append(buf, part.InlineData.Data...)
			}
		}
	}
	if streamErr != nil {
		return nil, streamErr
	}
	if len(buf) == 0 {
		return nil, fmt.Errorf("TTS: stream vuoto")
	}
	return buf, nil
}

// buildTTSPrompt costruisce il prompt strutturato che Gemini TTS riconosce:
// sezioni "# Audio Profile", "# Director's note", "## Transcript:" con il
// contenuto sotto l'ultima. Pattern preso dai sample ufficiali Google.
func buildTTSPrompt(audioProfile, directorNote, transcript string) string {
	var b strings.Builder
	b.WriteString("Read the following transcript based on the audio profile and director's note.\n\n")
	if audioProfile != "" {
		b.WriteString("# Audio Profile\n")
		b.WriteString(audioProfile)
		b.WriteString("\n\n")
	}
	if directorNote != "" {
		b.WriteString("# Director's note\n")
		b.WriteString("Style: ")
		b.WriteString(directorNote)
		b.WriteString("\n\n")
	}
	b.WriteString("## Transcript:\n")
	b.WriteString(transcript)
	return b.String()
}

// extractRetryDelay esamina un errore Gemini per capire se e` un 429 e con
// quale delay riprovare. Ritorna (delay, true) se e` quota; (0, false)
// altrimenti.
func extractRetryDelay(err error) (time.Duration, bool) {
	if err == nil {
		return 0, false
	}
	msg := err.Error()
	if !(strings.Contains(msg, "RESOURCE_EXHAUSTED") || strings.Contains(msg, "Error 429") || strings.Contains(msg, "quota")) {
		return 0, false
	}
	if m := retryDelayRE.FindStringSubmatch(msg); m != nil {
		if seconds, perr := strconv.ParseFloat(m[1], 64); perr == nil {
			// piccolo cuscinetto: +1s per assorbire jitter / clock skew
			return time.Duration((seconds+1)*float64(time.Second)), true
		}
	}
	return 0, true
}

