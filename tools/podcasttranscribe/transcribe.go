package podcasttranscribe

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/tidwall/gjson"
	"google.golang.org/genai"

	"mensadb/tools/env"
)

const (
	// Timeout end-to-end: upload + transcribe per episodio. 1h di audio
	// con segments richiede tipicamente 1-3 min ma teniamo margine ampio
	// per i lunghi (es. interviste >2h).
	transcribeTimeout = 30 * time.Minute
)

// retryBackoffs: stessa cadenza esponenziale di quidaudio (2/4/8/16 min)
// su errori transient (429 quota, 5xx, network).
var retryBackoffs = []time.Duration{
	2 * time.Minute,
	4 * time.Minute,
	8 * time.Minute,
	16 * time.Minute,
}

// Semaphore limita le chiamate concorrenti a Gemini per non sforare quota.
var (
	semOnce sync.Once
	sem     chan struct{}
)

func getSemaphore() chan struct{} {
	semOnce.Do(func() {
		sem = make(chan struct{}, env.GetGeminiTranscribeConcurrency())
	})
	return sem
}

var retryDelayRE = regexp.MustCompile(`retry in (\d+(?:\.\d+)?)s`)

// Segment e` un blocco temporizzato di audio trascritto. Permette al client
// di fare seek puntuale (mensa://podcast/<id>?t=<start_seconds>).
type Segment struct {
	StartSeconds float64 `json:"start_seconds"`
	EndSeconds   float64 `json:"end_seconds"`
	Text         string  `json:"text"`
}

// Result e` quello che ritorna Transcribe: testo piatto + segments + flag
// di adattabilita`. Suitable=false significa che l'audio non e` un podcast
// parlato (es. solo musica) e non vale la pena indicizzarlo.
type Result struct {
	Suitable        bool      `json:"suitable"`
	Reason          string    `json:"reason"`
	Language        string    `json:"language"`
	Transcript      string    `json:"transcript"`
	Segments        []Segment `json:"segments"`
	DurationSeconds int       `json:"-"`
}

const transcribePrompt = `Sei un transcribe professionale di podcast in italiano.
Trascrivi accuratamente l'audio fornito, in italiano, mantenendo:
- punteggiatura naturale (virgole, punti, punti interrogativi)
- nomi propri capitalizzati come pronunciati
- nessuna omissione o riformulazione: cerca di essere verbatim ma puoi
  rimuovere riempitivi inutili ("uhm", "ehh") se non aggiungono significato

Decidi anche se l'audio e' adatto a essere indicizzato:
- suitable = false se l'audio e' principalmente musica strumentale, silenzio,
  audio rotto, o testo cosi' breve (< 100 caratteri di parlato) da non
  meritare indicizzazione. In quel caso transcript e segments possono essere
  vuoti, valorizza solo reason.
- suitable = true altrimenti.

Restituisci anche dei segments temporali: ogni segment copre un blocco
coerente (singolo discorso, paragrafo, cambio di argomento) di circa
15-60 secondi. start_seconds/end_seconds sono i timestamp espressi in
secondi (decimali ammessi) dall'inizio dell'audio. text e' la trascrizione
di QUEL blocco. La concatenazione dei segments deve coprire l'intero audio.

Identifica la lingua principale (campo language) come codice ISO (es. it-IT).`

