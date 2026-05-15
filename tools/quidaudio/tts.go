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
	synthesizeTimeout = 180 * time.Second
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

	// Gemini TTS legge ad alta voce qualsiasi cosa gli arrivi nel content,
	// inclusi i prefissi di stile mal frasati. Per fargli interpretare lo
	// stile come direttiva (non come testo da leggere) lo wrappiamo in un
	// comando esplicito riconoscibile, separato dal contenuto da una riga
	// vuota. Pattern documentato da Google ("Say in a [...] tone: TEXT").
	style := env.GetGeminiTTSStylePrompt()
	prompt := text
	if style != "" {
		prompt = "Leggi ad alta voce in italiano il testo seguente con questo stile di narrazione: " + style + ".\n\n" + text
	}

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
		resp, err := client.Models.GenerateContent(ctx, env.GetGeminiTTSModel(), contents, config)
		cancel()

		if err == nil {
			if len(resp.Candidates) == 0 || resp.Candidates[0].Content == nil {
				return nil, fmt.Errorf("TTS: risposta vuota")
			}
			for _, part := range resp.Candidates[0].Content.Parts {
				if part.InlineData != nil && len(part.InlineData.Data) > 0 {
					return part.InlineData.Data, nil
				}
			}
			return nil, fmt.Errorf("TTS: nessun part audio nella risposta")
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

