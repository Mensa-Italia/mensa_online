package whisper

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Modello: medium quantizzato (Q5_K), ~700MB, ottimo rapporto qualita`/CPU
// su italiano parlato. Per podcast Mensa va piu` che bene; per nomi rari
// large-v3 sarebbe leggermente meglio ma pesa 3x.
const (
	modelName       = "ggml-medium-q5_0.bin"
	modelURL        = "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/" + modelName
	modelDir        = "/pb/main/pb_data/whisper-models"
	downloadTimeout = 30 * time.Minute
)

// modelOnce + modelPath cachano la posizione del modello scaricato. Il
// download avviene solo al primo uso (lazy).
var (
	modelOnce sync.Once
	modelPath string
	modelErr  error
)

// EnsureModel scarica (se non gia` presente) il modello whisper nel volume
// persistente pb_data/whisper-models. Idempotente. Concorrenza-safe via
// sync.Once.
func EnsureModel(ctx context.Context) (string, error) {
	modelOnce.Do(func() {
		if err := os.MkdirAll(modelDir, 0o755); err != nil {
			modelErr = fmt.Errorf("mkdir model dir: %w", err)
			return
		}
		dest := filepath.Join(modelDir, modelName)
		if st, err := os.Stat(dest); err == nil && st.Size() > 100<<20 {
			// File esiste e ha dimensione plausibile (>100MB): considerato ok.
			modelPath = dest
			return
		}
		// Download verso file temporaneo, poi rename atomico per evitare
		// che un crash a meta` download lasci un modello corrotto.
		tmp := dest + ".part"
		_ = os.Remove(tmp)

		dlCtx, cancel := context.WithTimeout(ctx, downloadTimeout)
		defer cancel()
		req, err := http.NewRequestWithContext(dlCtx, http.MethodGet, modelURL, nil)
		if err != nil {
			modelErr = err
			return
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			modelErr = fmt.Errorf("download model: %w", err)
			return
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			modelErr = fmt.Errorf("download model: status %d", resp.StatusCode)
			return
		}
		f, err := os.Create(tmp)
		if err != nil {
			modelErr = fmt.Errorf("create tmp model: %w", err)
			return
		}
		if _, err := io.Copy(f, resp.Body); err != nil {
			_ = f.Close()
			_ = os.Remove(tmp)
			modelErr = fmt.Errorf("write model: %w", err)
			return
		}
		_ = f.Close()
		if err := os.Rename(tmp, dest); err != nil {
			modelErr = fmt.Errorf("rename model: %w", err)
			return
		}
		modelPath = dest
	})
	if modelErr != nil {
		return "", modelErr
	}
	if modelPath == "" {
		return "", errors.New("model path empty after init")
	}
	return modelPath, nil
}
