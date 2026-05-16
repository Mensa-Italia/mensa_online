package podcasttranscribe

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	speech "cloud.google.com/go/speech/apiv2"
	"cloud.google.com/go/speech/apiv2/speechpb"

	"mensadb/tools/env"
)

const (
	// Soglia sotto cui Recognize sync v2 accetta l'audio senza dover passare
	// dal BatchRecognize (con GCS). Lasciamo 5s di margine sui 60s nominali.
	chunkSeconds = 55

	// Timeout per ogni call sync Recognize su un chunk.
	chunkTimeout = 90 * time.Second

	// Timeout end-to-end per la trascrizione di un intero episodio
	// (incluso ffmpeg + tutti i chunk in parallelo).
	totalTimeout = 30 * time.Minute
)

// retryBackoffs: cadenza esponenziale 2/4/8/16 min su errori transient
// del singolo chunk (quota / 5xx).
var retryBackoffs = []time.Duration{
	2 * time.Minute,
	4 * time.Minute,
	8 * time.Minute,
	16 * time.Minute,
}

var retryDelayRE = regexp.MustCompile(`retry in (\d+(?:\.\d+)?)s`)

// Segment e` un blocco temporizzato di audio trascritto. Permette al client
// di fare seek puntuale (mensa://podcast/<id>?t=<start_seconds>).
type Segment struct {
	StartSeconds float64 `json:"start_seconds"`
	EndSeconds   float64 `json:"end_seconds"`
	Text         string  `json:"text"`
}

// Result e` quello che ritorna Transcribe.
type Result struct {
	Suitable        bool      `json:"suitable"`
	Reason          string    `json:"reason"`
	Language        string    `json:"language"`
	Transcript      string    `json:"transcript"`
	Segments        []Segment `json:"segments"`
	DurationSeconds int       `json:"-"`
}

// chunkResult e` il sottoinsieme di risposta che serve a noi: testo +
// word-level timestamps. Word.start/end sono relativi all'inizio del chunk;
// li offsettiamo al merge.
type chunkResult struct {
	index    int
	offset   float64 // secondi dall'inizio dell'episodio
	text     string
	words    []wordTiming
	language string
	err      error
}

type wordTiming struct {
	Word  string
	Start float64
	End   float64
}

// Transcribe carica l'audio, lo splitta in chunk con ffmpeg, chiama STT v2
// (modello chirp_2) per ogni chunk in parallelo e ricostruisce transcript
// + segments con timestamp assoluti.
func Transcribe(ctx context.Context, audioPath, _mimeType string) (*Result, error) {
	c, err := getClient(ctx)
	if err != nil {
		return nil, err
	}
	project := env.GetGoogleSTTProject()
	if project == "" {
		return nil, fmt.Errorf("GOOGLE_STT_PROJECT non impostato")
	}

	ctx, cancel := context.WithTimeout(ctx, totalTimeout)
	defer cancel()

	// 1. Chunk dell'audio con ffmpeg (formato linear16 mono 16kHz —
	// chirp_2 lo accetta nativamente e mantiene file piccoli).
	chunkDir, chunks, err := splitToChunks(ctx, audioPath, chunkSeconds)
	if err != nil {
		return nil, fmt.Errorf("ffmpeg chunk: %w", err)
	}
	defer func() { _ = os.RemoveAll(chunkDir) }()

	if len(chunks) == 0 {
		return &Result{Suitable: false, Reason: "audio vuoto o non leggibile"}, nil
	}

	// 2. Parallelizza Recognize sui chunk con semaforo configurabile.
	sem := make(chan struct{}, env.GetGoogleSTTConcurrency())
	results := make([]chunkResult, len(chunks))
	var wg sync.WaitGroup
	for i, path := range chunks {
		i, path := i, path
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			res := recognizeChunkWithRetry(ctx, c, project, path)
			res.index = i
			res.offset = float64(i) * chunkSeconds
			results[i] = res
		}()
	}
	wg.Wait()

	// 3. Merge: ordina per index, costruisci transcript + segments.
	sort.Slice(results, func(i, j int) bool { return results[i].index < results[j].index })

	var (
		fullTranscript strings.Builder
		segments       []Segment
		anyOK          bool
		firstLang      string
	)
	for _, r := range results {
		if r.err != nil {
			log.Printf("[podcasttranscribe] chunk %d errore: %v", r.index, r.err)
			continue
		}
		anyOK = true
		if firstLang == "" {
			firstLang = r.language
		}
		if r.text == "" {
			continue
		}
		if fullTranscript.Len() > 0 {
			fullTranscript.WriteByte(' ')
		}
		fullTranscript.WriteString(r.text)

		// Raggruppa words in segments di ~10s (o un segment per chunk se
		// pochi words). I word timestamp sono relativi al chunk → +offset.
		segs := groupWordsIntoSegments(r.words, r.offset)
		if len(segs) == 0 && r.text != "" {
			// Niente word timing (raro) → un solo segment per il chunk.
			segs = []Segment{{
				StartSeconds: r.offset,
				EndSeconds:   r.offset + chunkSeconds,
				Text:         r.text,
			}}
		}
		segments = append(segments, segs...)
	}

	if !anyOK {
		return nil, fmt.Errorf("tutti i chunk hanno fallito")
	}

	if fullTranscript.Len() < 100 {
		return &Result{
			Suitable: false,
			Reason:   "transcript troppo breve (< 100 char), probabile audio non parlato",
			Language: firstLang,
		}, nil
	}

	dur := 0
	if n := len(segments); n > 0 {
		dur = int(segments[n-1].EndSeconds)
	}
	return &Result{
		Suitable:        true,
		Language:        firstLang,
		Transcript:      fullTranscript.String(),
		Segments:        segments,
		DurationSeconds: dur,
	}, nil
}

