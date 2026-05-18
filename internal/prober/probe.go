package prober

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptrace"
	"sync"
	"time"
)

const (
	pathDirect = "direct"
	pathDatum  = "datum"
)

type Prober struct {
	store      *Store
	client     *http.Client
	directURL  string
	datumURL   string
	interval   time.Duration
	timeout    time.Duration
	retention  time.Duration
	pruneEvery time.Duration
	userAgent  string
	debug      bool
}

type ProberConfig struct {
	Store      *Store
	DirectURL  string
	DatumURL   string
	Interval   time.Duration
	Timeout    time.Duration
	Retention  time.Duration
	PruneEvery time.Duration
	UserAgent  string
	Debug      bool
}

func NewProber(cfg ProberConfig) *Prober {
	if cfg.Interval <= 0 {
		cfg.Interval = 5 * time.Second
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 10 * time.Second
	}
	if cfg.Retention <= 0 {
		cfg.Retention = 7 * 24 * time.Hour
	}
	if cfg.PruneEvery <= 0 {
		cfg.PruneEvery = 1 * time.Hour
	}
	if cfg.UserAgent == "" {
		cfg.UserAgent = "demo-datum-prober/0.1"
	}
	return &Prober{
		store:      cfg.Store,
		directURL:  cfg.DirectURL,
		datumURL:   cfg.DatumURL,
		interval:   cfg.Interval,
		timeout:    cfg.Timeout,
		retention:  cfg.Retention,
		pruneEvery: cfg.PruneEvery,
		userAgent:  cfg.UserAgent,
		debug:      cfg.Debug,
		client: &http.Client{
			Timeout: cfg.Timeout,
			Transport: &http.Transport{
				// Fresh connection per probe so we measure the real path, not
				// a warm keep-alive.
				DisableKeepAlives: true,
				TLSClientConfig:   &tls.Config{MinVersion: tls.VersionTLS12},
				ForceAttemptHTTP2: true,
			},
		},
	}
}

func (p *Prober) Run(ctx context.Context) {
	probeTick := time.NewTicker(p.interval)
	defer probeTick.Stop()
	pruneTick := time.NewTicker(p.pruneEvery)
	defer pruneTick.Stop()

	// Fire immediately so the UI has something on first paint.
	p.probeBoth(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-probeTick.C:
			p.probeBoth(ctx)
		case <-pruneTick.C:
			cutoff := time.Now().Add(-p.retention).UnixMilli()
			if n, err := p.store.Prune(cutoff); err != nil {
				log.Printf("prober: prune error: %v", err)
			} else if n > 0 {
				log.Printf("prober: pruned %d rows older than %s", n, p.retention)
			}
		}
	}
}

func (p *Prober) probeBoth(ctx context.Context) {
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		p.probeOne(ctx, pathDirect, p.directURL)
	}()
	go func() {
		defer wg.Done()
		p.probeOne(ctx, pathDatum, p.datumURL)
	}()
	wg.Wait()
}

func (p *Prober) probeOne(ctx context.Context, path, url string) {
	sample := Sample{
		Path: path,
		TsMs: time.Now().UnixMilli(),
	}
	if url == "" {
		sample.Error = "no target url configured"
		_ = p.store.Insert(sample)
		return
	}

	probeCtx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	start := time.Now()
	var ttfb time.Duration
	trace := &httptrace.ClientTrace{
		GotFirstResponseByte: func() { ttfb = time.Since(start) },
	}

	req, err := http.NewRequestWithContext(
		httptrace.WithClientTrace(probeCtx, trace),
		http.MethodGet, url, nil,
	)
	if err != nil {
		sample.Error = err.Error()
		_ = p.store.Insert(sample)
		if p.debug {
			log.Printf("prober: >> %-6s GET %s build-error=%v", path, url, err)
		}
		return
	}
	req.Header.Set("User-Agent", p.userAgent)
	req.Header.Set("Accept", "application/json,*/*")

	if p.debug {
		log.Printf("prober: >> %-6s GET %s ua=%q", path, url, p.userAgent)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		sample.Error = err.Error()
		sample.TotalMs = msSince(start)
		_ = p.store.Insert(sample)
		if p.debug {
			log.Printf("prober: << %-6s GET %s ERROR err=%v dur=%.2fms",
				path, url, err, sample.TotalMs)
		}
		return
	}
	defer resp.Body.Close()

	n, copyErr := io.Copy(io.Discard, resp.Body)
	total := time.Since(start)

	sample.StatusCode = resp.StatusCode
	sample.Ok = resp.StatusCode >= 200 && resp.StatusCode < 300 && copyErr == nil
	sample.TtfbMs = float64(ttfb.Microseconds()) / 1000.0
	sample.TotalMs = float64(total.Microseconds()) / 1000.0
	sample.Bytes = n
	if copyErr != nil {
		sample.Error = copyErr.Error()
	}

	if p.debug {
		ct := resp.Header.Get("Content-Type")
		errStr := ""
		if copyErr != nil {
			errStr = fmt.Sprintf(" body-err=%v", copyErr)
		}
		log.Printf("prober: << %-6s GET %s status=%d ttfb=%.2fms total=%.2fms bytes=%d type=%q%s",
			path, url, resp.StatusCode,
			sample.TtfbMs, sample.TotalMs, n, ct, errStr,
		)
	}

	if err := p.store.Insert(sample); err != nil {
		log.Printf("prober: insert error: %v", err)
	}
}

func msSince(t time.Time) float64 {
	return float64(time.Since(t).Microseconds()) / 1000.0
}
