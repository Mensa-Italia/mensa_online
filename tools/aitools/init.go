package aitools

import (
	"context"
	"mensadb/tools/env"

	"google.golang.org/genai"
)

func GetAIClient() *genai.Client {
	ctx := context.Background()
	client, _ := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:  env.GetGeminiKey(),
		Backend: genai.BackendGeminiAPI,
	})
	return client
}
