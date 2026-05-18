package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"demo.datum.net/internal/origin"
	"demo.datum.net/internal/prober"
	"demo.datum.net/web"
)

const usage = `demo — split-path prober and sample origin for demo.datum.net

Usage:
  demo prober [flags]    run the probing service + web UI
  demo origin [flags]    run the fixed-JSON origin
  demo version

Run a subcommand with -h to see its flags.
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var err error
	switch cmd {
	case "prober":
		err = runProber(ctx, args)
	case "origin":
		err = runOrigin(ctx, args)
	case "version":
		fmt.Println("demo 0.1.0")
	case "-h", "--help", "help":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", cmd, usage)
		os.Exit(2)
	}

	if err != nil {
		log.Fatalf("%s: %v", cmd, err)
	}
}

func runProber(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("prober", flag.ExitOnError)
	listen := fs.String("listen", ":8080", "HTTP listen address for the UI + API")
	directURL := fs.String("target-direct", "", "URL to probe via the public internet (required)")
	datumURL := fs.String("target-datum", "", "URL to probe via the Datum path (required)")
	dbPath := fs.String("db", "./prober.db", "Path to sqlite database file")
	interval := fs.Duration("interval", 5*time.Second, "Time between probe cycles")
	timeout := fs.Duration("timeout", 10*time.Second, "Per-request timeout")
	retention := fs.Duration("retention", 7*24*time.Hour, "How long to keep samples in the database")
	pruneEvery := fs.Duration("prune-every", time.Hour, "How often to prune old samples")
	debug := fs.Bool("debug", false, "Log every outgoing probe request/response and every incoming UI/API request")
	_ = fs.Parse(args)

	if *directURL == "" || *datumURL == "" {
		fs.Usage()
		return fmt.Errorf("both --target-direct and --target-datum are required")
	}

	store, err := prober.OpenStore(*dbPath)
	if err != nil {
		return err
	}
	defer store.Close()

	p := prober.NewProber(prober.ProberConfig{
		Store:      store,
		DirectURL:  *directURL,
		DatumURL:   *datumURL,
		Interval:   *interval,
		Timeout:    *timeout,
		Retention:  *retention,
		PruneEvery: *pruneEvery,
		Debug:      *debug,
	})

	srv := prober.NewServer(prober.APIConfig{
		Listen:    *listen,
		Store:     store,
		DirectURL: *directURL,
		DatumURL:  *datumURL,
		Web:       web.FS(),
		Debug:     *debug,
	})

	errCh := make(chan error, 2)
	go func() { p.Run(ctx); errCh <- nil }()
	go func() { errCh <- srv.Run(ctx) }()

	// First error (or first clean exit) ends the program.
	if err := <-errCh; err != nil {
		return err
	}
	<-errCh
	return nil
}

func runOrigin(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("origin", flag.ExitOnError)
	listen := fs.String("listen", ":8080", "HTTP listen address")
	payload := fs.Int("payload-bytes", 4096, "Approximate response body size in bytes")
	debug := fs.Bool("debug", false, "Log every incoming request (method, path, remote, status, bytes, duration)")
	_ = fs.Parse(args)

	return origin.Run(ctx, origin.Config{
		Listen:       *listen,
		PayloadBytes: *payload,
		Debug:        *debug,
	})
}
