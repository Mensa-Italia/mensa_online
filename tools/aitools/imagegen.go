package aitools

import (
	"context"
	"errors"
	"fmt"
	"log"
	"mensadb/tools/env"
	"strings"
	"time"

	"google.golang.org/genai"
)

var ErrNoImageBytes = errors.New("il modello non ha restituito byte immagine")

// imageGenAttempts e` il numero di tentativi per una singola generazione.
// La Gemini API sotto carico risponde sporadicamente 404 con body VUOTO
// (osservato in diretta su gemini-3.1-flash-image e gemini-3-pro-image:
// stessa identica richiesta, 404 a raffica e poi 200 al retry). Non e` un
// modello inesistente — quello risponde 404 con un body JSON esplicito —
// quindi va distinto dal ritiro vero e proprio e ritentato.
const imageGenAttempts = 3

// GenerateImageBytes genera un'immagine con il modello immagine di Gemini
// (env GEMINI_IMAGE_MODEL) e ne ritorna i byte grezzi.
//
// NOTA STORICA: fino a questa modifica si usava client.Models.GenerateImages,
// che colpisce la superficie {model}:predict, esclusiva di Imagen. Google ha
// ritirato la famiglia imagen-* dalla Gemini API — models/imagen-4.0-generate-001
// risponde 404 NOT_FOUND — e questo ha fermato sia i timbri sia le copertine
// AI degli eventi. I modelli immagine rimasti espongono solo generateContent,
// quindi l'immagine va chiesta come modalita` di risposta e riletta da
// InlineData. Cambiare solo il nome del modello dentro GenerateImages NON
// funziona: e` proprio l'endpoint a non esistere piu`.
//
// aspectRatio accetta i valori supportati da genai.ImageConfig ("1:1", "16:9",
// ...); se vuoto il modello usa il suo default.
func GenerateImageBytes(ctx context.Context, prompt string, aspectRatio string) ([]byte, error) {
	client := GetAIClient()
	if client == nil {
		return nil, errStampClientUnavailable
	}

	model := env.GetGeminiImageModel()
	config := &genai.GenerateContentConfig{
		// Senza IMAGE il modello risponde solo testo.
		ResponseModalities: []string{"TEXT", "IMAGE"},
	}
	if aspectRatio != "" {
		config.ImageConfig = &genai.ImageConfig{
			AspectRatio: aspectRatio,
			ImageSize:   "1K",
		}
	}

	var lastErr error
	for attempt := 1; attempt <= imageGenAttempts; attempt++ {
		result, err := client.Models.GenerateContent(ctx, model, genai.Text(prompt), config)
		if err != nil {
			lastErr = err
			if !isRetriableGenAIError(err) {
				return nil, fmt.Errorf("generazione immagine con %s: %w", model, err)
			}
			log.Printf("GenerateImageBytes: tentativo %d/%d fallito su %s: %v", attempt, imageGenAttempts, model, err)
		} else if data := firstInlineImage(result); len(data) > 0 {
			return data, nil
		} else {
			// Risposta valida ma senza immagine: quasi sempre un blocco
			// safety. Il motivo sta nel finish reason, non nell'errore.
			lastErr = fmt.Errorf("%w (modello %s, motivo: %s)", ErrNoImageBytes, model, finishReasons(result))
			log.Printf("GenerateImageBytes: %v", lastErr)
		}

		if attempt < imageGenAttempts {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Duration(attempt) * 2 * time.Second):
			}
		}
	}

	return nil, lastErr
}

// firstInlineImage estrae i byte della prima parte immagine della risposta.
// I modelli immagine restituiscono spesso anche una parte testuale di
// commento, che va saltata invece di far fallire l'estrazione.
func firstInlineImage(result *genai.GenerateContentResponse) []byte {
	if result == nil {
		return nil
	}
	for _, candidate := range result.Candidates {
		if candidate == nil || candidate.Content == nil {
			continue
		}
		for _, part := range candidate.Content.Parts {
			if part == nil || part.InlineData == nil {
				continue
			}
			// Va controllata la lunghezza, non solo il puntatore: una parte
			// filtrata arriva non-nil ma vuota, ed e` esattamente cosi` che
			// il vecchio codice mascherava i blocchi safety da
			// "image: unknown format".
			if len(part.InlineData.Data) > 0 {
				return part.InlineData.Data
			}
		}
	}
	return nil
}

// finishReasons raccoglie i finish reason dei candidati per rendere
// diagnosticabile una risposta senza immagine.
func finishReasons(result *genai.GenerateContentResponse) string {
	if result == nil {
		return "nessuna risposta"
	}
	reasons := make([]string, 0, len(result.Candidates))
	for _, candidate := range result.Candidates {
		if candidate != nil && candidate.FinishReason != "" {
			reasons = append(reasons, string(candidate.FinishReason))
		}
	}
	if len(reasons) == 0 {
		return "nessun finish reason"
	}
	return strings.Join(reasons, ", ")
}

// isRetriableGenAIError distingue i guasti transitori da quelli definitivi.
// Un modello ritirato risponde 404 con un messaggio esplicito e non va
// ritentato; il 404 a body vuoto e i 429/5xx sono transitori.
func isRetriableGenAIError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "is not found") || strings.Contains(msg, "not supported") {
		return false
	}
	return true
}
