package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/spf13/cobra"

	"github.com/rustyeddy/trader/internal/logging"
	"github.com/rustyeddy/trader/internal/store"
	"github.com/rustyeddy/trader/rest"
	"github.com/rustyeddy/trader/service"
)

// envAPIToken is where the bearer token the API requires comes from (like the
// OANDA token, never a flag or a file).
const envAPIToken = "TRADER_API_TOKEN"

const defaultListen = "127.0.0.1:8080"

type serveFlags struct {
	config, dataRoot string
	listen           string
	timeout          time.Duration
}

func newServeCmd(a *app) *cobra.Command {
	var f serveFlags
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Serve the canonical market data over HTTP (read only)",
		Long: `Serve the canonical bars over a read-only REST API:

  GET /v1/marketdata/bars?instrument=EURUSD&interval=h1&from=2024-01-01T00:00:00Z&to=2024-02-01T00:00:00Z

It is a transport only: the same service the command line uses reads the files, and
nothing is fetched from a provider, so it needs no provider credentials and makes no
network call out. By default it listens on ` + defaultListen + ` only. Listening on any
other address (a non-loopback one) is refused unless ` + envAPIToken + ` is set in the
environment, in which case every request must carry "Authorization: Bearer <token>".
There is no TLS: put it behind a proxy if it leaves the machine. The data root is
--data-root, then ` + envDataRoot + `, then marketdata.root in ` + defaultConfigPath + `, then
the default ` + store.DefaultRoot + `. It runs until interrupted and then finishes the
requests in flight.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return a.runServe(cmd, f)
		},
	}
	fl := cmd.Flags()
	fl.StringVar(&f.config, "config", defaultConfigPath, "config file (a missing default file is fine when flags or the environment supply the values)")
	fl.StringVar(&f.dataRoot, "data-root", "", "data root (default: "+envDataRoot+", then marketdata.root in the config file, then "+store.DefaultRoot+")")
	fl.StringVar(&f.listen, "listen", defaultListen, "address to listen on, host:port (a non-loopback host needs "+envAPIToken+")")
	fl.DurationVar(&f.timeout, "request-timeout", rest.DefaultTimeout, "longest one request may take")
	return cmd
}

// loopback reports whether a listen address binds to the loopback interface only.
func loopback(addr string) (bool, error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false, fmt.Errorf("--listen %q: %w", addr, err)
	}
	if host == "localhost" {
		return true, nil
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback(), nil // an empty host or 0.0.0.0 listens on every interface
}

// runServe resolves the options and serves rest.NewHandler(service.ReadBars) until the
// context ends. Transport only: no provider and no domain package.
func (a *app) runServe(cmd *cobra.Command, f serveFlags) error {
	local, err := loopback(f.listen)
	if err != nil {
		return err
	}
	token := a.getenv(envAPIToken)
	if !local && token == "" {
		return fmt.Errorf("--listen %q is not a loopback address: set %s so every request must present a bearer token", f.listen, envAPIToken)
	}
	if f.timeout <= 0 {
		return errors.New("--request-timeout must be positive")
	}
	cfg, err := loadConfig(f.config, cmd.Flags().Changed("config"))
	if err != nil {
		return err
	}
	if err := useRoot(resolveRoot(f.dataRoot, a.getenv(envDataRoot), cfg.Marketdata.Root)); err != nil {
		return err
	}
	ln, err := net.Listen("tcp", f.listen)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}

	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
	defer stop()
	log := logging.WithComponent(slog.Default(), logging.ComponentMarketData)
	handler := rest.NewHandler(rest.Config{
		ReadBars: func(ctx context.Context, req service.BarsRequest) (service.BarsPage, error) {
			return service.ReadBars(ctx, log, req)
		},
		Token: token, Timeout: f.timeout, Log: log,
	})
	srv := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      f.timeout + 5*time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 16,
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}

	auth := "no authentication (loopback only)"
	if token != "" {
		auth = "bearer token required"
	}
	if _, werr := fmt.Fprintf(cmd.OutOrStdout(), "listening on http://%s (data root %s; %s)\n", ln.Addr(), store.Root(), auth); werr != nil {
		_ = ln.Close()
		return fmt.Errorf("write: %w", werr)
	}

	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	select {
	case err := <-errc:
		return err // the server stopped by itself: never a clean Shutdown
	case <-ctx.Done():
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdown); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	if err := <-errc; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
