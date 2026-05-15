package quidaudio

import (
	"context"
	"fmt"
	"time"

	"github.com/tidwall/gjson"
	"google.golang.org/genai"

	"mensadb/tools/aitools"
)

// 120s perche` articoli lunghi + thinking di gemini-3-flash sforavano i 60s
// e ritornavano 504 DEADLINE_EXCEEDED.
const preprocessTimeout = 120 * time.Second

// preprocessPrompt e' il system instruction per il modello text che decide
// se un articolo si presta a un audiolibro e ne produce la versione "pulita"
// per il TTS. Rimuove rumore visivo (link, marker editoriali), normalizza
// punteggiatura strana e espande abbreviazioni che il TTS leggerebbe male.
const preprocessPrompt = `Sei un editor che prepara articoli per la lettura ad alta voce con un sintetizzatore vocale (TTS) di alta qualita'.

Per l'articolo che ti viene fornito decidi:

1. "suitable" = false se:
   - e' un quiz, una griglia, un indice, un puro elenco di nomi, una poesia tipografica
   - e' troppo corto (< 400 caratteri di prosa)
   - e' essenzialmente didascalie di immagini o tabelle
   - il testo perderebbe completamente senso letto ad alta voce
2. "suitable" = true negli altri casi (saggi, racconti, articoli divulgativi, editoriali).

Se suitable = true, restituisci "cleaned_text" con:
   - rimosse URL nude e marker tipo "[continua]" / "[Foto: ...]"
   - rimossi loghi/firme finali, "Quid - XX", numeri di pagina, note in pedice numeriche
   - espanse abbreviazioni ambigue (es. "es." -> "ad esempio", "p.es." -> "per esempio")
   - normalizzata punteggiatura strana (—, …, virgolette tipografiche) verso forme standard
   - paragrafi separati da una riga vuota
   - nessuna alterazione del contenuto: ne' riassumere ne' aggiungere

Se suitable = false, "cleaned_text" puo' essere vuoto. In ogni caso valorizza "reason" con una frase di spiegazione.`

// Preprocessed e' l'output del pre-processore TTS.
type Preprocessed struct {
	Suitable    bool   `json:"suitable"`
	Reason      string `json:"reason"`
	CleanedText string `json:"cleaned_text"`
}

// Preprocess invia titolo + body al modello text di Gemini per decidere se
// vale la pena generare un audiolibro e per ottenere il testo pulito.
//
// In caso di errore client/SDK ritorna (nil, err) senza fare assumptions:
// il chiamante decidera` se ritentare piu` tardi o saltare l'articolo.
func Preprocess(title, body string) (*Preprocessed, error) {
	client := aitools.GetAIClient()
	if client == nil {
		return nil, fmt.Errorf("gemini client non disponibile")
	}

	ctx, cancel := context.WithTimeout(context.Background(), preprocessTimeout)
	defer cancel()

	contents := []*genai.Content{{
		Role: genai.RoleUser,
		Parts: []*genai.Part{
			genai.NewPartFromText("Titolo: " + title + "\n\nTesto:\n" + body),
		},
	}}

	systemParts := []*genai.Part{genai.NewPartFromText(preprocessPrompt)}
	config := &genai.GenerateContentConfig{
		SystemInstruction: &genai.Content{Parts: systemParts},
		ResponseMIMEType:  "application/json",
		ResponseSchema: &genai.Schema{
			Type:     genai.TypeObject,
			Required: []string{"suitable", "reason"},
			Properties: map[string]*genai.Schema{
				"suitable":     {Type: genai.TypeBoolean},
				"reason":       {Type: genai.TypeString},
				"cleaned_text": {Type: genai.TypeString},
			},
		},
	}

	resp, err := client.Models.GenerateContent(ctx, "gemini-3-flash-preview", contents, config)
	if err != nil {
		return nil, fmt.Errorf("gemini generate: %w", err)
	}
	parsed := gjson.Parse(resp.Text())
	out := &Preprocessed{
		Suitable:    parsed.Get("suitable").Bool(),
		Reason:      parsed.Get("reason").String(),
		CleanedText: parsed.Get("cleaned_text").String(),
	}
	return out, nil
}
