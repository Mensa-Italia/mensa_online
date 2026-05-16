package whisper

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	// Timeout per la trascrizione di un singolo episodio. Whisper medium su
	// 8 thread CPU x86 fa ~0.5x RT, quindi 1h podcast = ~30 min wallclock.
	// 60 min copre comodamente fino a 2h di audio. Per podcast > 2h
	// considera di splittare.
	transcribeTimeout = 60 * time.Minute
)

// Segment e` un blocco temporizzato di trascrizione. Stessa shape di
// podcastsync.VTTSegment cosi` saveTranscriptFromVTT/saveTranscriptFromWhisper
// possono usare la stessa logica di salvataggio.
type Segment struct {
	StartSeconds float64 `json:"start_seconds"`
	EndSeconds   float64 `json:"end_seconds"`
	Text         string  `json:"text"`
}

// Result e` quello che ritorna Transcribe.
type Result struct {
	Transcript      string    `json:"transcript"`
	Segments        []Segment `json:"segments"`
	Language        string    `json:"language"`
	DurationSeconds int       `json:"duration_seconds"`
}

// whisperJSONOutput riflette il formato JSON di whisper-cli -oj.
type whisperJSONOutput struct {
	Result struct {
		Language string `json:"language"`
	} `json:"result"`
	Transcription []struct {
		Timestamps struct {
			From string `json:"from"`
			To   string `json:"to"`
		} `json:"timestamps"`
		Offsets struct {
			From int `json:"from"`
			To   int `json:"to"`
		} `json:"offsets"`
		Text string `json:"text"`
	} `json:"transcription"`
}

// Transcribe esegue whisper.cpp sul file audio passato e ritorna transcript
// + segments con timestamp assoluti. Il modello viene scaricato lazy al
// primo uso (~700MB nel volume pb_data).
//
// L'audio viene prima convertito in WAV 16kHz mono (formato che whisper
// digerisce nativamente) via ffmpeg.
func Transcribe(ctx context.Context, audioPath string) (*Result, error) {
	model, err := EnsureModel(ctx)
	if err != nil {
		return nil, fmt.Errorf("ensure model: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, transcribeTimeout)
	defer cancel()

	// 1. Converti l'audio in WAV 16kHz mono PCM (formato preferito whisper).
	tmpDir, err := os.MkdirTemp("", "whisper_*")
	if err != nil {
		return nil, fmt.Errorf("mktemp: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()
	wavPath := filepath.Join(tmpDir, "input.wav")
	ffmpegCmd := exec.CommandContext(ctx, "ffmpeg",
		"-y", "-hide_banner", "-loglevel", "error",
		"-i", audioPath,
		"-ar", "16000", "-ac", "1",
		"-c:a", "pcm_s16le",
		wavPath,
	)
	var ffStderr bytes.Buffer
	ffmpegCmd.Stderr = &ffStderr
	if err := ffmpegCmd.Run(); err != nil {
		return nil, fmt.Errorf("ffmpeg→wav: %w (stderr=%s)", err, ffStderr.String())
	}

	// 2. Esegui whisper-cli. -oj scrive <input>.json accanto al file.
	// Threads: lascia al binario decidere il default (= num CPU/2) ma
	// cappiamo a 8 per evitare di saturare un host shared.
	threads := runtime.NumCPU() / 2
	if threads < 2 {
		threads = 2
	}
	if threads > 8 {
		threads = 8
	}
	whisperCmd := exec.CommandContext(ctx, "whisper-cli",
		"-m", model,
		"-l", "it",
		"-oj",                  // JSON output
		"-of", wavPath,         // output file prefix (genera wavPath.json)
		"-t", strconv.Itoa(threads),
		"-nt", // niente timestamp nel testo (li abbiamo già nei segments)
		wavPath,
	)
	var whStderr bytes.Buffer
	whisperCmd.Stderr = &whStderr
	if err := whisperCmd.Run(); err != nil {
		return nil, fmt.Errorf("whisper-cli: %w (stderr=%s)", err, whStderr.String())
	}

	// 3. Parse il JSON.
	jsonBytes, err := os.ReadFile(wavPath + ".json")
	if err != nil {
		return nil, fmt.Errorf("read whisper json: %w", err)
	}
	var raw whisperJSONOutput
	if err := json.Unmarshal(jsonBytes, &raw); err != nil {
		return nil, fmt.Errorf("decode whisper json: %w", err)
	}

	res := &Result{Language: raw.Result.Language}
	var transcript strings.Builder
	for _, t := range raw.Transcription {
		text := strings.TrimSpace(t.Text)
		if text == "" {
			continue
		}
		res.Segments = append(res.Segments, Segment{
			StartSeconds: float64(t.Offsets.From) / 1000.0,
			EndSeconds:   float64(t.Offsets.To) / 1000.0,
			Text:         text,
		})
		if transcript.Len() > 0 {
			transcript.WriteByte(' ')
		}
		transcript.WriteString(text)
	}
	res.Transcript = transcript.String()
	if n := len(res.Segments); n > 0 {
		res.DurationSeconds = int(res.Segments[n-1].EndSeconds)
	}
	return res, nil
}
