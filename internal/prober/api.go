package prober

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"log"
	"math"
	"net/http"
	"sort"
	"time"

	"demo.datum.net/internal/httpdebug"
)

const summaryWindow = 5 * time.Minute

type apiServer struct {
	store     *Store
	directURL string
	datumURL  string
	web       fs.FS
	startedAt time.Time
}

type metricsResponse struct {
	Now             string      `json:"now"`
	ProberStartedAt string      `json:"prober_started_at"`
	SummaryWindowS  int         `json:"summary_window_s"`
	DatumPathDown   bool        `json:"datum_path_down"`
	Targets         targetsInfo `json:"targets"`
	Direct          pathSummary `json:"direct"`
	Datum           pathSummary `json:"datum"`
}

type targetsInfo struct {
	DirectURL string `json:"direct_url"`
	DatumURL  string `json:"datum_url"`
}

type pathSummary struct {
	CurrentMs   float64     `json:"current_ms"`
	P50Ms       float64     `json:"p50_ms"`
	P95Ms       float64     `json:"p95_ms"`
	P50TtfbMs   float64     `json:"p50_ttfb_ms"`
	ErrorPct    float64     `json:"error_pct"`
	RetransPct  float64     `json:"retrans_pct"`
	SampleCount int         `json:"sample_count"`
	OkCount     int         `json:"ok_count"`
	LastError   string      `json:"last_error,omitempty"`
	LastStatus  int         `json:"last_status_code,omitempty"`
	Samples     []apiSample `json:"samples"`
}

type apiSample struct {
	TsMs    int64   `json:"ts_ms"`
	Ok      bool    `json:"ok"`
	TotalMs float64 `json:"total_ms"`
}

type Server struct {
	cfg APIConfig
	srv *http.Server
}

type APIConfig struct {
	Listen    string
	Store     *Store
	DirectURL string
	DatumURL  string
	Web       fs.FS // static UI assets, served at /
	Debug     bool
}

func NewServer(cfg APIConfig) *Server {
	api := &apiServer{
		store:     cfg.Store,
		directURL: cfg.DirectURL,
		datumURL:  cfg.DatumURL,
		web:       cfg.Web,
		startedAt: time.Now(),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/metrics", api.handleMetrics)
	mux.HandleFunc("/api/health", api.handleHealth)
	if cfg.Web != nil {
		mux.Handle("/", http.FileServer(http.FS(cfg.Web)))
	}

	var handler http.Handler = mux
	if cfg.Debug {
		handler = httpdebug.LogMiddleware("prober-api", mux)
	}

	return &Server{
		cfg: cfg,
		srv: &http.Server{
			Addr:              cfg.Listen,
			Handler:           handler,
			ReadHeaderTimeout: 5 * time.Second,
		},
	}
}

func (s *Server) Run(ctx context.Context) error {
	errCh := make(chan error, 1)
	go func() {
		log.Printf("prober: api listening on %s", s.cfg.Listen)
		errCh <- s.srv.ListenAndServe()
	}()
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.srv.Shutdown(shutdownCtx)
		return nil
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func (a *apiServer) handleHealth(w http.ResponseWriter, _ *http.Request) {
	count, _ := a.store.Count()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":            true,
		"started_at":    a.startedAt.UTC().Format(time.RFC3339),
		"sample_count":  count,
	})
}

func (a *apiServer) handleMetrics(w http.ResponseWriter, _ *http.Request) {
	sinceMs := time.Now().Add(-summaryWindow).UnixMilli()

	direct, err := a.store.Recent(pathDirect, sinceMs)
	if err != nil {
		http.Error(w, "store read failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	datum, err := a.store.Recent(pathDatum, sinceMs)
	if err != nil {
		http.Error(w, "store read failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	resp := metricsResponse{
		Now:             time.Now().UTC().Format(time.RFC3339Nano),
		ProberStartedAt: a.startedAt.UTC().Format(time.RFC3339),
		SummaryWindowS:  int(summaryWindow.Seconds()),
		Targets: targetsInfo{
			DirectURL: a.directURL,
			DatumURL:  a.datumURL,
		},
		Direct:        summarize(direct),
		Datum:         summarize(datum),
		DatumPathDown: pathDown(datum),
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(resp)
}

func summarize(samples []Sample) pathSummary {
	out := pathSummary{
		Samples: make([]apiSample, 0, len(samples)),
	}
	if len(samples) == 0 {
		return out
	}

	okTotals := make([]float64, 0, len(samples))
	okTtfbs := make([]float64, 0, len(samples))
	var sumRetrans, sumSegs int64
	for _, s := range samples {
		out.Samples = append(out.Samples, apiSample{
			TsMs:    s.TsMs,
			Ok:      s.Ok,
			TotalMs: s.TotalMs,
		})
		out.SampleCount++
		if s.Ok {
			out.OkCount++
			okTotals = append(okTotals, s.TotalMs)
			if s.TtfbMs > 0 {
				okTtfbs = append(okTtfbs, s.TtfbMs)
			}
			if s.DataSegsOut > 0 {
				sumRetrans += s.TotalRetrans
				sumSegs += s.DataSegsOut
			}
		}
	}

	last := samples[len(samples)-1]
	out.LastStatus = last.StatusCode
	out.LastError = last.Error
	if last.Ok {
		out.CurrentMs = last.TotalMs
	}

	if len(okTotals) > 0 {
		out.P50Ms = percentile(okTotals, 50)
		out.P95Ms = percentile(okTotals, 95)
	}
	if len(okTtfbs) > 0 {
		out.P50TtfbMs = percentile(okTtfbs, 50)
	}
	if out.SampleCount > 0 {
		out.ErrorPct = math.Round((1.0-float64(out.OkCount)/float64(out.SampleCount))*10000) / 100
	}
	if sumSegs > 0 {
		out.RetransPct = math.Round(float64(sumRetrans)/float64(sumSegs)*10000) / 100
	}
	return out
}

// pathDown reports true if the last 3 samples were all failures.
func pathDown(samples []Sample) bool {
	const n = 3
	if len(samples) < n {
		return false
	}
	tail := samples[len(samples)-n:]
	for _, s := range tail {
		if s.Ok {
			return false
		}
	}
	return true
}

func percentile(vs []float64, p float64) float64 {
	if len(vs) == 0 {
		return 0
	}
	sorted := append([]float64(nil), vs...)
	sort.Float64s(sorted)
	idx := int(math.Ceil(p/100.0*float64(len(sorted)))) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}
