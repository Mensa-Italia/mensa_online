package podcasttranscribe

import (
	"context"
	"encoding/base64"
	"errors"
	"log"
	"sync"

	speech "cloud.google.com/go/speech/apiv2"
	"google.golang.org/api/option"

	"mensadb/tools/env"
)

// Client Google Cloud Speech-to-Text v2 (modello chirp_2). Singleton lazy,
// inizializzato al primo uso. Le credenziali arrivano dall'env in base64:
//   - GOOGLE_STT_CREDENTIALS_JSON (preferita)
//   - FIREBASE_AUTH_KEY (fallback: spesso e` lo stesso service account)
var (
	clientOnce sync.Once
	client     *speech.Client
	clientErr  error
)

func getClient(ctx context.Context) (*speech.Client, error) {
	clientOnce.Do(func() {
		raw := env.GetGoogleSTTCredentialsJSON()
		if raw == "" {
			raw = env.GetFireBaseAuthKey()
		}
		if raw == "" {
			clientErr = errors.New("GOOGLE_STT_CREDENTIALS_JSON / FIREBASE_AUTH_KEY non impostate")
			log.Printf("[podcasttranscribe] %v", clientErr)
			return
		}
		creds, err := base64.StdEncoding.DecodeString(raw)
		if err != nil {
			clientErr = err
			log.Printf("[podcasttranscribe] decode credentials base64: %v", err)
			return
		}

		// Endpoint regionale: la console e` su `eu-` per i nostri progetti.
		endpoint := env.GetGoogleSTTEndpoint()
		opts := []option.ClientOption{
			option.WithCredentialsJSON(creds),
		}
		if endpoint != "" {
			opts = append(opts, option.WithEndpoint(endpoint))
		}

		client, clientErr = speech.NewClient(ctx, opts...)
		if clientErr != nil {
			log.Printf("[podcasttranscribe] init STT client: %v", clientErr)
		}
	})
	if clientErr != nil {
		return nil, clientErr
	}
	return client, nil
}
