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
	"github.com/rustyeddy/trader/strategy/proto"
	strategyserver "github.com/rustyeddy/trader/strategy/server"
)

// envAPIToken is where the bearer token the API requires comes from (like the
// OANDA token, never a flag or a file).
const envAPIToken = "TRADER_API_TOKEN"

const defaultListen = "127.0.0.1:8080"

// defaultStrategyListen is where the strategy service listens unless told
// otherwise: loopback only, like the API. strategyOff turns it off.
const (
	defaultStrategyListen = "127.0.0.1:8081"
	strategyOff           = "off"
)

type serveFlags struct {
	config, dataRoot string
	listen           string
	strategyListen   string
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
requests in flight.

It also starts the strategy service, the loopback TCP protocol external strategies
connect to (ADR-094), on ` + defaultStrategyListen + ` unless --strategy-listen names another
loopback address; "` + strategyOff + `" turns it off. That service has no TLS or token, so an
address that is not loopback is refused. Until the engine is attached to it every
registration is refused with an explanation, and no run starts.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return a.runServe(cmd, f)
		},
	}
	fl := cmd.Flags()
	fl.StringVar(&f.config, "config", defaultConfigPath, "config file (a missing default file is fine when flags or the environment supply the values)")
	fl.StringVar(&f.dataRoot, "data-root", "", "data root (default: "+envDataRoot+", then marketdata.root in the config file, then "+store.DefaultRoot+")")
	fl.StringVar(&f.listen, "listen", defaultListen, "address to listen on, host:port (a non-loopback host needs "+envAPIToken+")")
	fl.StringVar(&f.strategyListen, "strategy-listen", defaultStrategyListen, "loopback address of the strategy service, host:port, or \""+strategyOff+"\" (a non-loopback host is refused)")
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
	if f.strategyListen != strategyOff {
		slocal, err := strategyserver.IsLoopbackAddr(f.strategyListen)
		if err != nil {
			return fmt.Errorf("--strategy-listen: %w", err)
		}
		if !slocal {
			return fmt.Errorf("--strategy-listen %q is not a loopback address: the strategy protocol has no TLS or token in v1", f.strategyListen)
		}
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

	var sln net.Listener
	if f.strategyListen != strategyOff {
		if sln, err = strategyserver.Listen(f.strategyListen); err != nil {
			_ = ln.Close()
			return fmt.Errorf("--strategy-listen: %w", err)
		}
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

	// The strategy service: nil channel when it is off, so its case never fires.
	var sdone chan struct{}
	var serr error
	if sln != nil {
		ssrv, nerr := strategyserver.New(strategyserver.Config{
			Admit:  admitNoEngine,
			Handle: func(context.Context, *strategyserver.Session) error { return errNoEngine },
			Logger: logging.WithComponent(slog.Default(), logging.ComponentStrategy),
		})
		if nerr != nil {
			_ = sln.Close()
			_ = ln.Close()
			return nerr
		}
		if _, werr := fmt.Fprintf(cmd.OutOrStdout(), "strategy protocol on tcp://%s (loopback only; runs are refused until the engine is attached)\n", sln.Addr()); werr != nil {
			_ = sln.Close()
			_ = ln.Close()
			return fmt.Errorf("write: %w", werr)
		}
		sdone = make(chan struct{})
		go func() { serr = ssrv.Serve(ctx, sln); close(sdone) }()
	} else if _, werr := fmt.Fprintln(cmd.OutOrStdout(), "strategy protocol off"); werr != nil {
		_ = ln.Close()
		return fmt.Errorf("write: %w", werr)
	}
	stopStrategy := func() {
		stop()
		if sdone != nil {
			<-sdone
		}
	}

	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	var runErr error
	select {
	case err := <-errc:
		stopStrategy()
		return err // the server stopped by itself: never a clean Shutdown
	case <-sdone:
		runErr = fmt.Errorf("strategy service stopped: %w", serr) // it only stops by itself on a failure
	case <-ctx.Done():
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdown); err != nil {
		stopStrategy()
		return fmt.Errorf("shutdown: %w", err)
	}
	if err := <-errc; err != nil && !errors.Is(err, http.ErrServerClosed) {
		stopStrategy()
		return err
	}
	stopStrategy()
	return runErr
}

// errNoEngine is why a strategy run cannot start yet.
var errNoEngine = errors.New("strategy runs are not available: the engine is not attached to the strategy service")

// admitNoEngine refuses every registration until the engine is attached to the
// service (#703): the service is up and observable, but starts no run, so nothing
// can reach an account or a broker through it.
func admitNoEngine(context.Context, *proto.Register) (strategyserver.Grant, error) {
	return strategyserver.Grant{}, strategyserver.Reject("strategy runs are not available yet: the engine is not attached to the strategy service (issue #703)")
}
