package origin

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"time"

	"demo.datum.net/internal/httpdebug"
)

type Config struct {
	Listen       string
	PayloadBytes int
	Debug        bool
}

type Payload struct {
	Service string `json:"service"`
	Version string `json:"version"`
	Now     string `json:"now"`
	Padding string `json:"padding"`
}

func Run(ctx context.Context, cfg Config) error {
	if cfg.PayloadBytes < 0 {
		cfg.PayloadBytes = 0
	}
	padding := makePadding(cfg.PayloadBytes)

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		_ = json.NewEncoder(w).Encode(Payload{
			Service: "demo-origin",
			Version: "1",
			Now:     time.Now().UTC().Format(time.RFC3339Nano),
			Padding: padding,
		})
	})

	var handler http.Handler = mux
	if cfg.Debug {
		handler = httpdebug.LogMiddleware("origin", mux)
	}

	srv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Printf("origin: listening on %s, payload=%d bytes", cfg.Listen, cfg.PayloadBytes)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
		return nil
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func makePadding(n int) string {
	if n == 0 {
		return ""
	}
	raw := make([]byte, (n*3)/4+1)
	_, _ = rand.Read(raw)
	s := base64.StdEncoding.EncodeToString(raw)
	if len(s) > n {
		s = s[:n]
	}
	return s
}
