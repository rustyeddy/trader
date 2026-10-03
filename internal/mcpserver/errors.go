package mcpserver

import (
	"context"
	"errors"

	svcmarketdata "github.com/rustyeddy/trader/internal/service/marketdata"
)

// ErrUnknownProvider reports a request naming a provider the server has
// no market-data integration for. A MarketDataFactory wraps it so the
// error can be shown to the client.
var ErrUnknownProvider = errors.New("unknown market-data provider")

// echoesInput lists errors whose messages are built only from the
// caller's own input or the server's own policy, so a client may see
// them verbatim.
var echoesInput = []error{
	svcmarketdata.ErrInvalidRequest,
	svcmarketdata.ErrInvalidInterval,
	svcmarketdata.ErrInvalidDate,
	svcmarketdata.ErrInvalidSymbol,
	svcmarketdata.ErrNoListingDefault,
	ErrUnknownProvider,
	ErrMarketDataUnavailable,
	ErrWritesDisabled,
}

// publicMessage is the single policy for what a client sees of an error
// (ADR-068: MCP never exposes filesystem layout or composition
// internals). Errors built from the caller's input pass through; listing
// conflicts and cancellation get fixed messages; anything else —
// storage, I/O, configuration, internal failures, whose messages may
// name paths — collapses to "<what> failed" while the full error is
// logged server-side.
func (s *server) publicMessage(ctx context.Context, tool, symbol, what string, err error) string {
	for _, safe := range echoesInput {
		if errors.Is(err, safe) {
			return err.Error()
		}
	}
	switch {
	case errors.Is(err, svcmarketdata.ErrListingConflict):
		return "a different instrument is already registered under this symbol"
	case errors.Is(err, context.Canceled):
		return "request canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "request timed out"
	}
	attrs := []any{"tool", tool, "error", err}
	if symbol != "" {
		attrs = append(attrs, "symbol", symbol)
	}
	s.deps.Logger.ErrorContext(ctx, "mcp tool error withheld from client", attrs...)
	return what + " failed; see the trader-mcp server log"
}

// publicError is publicMessage as an error, for a whole-call failure.
func (s *server) publicError(ctx context.Context, tool, what string, err error) error {
	return errors.New(s.publicMessage(ctx, tool, "", what, err))
}
