// Package mcpserver provides Trader's narrow MCP research adapter.
//
// MCP is an agent-facing transport over Trader's application services
// (ADR-022): tool handlers translate requests and results, and every
// piece of business logic stays in the services below. The server
// receives those services by injection (Deps), constructed at the
// composition root (cmd/trader-mcp), so tests can build it with doubles
// or temp-dir services.
//
// # Write access
//
// Tools that change Trader's data — writing to the raw or canonical
// store, or downloading with provider credentials — are disabled unless
// the server is started with writes allowed (Deps.AllowWrites; the
// trader-mcp flag --allow-writes, or TRADER_MCP_ALLOW_WRITES=true).
// A write tool on a server without write access returns ErrWritesDisabled
// as a tool error the client can show, naming how to enable it.
// Read-only tools are always available. This keeps the server's original
// read/research posture (#405) the default, and makes data mutation an
// explicit operator decision (#433).
//
// # Output
//
// On the stdio transport, stdout carries the MCP protocol. Logs go to
// Deps.Logger, which the composition root must not point at stdout.
// Credentials are never included in tool results or logs.
package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/rustyeddy/trader/instrument"
	svcmarketdata "github.com/rustyeddy/trader/internal/service/marketdata"
	"github.com/rustyeddy/trader/version"
)

// ErrWritesDisabled reports a data-mutating tool called on a server
// started without write access.
var ErrWritesDisabled = errors.New("data-mutating tools are disabled on this server; restart trader-mcp with --allow-writes (or TRADER_MCP_ALLOW_WRITES=true)")

// ErrMarketDataUnavailable reports a market-data tool called on a server
// constructed without a MarketData factory.
var ErrMarketDataUnavailable = errors.New("market data is not configured on this server")

// MarketData is one provider's market-data service: the application
// service, the resolver its Manager resolves instruments through
// (registered per request), and the provider and archive root it serves.
type MarketData struct {
	Service     *svcmarketdata.Service
	Resolver    *instrument.MemoryResolver
	Provider    string
	ArchiveRoot string
}

// MarketDataFactory builds the market-data service for a request's
// provider. A Manager serves exactly one provider, so the server asks
// for one per request; an empty provider means DefaultProvider.
type MarketDataFactory interface {
	DefaultProvider() string
	ForProvider(provider string) (MarketData, error)
}

// Deps are the server's injected dependencies.
type Deps struct {
	// Logger receives the server's structured logs. Nil discards them.
	Logger *slog.Logger
	// MarketData builds per-provider market-data services. Nil leaves
	// market-data tools reporting ErrMarketDataUnavailable.
	MarketData MarketDataFactory
	// AllowWrites enables data-mutating tools (see the package doc).
	AllowWrites bool
}

// server holds the dependencies tool handlers use.
type server struct {
	deps Deps
}

// New constructs the Trader MCP server over deps.
func New(deps Deps) *mcp.Server {
	if deps.Logger == nil {
		deps.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	s := &server{deps: deps}
	srv := mcp.NewServer(&mcp.Implementation{Name: "trader-mcp", Version: version.Current().Version}, nil)
	s.register(srv)
	return srv
}

// register adds every tool to srv. Tools that need the server's
// dependencies are methods on s.
func (s *server) register(srv *mcp.Server) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "trader_version",
		Description: "Return the Trader build identity serving this MCP session.",
	}, traderVersion)
}

// requireWrites returns ErrWritesDisabled unless the server allows
// data-mutating tools. A write tool calls it before doing anything.
func (s *server) requireWrites(tool string) error {
	if s.deps.AllowWrites {
		return nil
	}
	s.deps.Logger.Warn("mcp write tool refused", "tool", tool, "reason", "writes disabled")
	return fmt.Errorf("%s: %w", tool, ErrWritesDisabled)
}

// marketData returns the market-data service for provider, or
// ErrMarketDataUnavailable when the server has none.
func (s *server) marketData(provider string) (MarketData, error) {
	if s.deps.MarketData == nil {
		return MarketData{}, ErrMarketDataUnavailable
	}
	return s.deps.MarketData.ForProvider(provider)
}

// VersionInput is empty because trader_version has no arguments.
type VersionInput struct{}

// VersionOutput identifies the Trader build serving MCP requests.
type VersionOutput struct {
	Version    string `json:"version"`
	Revision   string `json:"revision,omitempty"`
	Dirty      bool   `json:"dirty"`
	CommitTime string `json:"commit_time,omitempty"`
}

func traderVersion(context.Context, *mcp.CallToolRequest, VersionInput) (*mcp.CallToolResult, VersionOutput, error) {
	info := version.Current()
	output := VersionOutput{Version: info.Version, Revision: info.Revision, Dirty: info.Dirty}
	if !info.CommitTime.IsZero() {
		output.CommitTime = info.CommitTime.UTC().Format("2006-01-02T15:04:05Z07:00")
	}
	return nil, output, nil
}
