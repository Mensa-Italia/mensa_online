package httpx

import (
	"context"
	"errors"
	"net"
	"net/http"
	"sync"
	"time"
)

var (
	defaultOnce   sync.Once
	defaultClient *http.Client
)

// Default returns a shared http.Client with sensible production timeouts.
// Reuse the same client to share connection pool.
func Default() *http.Client {
	defaultOnce.Do(func() {
		defaultClient = &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     90 * time.Second,
				TLSHandshakeTimeout: 10 * time.Second,
			},
		}
	})
	return defaultClient
}

// New returns a client with custom timeout but the same shared transport.
func New(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout:   timeout,
		Transport: Default().Transport,
	}
}

// DoWithRetry esegue req con retry esponenziale su errori di rete o status >=500.
// MAI ritorna error se la richiesta finisce in modo "applicativo" (4xx) — quello è il chiamante.
func DoWithRetry(ctx context.Context, c *http.Client, req *http.Request, maxAttempts int) (*http.Response, error) {
	if maxAttempts < 1 {
		maxAttempts = 1
	}
	var lastErr error
	backoff := 200 * time.Millisecond
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		resp, err := c.Do(req.Clone(ctx))
		if err == nil && resp.StatusCode < 500 {
			return resp, nil
		}
		if resp != nil {
			resp.Body.Close()
		}
		if err != nil {
			var nerr net.Error
			if !errors.As(err, &nerr) && !isRetryable(err) {
				return nil, err
			}
			lastErr = err
		} else {
			lastErr = errors.New(resp.Status)
		}
		if attempt < maxAttempts {
			t := time.NewTimer(backoff)
			select {
			case <-ctx.Done():
				t.Stop()
				return nil, ctx.Err()
			case <-t.C:
			}
			backoff *= 2
			if backoff > 5*time.Second {
				backoff = 5 * time.Second
			}
		}
	}
	return nil, lastErr
}

func isRetryable(err error) bool {
	// network-level/timeout: vedi sopra. Questo è un fallback.
	return err != nil
}
