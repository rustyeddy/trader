// Command trader-mcp exposes Trader's typed research capabilities over
// MCP on stdio.
//
// It is a composition root (issue #433): it resolves configuration the
// same way the trader CLI's "data" commands do — the same TRADER_*
// environment variables, defaults, and credentials, through the shared
// cmd/internal/marketdatacfg package — builds the services, and injects
// them into internal/mcpserver.
//
// Flags:
//
//	--store-root, --raw-root, --archive-root, --provider,
//	--oanda-base-url, --alpaca-base-url   as for "trader data"
//	--allow-writes                        enable data-mutating tools
//	--log-level, --log-format, --log-output
//
// Credentials come from the environment only (TRADER_OANDA_TOKEN,
// TRADER_ALPACA_KEY_ID, TRADER_ALPACA_SECRET_KEY), never flags, and are
// never logged. Logs default to stderr; stdout carries the MCP protocol,
// so --log-output stdout is rejected.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/rustyeddy/trader/cmd/internal/marketdatacfg"
	"github.com/rustyeddy/trader/internal/config"
	"github.com/rustyeddy/trader/internal/logging"
	"github.com/rustyeddy/trader/internal/mcpserver"
	"github.com/rustyeddy/trader/version"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Environ(), &mcp.StdioTransport{}, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "trader-mcp: %v\n", err)
		os.Exit(1)
	}
}

// run builds the server from args and environ, then serves it on
// transport until ctx ends or the client disconnects.
func run(ctx context.Context, args, environ []string, transport mcp.Transport, stderr io.Writer) error {
	srv, closer, err := build(args, environ, stderr)
	if errors.Is(err, flag.ErrHelp) {
		return nil // -h/--help printed usage; that is success, not a startup error
	}
	if err != nil {
		return err
	}
	defer func() { _ = closer.Close() }()
	return srv.Run(ctx, transport)
}

// serverConfig is trader-mcp's own setting beyond market data and
// logging.
type serverConfig struct {
	AllowWrites bool `config:"mcp_allow_writes" flag:"allow-writes"`
}

// errLogToStdout reports a log destination that would corrupt the MCP
// stdio stream.
var errLogToStdout = errors.New("--log-output stdout would corrupt the MCP stdio stream; use stderr or a file path")

// build resolves configuration and constructs the server. The returned
// closer releases the log output.
func build(args, environ []string, stderr io.Writer) (*mcp.Server, io.Closer, error) {
	fs := flag.NewFlagSet("trader-mcp", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.String("store-root", "", "canonical data store root")
	fs.String("raw-root", "", "raw data root for --provider")
	fs.String("archive-root", "", "provider archive root for --provider")
	fs.String("provider", "", "default market-data provider: oanda, alpaca, or stooq (default oanda)")
	fs.String("oanda-base-url", "", "OANDA API base URL")
	fs.String("alpaca-base-url", "", "Alpaca data API base URL")
	fs.Bool("allow-writes", false, "enable data-mutating tools")
	fs.String("log-level", "", "log level: DEBUG, INFO, WARN, or ERROR (default INFO)")
	fs.String("log-format", "", "log format: text or json (default text)")
	fs.String("log-output", "", "log output: stderr or a file path (default stderr)")
	if err := fs.Parse(args); err != nil {
		return nil, nil, err
	}
	if fs.NArg() > 0 {
		return nil, nil, fmt.Errorf("unexpected arguments %q: trader-mcp takes flags only", fs.Args())
	}

	// Only flags actually given override environment and defaults, as in
	// the trader CLI.
	data, logs, server := map[string]string{}, map[string]string{}, map[string]string{}
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "log-level":
			logs["level"] = f.Value.String()
		case "log-format":
			logs["format"] = f.Value.String()
		case "log-output":
			logs["output"] = f.Value.String()
		case "allow-writes":
			server["allow-writes"] = f.Value.String()
		default:
			data[f.Name] = f.Value.String()
		}
	})

	logCfg, err := config.Load[logging.Config](config.Options{EnvPrefix: marketdatacfg.EnvPrefix, Environ: environ, Overrides: logs})
	if err != nil {
		return nil, nil, err
	}
	if logCfg.Output == "stdout" {
		return nil, nil, errLogToStdout
	}
	logger, closer, err := logging.New(logCfg)
	if err != nil {
		return nil, nil, err
	}

	srvCfg, err := config.Load[serverConfig](config.Options{EnvPrefix: marketdatacfg.EnvPrefix, Environ: environ, Overrides: server})
	if err != nil {
		_ = closer.Close()
		return nil, nil, err
	}
	dataCfg, err := marketdatacfg.Load(environ, data)
	if err != nil {
		_ = closer.Close()
		return nil, nil, err
	}
	factory, err := marketdatacfg.NewFactory(dataCfg, logger)
	if err != nil {
		_ = closer.Close()
		return nil, nil, err
	}

	logger.Info("trader-mcp starting",
		slog.String("version", version.Current().Version),
		slog.String("default_provider", factory.DefaultProvider()),
		slog.Bool("writes_enabled", srvCfg.AllowWrites))

	srv := mcpserver.New(mcpserver.Deps{
		Logger:      logger,
		MarketData:  marketDataFactory{factory},
		AllowWrites: srvCfg.AllowWrites,
	})
	return srv, closer, nil
}

// marketDataFactory adapts marketdatacfg.Factory to
// mcpserver.MarketDataFactory.
type marketDataFactory struct{ f *marketdatacfg.Factory }

func (m marketDataFactory) DefaultProvider() string { return m.f.DefaultProvider() }

func (m marketDataFactory) ForProvider(provider string) (mcpserver.MarketData, error) {
	b, err := m.f.ForProvider(provider)
	if err != nil {
		return mcpserver.MarketData{}, err
	}
	return mcpserver.MarketData{Service: b.Service, Resolver: b.Resolver, Provider: b.Provider, ArchiveRoot: b.ArchiveRoot}, nil
}
