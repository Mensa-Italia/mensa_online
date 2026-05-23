package podcastsync

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	trimTimeout = 10 * time.Minute

	// silenceThreshold = soglia in dB sotto cui consideriamo "silenzio".
	silenceThreshold = "-45dB"

	// silenceDuration = quanti secondi di silenzio servono per far
	// scattare il taglio.
	silenceDuration = "0.3"
)

// TrimResult riporta sia la nuova durata sia di quanti secondi e` stato
// shiftato l'inizio dell'audio. StartOffsetSeconds serve a riallineare i
// VTT scaricati da YouTube (i cui timestamp si riferiscono all'audio
// originale, non a quello trimmato).
type TrimResult struct {
	NewDurationSeconds int
	StartOffsetSeconds float64
}

// silenceEventRE matcha le linee emesse da ffmpeg -af silencedetect.
var silenceEventRE = regexp.MustCompile(`silence_(start|end):\s*(-?\d+(?:\.\d+)?)`)

// TrimSilence taglia il silenzio iniziale e finale dell'mp3. A differenza
// di silenceremove "blind" del passato, prima detecta i boundaries esatti
// con silencedetect, poi taglia con -ss/-to: cosi` sappiamo precisamente
// di quanti secondi abbiamo shiftato l'inizio e possiamo riallineare i
// timestamp dei sottotitoli.
//
// Se ffmpeg fallisce, il file resta intatto e l'errore propaga: meglio
// audio originale con silenzio che episodio mancante.
func TrimSilence(mp3Path string) (*TrimResult, error) {
	if _, err := os.Stat(mp3Path); err != nil {
		return nil, fmt.Errorf("file non trovato: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), trimTimeout)
	defer cancel()

	totalDur := probeDurationFloat(mp3Path)
	if totalDur <= 0 {
		return nil, fmt.Errorf("durata audio non rilevabile")
	}

	startTrim, endStart := detectSilenceBounds(ctx, mp3Path, totalDur)
	// Se il silenzio iniziale e` < 0.3s o quello finale e` cosi` vicino
	// alla fine da non valere, lasciamo invariato.
	if startTrim < 0.3 && endStart >= totalDur-0.3 {
		return &TrimResult{
			NewDurationSeconds: int(totalDur),
			StartOffsetSeconds: 0,
		}, nil
	}
	if startTrim < 0 {
		startTrim = 0
	}
	if endStart <= 0 || endStart > totalDur {
		endStart = totalDur
	}
	if endStart <= startTrim+1 {
		// Trim improbabile / audio troppo corto. Skip senza errore.
		return &TrimResult{
			NewDurationSeconds: int(totalDur),
			StartOffsetSeconds: 0,
		}, nil
	}

	tmpOut := mp3Path + ".trim.mp3"
	cmd := exec.CommandContext(ctx, "ffmpeg",
		"-y", "-hide_banner", "-loglevel", "error",
		"-ss", fmt.Sprintf("%.3f", startTrim),
		"-to", fmt.Sprintf("%.3f", endStart),
		"-i", mp3Path,
		"-codec:a", "libmp3lame", "-q:a", "2",
		tmpOut,
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		_ = os.Remove(tmpOut)
		return nil, fmt.Errorf("ffmpeg trim: %w (stderr=%s)", err, stderr.String())
	}
	if err := os.Rename(tmpOut, mp3Path); err != nil {
		_ = os.Remove(tmpOut)
		return nil, fmt.Errorf("rename trimmed: %w", err)
	}

	return &TrimResult{
		NewDurationSeconds: probeDurationSeconds(mp3Path),
		StartOffsetSeconds: startTrim,
	}, nil
}

// detectSilenceBounds esegue una passata di ffmpeg silencedetect e parsa
// stderr. Ritorna:
//   - startTrim: quanti secondi di silenzio all'inizio (0 se assente)
//   - endStart: timestamp da cui inizia il silenzio finale (= totalDur se assente)
func detectSilenceBounds(ctx context.Context, mp3Path string, totalDur float64) (startTrim, endStart float64) {
	endStart = totalDur

	cmd := exec.CommandContext(ctx, "ffmpeg",
		"-hide_banner", "-nostats",
		"-i", mp3Path,
		"-af", fmt.Sprintf("silencedetect=noise=%s:d=%s", silenceThreshold, silenceDuration),
		"-f", "null", "-",
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	_ = cmd.Run() // exit code di solito 0 anche su match; ignoriamo

	// Parsa eventi: cerca il primo silence_end (se start=0 → silenzio
	// iniziale) e l'ultimo silence_start (se prossimo alla fine).
	var (
		gotInitialStart bool
		gotInitialEnd   bool
		lastSilStart    float64 = -1
	)
	for _, line := range strings.Split(stderr.String(), "\n") {
		matches := silenceEventRE.FindAllStringSubmatch(line, -1)
		for _, m := range matches {
			val, err := strconv.ParseFloat(m[2], 64)
			if err != nil {
				continue
			}
			switch m[1] {
			case "start":
				// Silenzio iniziale solo se il primo evento e` start a ~0.
				if !gotInitialStart && val < 0.5 {
					gotInitialStart = true
				}
				lastSilStart = val
			case "end":
				if gotInitialStart && !gotInitialEnd {
					startTrim = val
					gotInitialEnd = true
				}
			}
		}
	}
	// Silenzio finale: se l'ultimo silence_start cade entro 30s dalla fine
	// dell'audio, lo trattiamo come silenzio terminale e usiamo quel
	// timestamp come nuovo end del file trimmato.
	if lastSilStart > startTrim && lastSilStart < totalDur && totalDur-lastSilStart < 30 {
		endStart = lastSilStart
	}
	return startTrim, endStart
}

func probeDurationFloat(path string) float64 {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ffprobe",
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1",
		path,
	)
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return 0
	}
	s := strings.TrimSpace(out.String())
	if s == "" {
		return 0
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return f
}

func probeDurationSeconds(path string) int {
	return int(probeDurationFloat(path))
}