// splitToChunks usa ffmpeg per dividere l'audio in WAV PCM 16kHz mono di
// durata `seconds`. Ritorna la directory tmp + la lista di path ordinati.
func splitToChunks(ctx context.Context, srcPath string, seconds int) (string, []string, error) {
	dir, err := os.MkdirTemp("", "stt_chunks_*")
	if err != nil {
		return "", nil, err
	}
	outPattern := filepath.Join(dir, "chunk_%03d.wav")
	cmd := exec.CommandContext(ctx, "ffmpeg",
		"-y", "-hide_banner", "-loglevel", "error",
		"-i", srcPath,
		"-ar", "16000", "-ac", "1", // mono 16kHz PCM
		"-f", "segment",
		"-segment_time", strconv.Itoa(seconds),
		"-c:a", "pcm_s16le",
		outPattern,
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		_ = os.RemoveAll(dir)
		return "", nil, fmt.Errorf("ffmpeg: %w (stderr=%s)", err, stderr.String())
	}
	matches, err := filepath.Glob(filepath.Join(dir, "chunk_*.wav"))
	if err != nil {
		_ = os.RemoveAll(dir)
		return "", nil, err
	}
	sort.Strings(matches)
	return dir, matches, nil
}

// recognizeChunkWithRetry chiama STT v2 sync su un chunk con retry su errori
// transient (429/5xx/timeout) usando backoff esponenziale.
func recognizeChunkWithRetry(ctx context.Context, c *speech.Client, project, path string) chunkResult {
	audio, err := os.ReadFile(path)
	if err != nil {
		return chunkResult{err: fmt.Errorf("read chunk: %w", err)}
	}
	var lastErr error
	for attempt := 0; attempt <= len(retryBackoffs); attempt++ {
		res, err := recognizeOnce(ctx, c, project, audio)
		if err == nil {
			return res
		}
		lastErr = err
		if !isTransient(err) {
			return chunkResult{err: err}
		}
		if attempt == len(retryBackoffs) {
			break
		}
		delay := retryBackoffs[attempt]
		if hinted, ok := extractRetryDelay(err); ok && hinted > 0 && hinted < delay {
			delay = hinted
		}
		log.Printf("[podcasttranscribe] chunk transient, retry in %s (%d/%d): %v",
			delay, attempt+1, len(retryBackoffs), err)
		time.Sleep(delay)
	}
	return chunkResult{err: fmt.Errorf("%d retry esauriti: %w", len(retryBackoffs), lastErr)}
}

