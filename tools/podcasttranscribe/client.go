package podcasttranscribe

import (
	"context"
	"log"
	"sync"

	"google.golang.org/genai"

	"mensadb/tools/env"
)

// Client Gemini dedicato alla trascrizione, separato da aitools per isolare
// quota e billing. Singleton lazy.
var (
	clientOnce sync.Once
	client     *genai.Client
	clientErr  error
)

func getClient() *genai.Client {
	clientOnce.Do(func() {
		key := env.GetGeminiTranscribeKey()
		if key == "" {
			log.Printf("[podcasttranscribe] GEMINI_TRANSCRIBE_KEY non impostata e nessun fallback GEMINI_KEY, transcribe disabilitato")
			return
		}
		client, clientErr = genai.NewClient(context.Background(), &genai.ClientConfig{
			APIKey:  key,
			Backend: genai.BackendGeminiAPI,
		})
		if clientErr != nil {
			log.Printf("[podcasttranscribe] init Gemini client: %v", clientErr)
		}
	})
	if clientErr != nil {
		return nil
	}
	return client
}
