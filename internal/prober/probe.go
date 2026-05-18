package prober

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptrace"
	"sync"
	"sync/atomic"
	"time"
)

// tcpInfoSlotKey is the context key under which the active probe stashes
// an empty TCP_INFO slot. The transport's DialContext wraps the freshly
// dialed conn in an instrumentedConn that fills the slot from inside its
// Close() — the only place where the fd is guaranteed to still be open
// after the response body has been drained.
type tcpInfoSlotKey struct{}

type tcpInfoSnapshot struct {
	TotalRetrans uint32
	DataSegsOut  uint32
}

type tcpInfoSlot struct {
	snap atomic.Pointer[tcpInfoSnapshot]
}

type instrumentedConn struct {
	net.Conn
	tcp       *net.TCPConn
	slot      *tcpInfoSlot
	closeOnce sync.Once
}

func (c *instrumentedConn) Close() error {
	c.closeOnce.Do(func() {
		if c.tcp == nil || c.slot == nil {
			return
		}
		if rt, ds, ok := readRetrans(c.tcp); ok {
			c.slot.snap.Store(&tcpInfoSnapshot{TotalRetrans: rt, DataSegsOut: ds})
		}
	})
	return c.Conn.Close()
}

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
		client: newProbeClient(cfg.Timeout),
	}
}

func newProbeClient(timeout time.Duration) *http.Client {
	dialer := &net.Dialer{Timeout: timeout, KeepAlive: -1}
	t := &http.Transport{
		DisableKeepAlives: true,
		TLSClientConfig:   &tls.Config{MinVersion: tls.VersionTLS12},
		ForceAttemptHTTP2: true,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			c, err := dialer.DialContext(ctx, network, addr)
			if err != nil {
				return nil, err
			}
			tcp, _ := c.(*net.TCPConn)
			slot, _ := ctx.Value(tcpInfoSlotKey{}).(*tcpInfoSlot)
			if tcp == nil || slot == nil {
				return c, nil
			}
			return &instrumentedConn{Conn: c, tcp: tcp, slot: slot}, nil
		},
	}
	return &http.Client{Timeout: timeout, Transport: t}
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

	slot := new(tcpInfoSlot)
	probeCtx = context.WithValue(probeCtx, tcpInfoSlotKey{}, slot)

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

	_, copyErr := io.Copy(io.Discard, resp.Body)
	total := time.Since(start)
	// Close explicitly so our instrumentedConn.Close() runs and fills the
	// TCP_INFO slot before we read from it. The transport tears the fd
	// down inside this Close — there's no later chance to read TCP_INFO.
	_ = resp.Body.Close()

	sample.StatusCode = resp.StatusCode
	sample.Ok = resp.StatusCode >= 200 && resp.StatusCode < 300 && copyErr == nil
	sample.TtfbMs = float64(ttfb.Microseconds()) / 1000.0
	sample.TotalMs = float64(total.Microseconds()) / 1000.0
	if copyErr != nil {
		sample.Error = copyErr.Error()
	}

	if snap := slot.snap.Load(); snap != nil {
		sample.TotalRetrans = int64(snap.TotalRetrans)
		sample.DataSegsOut = int64(snap.DataSegsOut)
	}

	if p.debug {
		ct := resp.Header.Get("Content-Type")
		proto := resp.Proto
		errStr := ""
		if copyErr != nil {
			errStr = fmt.Sprintf(" body-err=%v", copyErr)
		}
		log.Printf("prober: << %-6s GET %s status=%d proto=%s ttfb=%.2fms total=%.2fms retrans=%d/%d type=%q%s",
			path, url, resp.StatusCode, proto,
			sample.TtfbMs, sample.TotalMs,
			sample.TotalRetrans, sample.DataSegsOut,
			ct, errStr,
		)
	}

	if err := p.store.Insert(sample); err != nil {
		log.Printf("prober: insert error: %v", err)
	}
}

func msSince(t time.Time) float64 {
	return float64(time.Since(t).Microseconds()) / 1000.0
}
