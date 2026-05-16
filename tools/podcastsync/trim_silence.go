package podcastsync

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const (
	trimTimeout = 10 * time.Minute

	// silenceThreshold = soglia in dB sotto cui consideriamo "silenzio".
	// -45 dB e` un buon compromesso: taglia respiri/intro mute ma non
	// frasi sussurrate o jingle bassi.
	silenceThreshold = "-45dB"

	// silenceDuration = quanti secondi di silenzio servono per far
	// scattare il taglio. Troppo basso = taglia anche pause naturali;
	// troppo alto = lascia silenzio in testa/coda.
	silenceDuration = "0.3"
)

// TrimSilence riscrive l'mp3 indicato rimuovendo silenzio in testa e in
// coda. Usa ffmpeg col filtro silenceremove applicato due volte (forward
// per la testa, reverse → forward → reverse per la coda).
//
// Ritorna la nuova durata in secondi (via ffprobe). Se ffmpeg fallisce,
// il file originale resta intatto e l'errore viene propagato — meglio
// avere un episodio con silenzio che nessun episodio.
func TrimSilence(mp3Path string) (int, error) {
	if _, err := os.Stat(mp3Path); err != nil {
		return 0, fmt.Errorf("file non trovato: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), trimTimeout)
	defer cancel()

	tmpOut := mp3Path + ".trim.mp3"
	// silenceremove parametrizzato per testa + coda:
	//   1) silenceremove forward: taglia il silenzio iniziale
	//   2) areverse: inverte l'audio
	//   3) silenceremove forward: taglia il silenzio iniziale del reverse (= finale dell'originale)
	//   4) areverse: rimette in ordine
	filter := fmt.Sprintf(
		"silenceremove=start_periods=1:start_silence=%s:start_threshold=%s:detection=peak,"+
			"areverse,"+
			"silenceremove=start_periods=1:start_silence=%s:start_threshold=%s:detection=peak,"+
			"areverse",
		silenceDuration, silenceThreshold,
		silenceDuration, silenceThreshold,
	)

	cmd := exec.CommandContext(ctx, "ffmpeg",
		"-y", "-hide_banner", "-loglevel", "error",
		"-i", mp3Path,
		"-af", filter,
		"-codec:a", "libmp3lame", "-q:a", "2",
		tmpOut,
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		_ = os.Remove(tmpOut)
		return 0, fmt.Errorf("ffmpeg silenceremove: %w (stderr=%s)", err, stderr.String())
	}

	// Sostituisci atomicamente l'originale.
	if err := os.Rename(tmpOut, mp3Path); err != nil {
		_ = os.Remove(tmpOut)
		return 0, fmt.Errorf("rename trimmed file: %w", err)
	}

	return probeDurationSeconds(mp3Path), nil
}

// probeDurationSeconds usa ffprobe per leggere la durata in secondi del file.
// Ritorna 0 in caso di errore: i chiamanti che usano questo valore devono
// gestire il fallback (es. lasciare la duration originale).
func probeDurationSeconds(path string) int {
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
	return int(f)
}
