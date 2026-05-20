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

	"demo.datum.net/internal/datumapi"
	"demo.datum.net/internal/origin"
	"demo.datum.net/internal/prober"
	"demo.datum.net/web"
)

const usage = `demo — split-path prober and sample origin for demo.datum.net

Usage:
  demo prober [flags]      run the probing service + web UI
  demo origin [flags]      run the fixed-JSON origin
  demo datum-smoke [flags] verify Datum control-plane auth/connectivity
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
	case "datum-smoke":
		err = runDatumSmoke(ctx, args)
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
	datumKubeconfig := fs.String("datum-kubeconfig", "", "Path to a Datum kubeconfig. If empty, basic-auth controls are hidden in the UI.")
	tunnelNamespace := fs.String("datum-tunnel-namespace", "default", "Namespace of the Datum tunnel HTTPRoute")
	tunnelName := fs.String("datum-tunnel-name", "", "Name of the Datum tunnel HTTPRoute the SecurityPolicy should attach to (required with --datum-kubeconfig)")
	_ = fs.Parse(args)

	if *directURL == "" || *datumURL == "" {
		fs.Usage()
		return fmt.Errorf("both --target-direct and --target-datum are required")
	}

	var datumClient *datumapi.Client
	if *datumKubeconfig != "" {
		if *tunnelName == "" {
			return fmt.Errorf("--datum-tunnel-name is required when --datum-kubeconfig is set")
		}
		c, err := datumapi.New(*datumKubeconfig)
		if err != nil {
			return fmt.Errorf("datum kubeconfig: %w", err)
		}
		datumClient = c
		log.Printf("prober: datum control plane enabled (tunnel=%s/%s)", *tunnelNamespace, *tunnelName)
	} else {
		log.Printf("prober: --datum-kubeconfig not set; basic-auth controls disabled")
	}

	store, err := prober.OpenStore(*dbPath)
	if err != nil {
		return err
	}
	defer store.Close()

	auth := prober.NewBasicAuthState()

	p := prober.NewProber(prober.ProberConfig{
		Store:      store,
		DirectURL:  *directURL,
		DatumURL:   *datumURL,
		Interval:   *interval,
		Timeout:    *timeout,
		Retention:  *retention,
		PruneEvery: *pruneEvery,
		Debug:      *debug,
		BasicAuth:  auth,
	})

	srv := prober.NewServer(prober.APIConfig{
		Listen:          *listen,
		Store:           store,
		DirectURL:       *directURL,
		DatumURL:        *datumURL,
		Web:             web.FS(),
		Debug:           *debug,
		Datum:           datumClient,
		TunnelNamespace: *tunnelNamespace,
		TunnelName:      *tunnelName,
		BasicAuth:       auth,
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

func runDatumSmoke(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("datum-smoke", flag.ExitOnError)
	kubeconfig := fs.String("kubeconfig", "", "Path to the Datum kubeconfig (from `datumctl auth update-kubeconfig`)")
	namespace := fs.String("namespace", "default", "Namespace to probe")
	_ = fs.Parse(args)

	if *kubeconfig == "" {
		fs.Usage()
		return fmt.Errorf("--kubeconfig is required")
	}

	client, err := datumapi.New(*kubeconfig)
	if err != nil {
		return err
	}
	n, err := client.Smoke(ctx, *namespace)
	if err != nil {
		return err
	}
	fmt.Printf("ok: listed SecurityPolicies in namespace %q (got %d item(s))\n", *namespace, n)
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
