package aitools

import (
	"context"
	"log"
	"sync"

	"google.golang.org/genai"
	"mensadb/tools/env"
)

var (
	clientOnce sync.Once
	clientInst *genai.Client
	clientErr  error
)

// GetAIClient ritorna il client Gemini condiviso (creato una sola volta).
// In caso di errore di init ritorna nil; il chiamante deve gestire il caso
// in modo fallback-friendly (no panic). Il client NON va mai chiuso dai
// chiamanti: è condiviso tra goroutine/feature dell'applicazione.
func GetAIClient() *genai.Client {
	clientOnce.Do(func() {
		ctx := context.Background()
		clientInst, clientErr = genai.NewClient(ctx, &genai.ClientConfig{
			APIKey:  env.GetGeminiKey(),
			Backend: genai.BackendGeminiAPI,
		})
		if clientErr != nil {
			log.Printf("GetAIClient: init error: %v", clientErr)
		}
	})
	if clientErr != nil {
		return nil
	}
	return clientInst
}
