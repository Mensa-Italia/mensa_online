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
)

// retryBackoffs definisce i delay tra tentativi su errori transient
// (429 senza retry-delay esplicito, 504, timeout, ecc.).
// Pattern esponenziale: 2 / 4 / 8 / 16 minuti. Dopo 4 tentativi il
// chiamante salva il marker duration_seconds = -1.
var retryBackoffs = []time.Duration{
	2 * time.Minute,
	4 * time.Minute,
	8 * time.Minute,
	16 * time.Minute,
}

// ttsSem limita le chiamate TTS concorrenti. Dimensione configurata da
// GEMINI_TTS_CONCURRENCY (default 2). Inizializzato lazy al primo uso.
var (
	ttsSemOnce sync.Once
	ttsSem     chan struct{}
)

func getSemaphore() chan struct{} {
	ttsSemOnce.Do(func() {
		ttsSem = make(chan struct{}, env.GetGeminiTTSConcurrency())
	})
	return ttsSem
}

// retryDelayRE estrae il "Please retry in 7.354591223s." dal messaggio di
// errore Gemini 429. La struttura del json error con RetryInfo non viene
// esposta dal SDK Go come tipo strutturato, quindi parsiamo dal testo.
var retryDelayRE = regexp.MustCompile(`retry in (\d+(?:\.\d+)?)s`)

// Synthesize chiama il modello TTS di Gemini con la voce indicata + stile
// di narrazione configurato. Ritorna l'audio in PCM 16-bit signed LE, 24kHz,
// mono.
//
// Concorrenza limitata dal semaphore (env GEMINI_TTS_CONCURRENCY, default 2)
// per non sforare la quota. Errori transient (429, 504, timeout) ritentati
// con backoff esponenziale 2/4/8/16 min: per i 429 con retry-delay esplicito
// usa quello suggerito da Gemini se piu` corto.
//
// Se voiceName e` vuoto, fallback su env GEMINI_TTS_VOICE.
func Synthesize(text, voiceName string) ([]byte, error) {
	client := getTTSClient()
	if client == nil {
		return nil, fmt.Errorf("gemini TTS client non disponibile")
	}

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
	sem := getSemaphore()
	for attempt := 0; attempt <= len(retryBackoffs); attempt++ {
		// Acquisisci slot semaphore solo per la chiamata API, lascialo libero
		// durante l'attesa del backoff cosi` altri goroutine possono procedere.
		sem <- struct{}{}
		ctx, cancel := context.WithTimeout(context.Background(), synthesizeTimeout)
		audio, err := streamSynthesize(ctx, client, contents, config)
		cancel()
		<-sem

		if err == nil {
			return audio, nil
		}

		lastErr = err
		if !isTransient(err) {
			return nil, fmt.Errorf("TTS generate: %w", err)
		}
		if attempt == len(retryBackoffs) {
			break
		}

		// Per i 429 Gemini suggerisce un retry-delay esplicito (di solito
		// pochi secondi). Se piu` corto del nostro backoff, usiamo il suo
		// — siamo gentili con la API.
		delay := retryBackoffs[attempt]
		if hinted, ok := extractRetryDelay(err); ok && hinted > 0 && hinted < delay {
			delay = hinted
		}
		log.Printf("[quidaudio] errore transient, retry in %s (tentativo %d/%d): %v",
			delay, attempt+1, len(retryBackoffs), err)
		time.Sleep(delay)
	}
	return nil, fmt.Errorf("TTS generate: %d retry esauriti: %w", len(retryBackoffs), lastErr)
}

// isTransient identifica gli errori per cui ha senso ritentare.
// 429 quota, 504 deadline, context timeout, errori di rete.
func isTransient(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "RESOURCE_EXHAUSTED") ||
		strings.Contains(msg, "Error 429") ||
		strings.Contains(msg, "quota") ||
		strings.Contains(msg, "DEADLINE_EXCEEDED") ||
		strings.Contains(msg, "Error 504") ||
		strings.Contains(msg, "Error 503") ||
		strings.Contains(msg, "UNAVAILABLE") ||
		strings.Contains(msg, "context deadline exceeded") ||
		strings.Contains(msg, "EOF") ||
		strings.Contains(msg, "connection reset")
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

// extractRetryDelay esamina un errore Gemini per estrarre l'eventuale
// "Please retry in X.Xs" suggerito sulle 429. Ritorna (delay, true) se
// trovato; (0, false) altrimenti.
func extractRetryDelay(err error) (time.Duration, bool) {
	if err == nil {
		return 0, false
	}
	if m := retryDelayRE.FindStringSubmatch(err.Error()); m != nil {
		if seconds, perr := strconv.ParseFloat(m[1], 64); perr == nil {
			// piccolo cuscinetto: +1s per assorbire jitter / clock skew
			return time.Duration((seconds+1) * float64(time.Second)), true
		}
	}
	return 0, false
}