// recognizeOnce: una singola chiamata sync Recognize per un chunk.
func recognizeOnce(ctx context.Context, c *speech.Client, project string, audio []byte) (chunkResult, error) {
	callCtx, cancel := context.WithTimeout(ctx, chunkTimeout)
	defer cancel()
	location := env.GetGoogleSTTLocation()
	recognizer := fmt.Sprintf("projects/%s/locations/%s/recognizers/_", project, location)
	req := &speechpb.RecognizeRequest{
		Recognizer: recognizer,
		Config: &speechpb.RecognitionConfig{
			DecodingConfig: &speechpb.RecognitionConfig_AutoDecodingConfig{
				AutoDecodingConfig: &speechpb.AutoDetectDecodingConfig{},
			},
			Model:         env.GetGoogleSTTModel(),
			LanguageCodes: []string{env.GetGoogleSTTLanguage()},
			Features: &speechpb.RecognitionFeatures{
				EnableAutomaticPunctuation: true,
				EnableWordTimeOffsets:      true,
				EnableWordConfidence:       false,
			},
		},
		AudioSource: &speechpb.RecognizeRequest_Content{Content: audio},
	}
	resp, err := c.Recognize(callCtx, req)
	if err != nil {
		return chunkResult{}, err
	}
	var (
		textParts []string
		words     []wordTiming
		lang      string
	)
	for _, r := range resp.GetResults() {
		alt := bestAlternative(r.GetAlternatives())
		if alt == nil {
			continue
		}
		textParts = append(textParts, alt.GetTranscript())
		if lang == "" {
			lang = r.GetLanguageCode()
		}
		for _, w := range alt.GetWords() {
			words = append(words, wordTiming{
				Word:  w.GetWord(),
				Start: w.GetStartOffset().AsDuration().Seconds(),
				End:   w.GetEndOffset().AsDuration().Seconds(),
			})
		}
	}
	return chunkResult{
		text:     strings.TrimSpace(strings.Join(textParts, " ")),
		words:    words,
		language: lang,
	}, nil
}

func bestAlternative(alts []*speechpb.SpeechRecognitionAlternative) *speechpb.SpeechRecognitionAlternative {
	if len(alts) == 0 {
		return nil
	}
	return alts[0] // STT ritorna ordinati per confidence decrescente
}

// groupWordsIntoSegments aggrega le parole in segments naturali ~10-30s
// usando i gap di silenzio tra parole consecutive come delimitatori.
// chunkOffset viene sommato a ogni timestamp.
func groupWordsIntoSegments(words []wordTiming, chunkOffset float64) []Segment {
	if len(words) == 0 {
		return nil
	}
	const (
		minSegmentDuration = 8.0  // s
		maxSegmentDuration = 30.0 // s
		breakOnSilenceGap  = 1.0  // s di silenzio tra parole = break
	)
	var segs []Segment
	currentStart := words[0].Start + chunkOffset
	currentEnd := words[0].End + chunkOffset
	var currentWords []string
	currentWords = append(currentWords, words[0].Word)

	flush := func() {
		if len(currentWords) == 0 {
			return
		}
		segs = append(segs, Segment{
			StartSeconds: currentStart,
			EndSeconds:   currentEnd,
			Text:         strings.Join(currentWords, " "),
		})
		currentWords = currentWords[:0]
	}

	for i := 1; i < len(words); i++ {
		w := words[i]
		gap := w.Start - words[i-1].End
		dur := w.End + chunkOffset - currentStart
		// break: gap di silenzio significativo + segment minimo raggiunto,
		// oppure durata max superata.
		if (gap >= breakOnSilenceGap && dur >= minSegmentDuration) || dur >= maxSegmentDuration {
			flush()
			currentStart = w.Start + chunkOffset
		}
		currentEnd = w.End + chunkOffset
		currentWords = append(currentWords, w.Word)
	}
	flush()
	return segs
}

func isTransient(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "RESOURCE_EXHAUSTED") ||
		strings.Contains(msg, "ResourceExhausted") ||
		strings.Contains(msg, "code = ResourceExhausted") ||
		strings.Contains(msg, "DEADLINE_EXCEEDED") ||
		strings.Contains(msg, "DeadlineExceeded") ||
		strings.Contains(msg, "Unavailable") ||
		strings.Contains(msg, "UNAVAILABLE") ||
		strings.Contains(msg, "Internal") ||
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

