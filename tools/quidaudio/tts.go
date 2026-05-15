package quidaudio

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/genai"

	"mensadb/tools/env"
)

const synthesizeTimeout = 180 * time.Second

// Synthesize chiama il modello TTS di Gemini con la voce e lo stile di
// narrazione configurati e ritorna l'audio in PCM 16-bit signed LE, 24kHz,
// mono. Il chiamante deve incapsularlo in WAV o transcoderlo in MP3.
//
// Se il client TTS non e` disponibile (no API key) ritorna (nil, err) e il
// chiamante puo` decidere di skippare in silenzio.
func Synthesize(text string) ([]byte, error) {
	client := getTTSClient()
	if client == nil {
		return nil, fmt.Errorf("gemini TTS client non disponibile")
	}

	ctx, cancel := context.WithTimeout(context.Background(), synthesizeTimeout)
	defer cancel()

	// Lo style prompt e` parte del testo input: la documentazione Gemini TTS
	// raccomanda di prependerlo seguito da due punti per far modulare la voce.
	style := env.GetGeminiTTSStylePrompt()
	prompt := text
	if style != "" {
		prompt = style + ":\n" + text
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
					VoiceName: env.GetGeminiTTSVoice(),
				},
			},
		},
	}

	resp, err := client.Models.GenerateContent(ctx, env.GetGeminiTTSModel(), contents, config)
	if err != nil {
		return nil, fmt.Errorf("TTS generate: %w", err)
	}
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
