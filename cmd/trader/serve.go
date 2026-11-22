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
	"github.com/rustyeddy/trader/strategy"
)

// envAPIToken is where the bearer token the API requires comes from (like the
// OANDA token, never a flag or a file).
const envAPIToken = "TRADER_API_TOKEN"

const defaultListen = "127.0.0.1:8080"

type serveFlags struct {
	config, dataRoot, resultsRoot string
	listen                        string
	strategyListen                string
	timeout                       time.Duration
}

func newServeCmd(a *app) *cobra.Command {
	var f serveFlags
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run Trader as a server: market data over HTTP, and strategy clients over TCP",
		Long: `Run Trader as a long-lived server. It does two things, each on its own port.

It accepts strategy clients (--strategy-listen, on by default at ` + defaultStrategyListen + `, loopback
only; an empty value turns it off). A strategy is an external process that connects,
asks for one backtest (instruments, interval, range), trades it and receives the
result; examples/emax is the canonical one. Trader runs one such backtest at a time:
a second client waits until the first is done, and gives up after its own timeout.
Each run is journaled under the results root like trader backtest (--results-root,
` + envResultsRoot + `, results.root, then ` + service.DefaultResultsRoot + `), a client that fails
ends only its own run, and the server keeps running. The month before the range is
sent first as warm-up bars.

It serves the canonical bars over a read-only REST API:

  GET /v1/marketdata/bars?instrument=EURUSD&interval=h1&from=2024-01-01T00:00:00Z&to=2024-02-01T00:00:00Z

It is a transport only: the same service the command line uses reads the files, and
nothing is fetched from a provider, so it needs no provider credentials and makes no
network call out. By default it listens on ` + defaultListen + ` only. Listening on any
other address (a non-loopback one) is refused unless ` + envAPIToken + ` is set in the
environment, in which case every request must carry "Authorization: Bearer <token>".
There is no TLS: put it behind a proxy if it leaves the machine. The data root is
--data-root, then ` + envDataRoot + `, then marketdata.root in ` + defaultConfigPath + `, then
the default ` + store.DefaultRoot + `. It runs until interrupted, then finishes the
requests in flight and cancels the strategy run in progress, if any, waiting a few
seconds for it to be journaled as cancelled.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return a.runServe(cmd, f)
		},
	}
	fl := cmd.Flags()
	fl.StringVar(&f.config, "config", defaultConfigPath, "config file (a missing default file is fine when flags or the environment supply the values)")
	fl.StringVar(&f.dataRoot, "data-root", "", "data root (default: "+envDataRoot+", then marketdata.root in the config file, then "+store.DefaultRoot+")")
	fl.StringVar(&f.listen, "listen", defaultListen, "address to listen on, host:port (a non-loopback host needs "+envAPIToken+")")
	fl.StringVar(&f.strategyListen, "strategy-listen", defaultStrategyListen, "loopback host:port to accept strategy clients on; empty turns the strategy listener off")
	fl.StringVar(&f.resultsRoot, "results-root", "", "results root for strategy runs (default: "+envResultsRoot+", then results.root in the config file, then "+service.DefaultResultsRoot+")")
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
	sln, err := listenStrategy(f.strategyListen)
	if err != nil {
		_ = ln.Close()
		return err
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
		if sln != nil {
			_ = sln.Close()
		}
		return fmt.Errorf("write: %w", werr)
	}

	// The strategy listener serves one run at a time on its own goroutine. Its
	// handler's context is a child of ctx, so a failure of either server stops both.
	runCtx, cancelRuns := context.WithCancel(ctx)
	defer cancelRuns()
	var strategyDone chan error
	if sln != nil {
		resultsRoot := orDefault(resolveRoot(f.resultsRoot, a.getenv(envResultsRoot), cfg.Results.Root), service.DefaultResultsRoot)
		if _, werr := fmt.Fprintf(cmd.OutOrStdout(), "strategy clients on %s (results root %s)\n", sln.Addr(), resultsRoot); werr != nil {
			_ = ln.Close()
			_ = sln.Close()
			return fmt.Errorf("write: %w", werr)
		}
		strategyDone = make(chan error, 1)
		go func() {
			strategyDone <- strategy.ServeRuns(runCtx, sln, strategyRuns(logging.WithComponent(slog.Default(), logging.ComponentBacktest), resultsRoot))
		}()
	}

	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	var serveErr error
	select {
	case serveErr = <-errc:
		// The HTTP server stopped by itself: never a clean Shutdown.
		cancelRuns()
		waitStrategy(strategyDone)
		return serveErr
	case serveErr = <-strategyDone:
		strategyDone = nil
		cancelRuns()
		if serveErr == nil {
			serveErr = errors.New("the strategy listener stopped")
		}
		serveErr = fmt.Errorf("strategy listener: %w", serveErr)
	case <-ctx.Done():
	}
	cancelRuns() // the run in progress is cancelled and journaled as such
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdown); err != nil {
		return errors.Join(serveErr, fmt.Errorf("shutdown: %w", err))
	}
	waitStrategy(strategyDone)
	if err := <-errc; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return errors.Join(serveErr, err)
	}
	return serveErr
}

// waitStrategy gives the strategy listener a few seconds to finish the run it was
// cancelled in, so the run is journaled as cancelled before the process exits. A
// nil channel is no listener.
func waitStrategy(done <-chan error) {
	if done == nil {
		return
	}
	select {
	case <-done:
	case <-time.After(10 * time.Second):
	}
}
