package quidaudio

import (
	"context"
	"log"
	"sync"

	"google.golang.org/genai"

	"mensadb/tools/env"
)

// Il client Gemini per il TTS e` separato da quello di aitools: usa una
// API key dedicata (GEMINI_TTS_KEY, con fallback su GEMINI_KEY) cosi` si
// possono isolare quota, billing e rate limit.
var (
	ttsClientOnce sync.Once
	ttsClient     *genai.Client
	ttsClientErr  error
)

func getTTSClient() *genai.Client {
	ttsClientOnce.Do(func() {
		key := env.GetGeminiTTSKey()
		if key == "" {
			log.Printf("[quidaudio] GEMINI_TTS_KEY non impostata e nessun fallback GEMINI_KEY, TTS disabilitato")
			return
		}
		ttsClient, ttsClientErr = genai.NewClient(context.Background(), &genai.ClientConfig{
			APIKey:  key,
			Backend: genai.BackendGeminiAPI,
		})
		if ttsClientErr != nil {
			log.Printf("[quidaudio] init Gemini TTS client: %v", ttsClientErr)
		}
	})
	if ttsClientErr != nil {
		return nil
	}
	return ttsClient
}