// Transcribe carica l'audio su Gemini Files API e chiede la trascrizione
// strutturata. Ritorna Result con transcript + segments. In caso di errore
// transient ritenta con backoff. Errori non-transient propagano subito.
func Transcribe(ctx context.Context, audioPath, mimeType string) (*Result, error) {
	c := getClient()
	if c == nil {
		return nil, fmt.Errorf("gemini client non disponibile")
	}
	if mimeType == "" {
		mimeType = "audio/mpeg"
	}

	// Caricamento file (Files API: supporta fino a ~2GB e 9.5h).
	audioBytes, err := os.ReadFile(audioPath)
	if err != nil {
		return nil, fmt.Errorf("read audio: %w", err)
	}

	uploadCtx, uploadCancel := context.WithTimeout(ctx, transcribeTimeout)
	defer uploadCancel()
	fileRef, err := c.Files.Upload(uploadCtx,
		io.NopCloser(bytes.NewReader(audioBytes)),
		&genai.UploadFileConfig{
			DisplayName: "podcast_episode_audio",
			MIMEType:    mimeType,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("upload audio: %w", err)
	}

	contents := []*genai.Content{{
		Role: genai.RoleUser,
		Parts: []*genai.Part{
			genai.NewPartFromFile(*fileRef),
		},
	}}

	systemParts := []*genai.Part{genai.NewPartFromText(transcribePrompt)}

	config := &genai.GenerateContentConfig{
		SystemInstruction: &genai.Content{Parts: systemParts},
		ResponseMIMEType:  "application/json",
		ResponseSchema: &genai.Schema{
			Type:     genai.TypeObject,
			Required: []string{"suitable", "reason"},
			Properties: map[string]*genai.Schema{
				"suitable": {Type: genai.TypeBoolean},
				"reason":   {Type: genai.TypeString},
				"language": {Type: genai.TypeString},
				"transcript": {Type: genai.TypeString},
				"segments": {
					Type: genai.TypeArray,
					Items: &genai.Schema{
						Type:     genai.TypeObject,
						Required: []string{"start_seconds", "end_seconds", "text"},
						Properties: map[string]*genai.Schema{
							"start_seconds": {Type: genai.TypeNumber},
							"end_seconds":   {Type: genai.TypeNumber},
							"text":          {Type: genai.TypeString},
						},
					},
				},
			},
		},
	}

	var lastErr error
	s := getSemaphore()
	for attempt := 0; attempt <= len(retryBackoffs); attempt++ {
		s <- struct{}{}
		callCtx, cancel := context.WithTimeout(ctx, transcribeTimeout)
		resp, err := c.Models.GenerateContent(callCtx, env.GetGeminiTranscribeModel(), contents, config)
		cancel()
		<-s

		if err == nil {
			return parseResult(resp), nil
		}
		lastErr = err
		if !isTransient(err) {
			return nil, fmt.Errorf("transcribe: %w", err)
		}
		if attempt == len(retryBackoffs) {
			break
		}
		delay := retryBackoffs[attempt]
		if hinted, ok := extractRetryDelay(err); ok && hinted > 0 && hinted < delay {
			delay = hinted
		}
		log.Printf("[podcasttranscribe] transient, retry in %s (tentativo %d/%d): %v",
			delay, attempt+1, len(retryBackoffs), err)
		time.Sleep(delay)
	}
	return nil, fmt.Errorf("transcribe: %d retry esauriti: %w", len(retryBackoffs), lastErr)
}

func parseResult(resp *genai.GenerateContentResponse) *Result {
	if resp == nil {
		return &Result{}
	}
	txt := resp.Text()
	parsed := gjson.Parse(txt)
	res := &Result{
		Suitable:   parsed.Get("suitable").Bool(),
		Reason:     parsed.Get("reason").String(),
		Language:   parsed.Get("language").String(),
		Transcript: parsed.Get("transcript").String(),
	}
	if res.Language == "" {
		res.Language = "it-IT"
	}
	for _, seg := range parsed.Get("segments").Array() {
		res.Segments = append(res.Segments, Segment{
			StartSeconds: seg.Get("start_seconds").Float(),
			EndSeconds:   seg.Get("end_seconds").Float(),
			Text:         seg.Get("text").String(),
		})
	}
	if n := len(res.Segments); n > 0 {
		res.DurationSeconds = int(res.Segments[n-1].EndSeconds)
	}
	return res
}

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

func extractRetryDelay(err error) (time.Duration, bool) {
	if err == nil {
		return 0, false
	}
	if m := retryDelayRE.FindStringSubmatch(err.Error()); m != nil {
		if seconds, perr := strconv.ParseFloat(m[1], 64); perr == nil {
			return time.Duration((seconds + 1) * float64(time.Second)), true
		}
	}
	return 0, false
}

// nopErr restituisce un errore generico per usi interni (placeholder).
var _ = errors.New
