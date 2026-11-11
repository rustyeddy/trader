package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/instrument"
	svcmarketdata "github.com/rustyeddy/trader/internal/service/marketdata"
)

const secretPath = "/srv/secret/trader/canonical/oanda/EURUSD/2024/01/EURUSD-2024-01-h1.csv"

var errLeaky = fmt.Errorf("marketdata: store: months: open %s: permission denied", secretPath)

// leakyMarketData fails with errors naming a filesystem path: per symbol
// for the second symbol, or for the whole call when callErr is set.
type leakyMarketData struct {
	providerOnly
	callErr error
}

func (l leakyMarketData) ResolveInstruments(_ context.Context, req svcmarketdata.ResolveInstrumentsRequest) (svcmarketdata.ResolveInstrumentsResponse, error) {
	if l.callErr != nil {
		return svcmarketdata.ResolveInstrumentsResponse{}, l.callErr
	}
	return svcmarketdata.ResolveInstrumentsResponse{Results: []svcmarketdata.InstrumentResult{
		{Request: req.Instruments[0], InstrumentResponse: svcmarketdata.InstrumentResponse{Instrument: instrument.ETFID("ARCA", "SPY")}},
		{Request: req.Instruments[1], Err: errLeaky},
	}}, nil
}

func (l leakyMarketData) DatasetsCoverage(_ context.Context, req svcmarketdata.DatasetsRequest) (svcmarketdata.DatasetsCoverageResponse, error) {
	if l.callErr != nil {
		return svcmarketdata.DatasetsCoverageResponse{}, l.callErr
	}
	return svcmarketdata.DatasetsCoverageResponse{Results: []svcmarketdata.CoverageResult{
		{Request: req.Instruments[0], Instrument: instrument.ETFID("ARCA", "SPY")},
		{Request: req.Instruments[1], Err: errLeaky},
	}}, nil
}

type fixedFactory struct {
	md  MarketData
	err error
}

func (f fixedFactory) DefaultProvider() string { return "stooq" }
func (f fixedFactory) ForProvider(string) (MarketData, error) {
	return f.md, f.err
}

func TestToolErrorsNeverExposePaths(t *testing.T) {
	args := map[string]map[string]any{
		"trader_instruments":         {"symbols": []string{"SPY", "QQQ"}},
		"trader_marketdata_coverage": {"symbols": []string{"SPY", "QQQ"}, "interval": "D1"},
	}
	cases := map[string]MarketDataFactory{
		"per-symbol error": fixedFactory{md: leakyMarketData{providerOnly: "stooq"}},
		"whole-call error": fixedFactory{md: leakyMarketData{providerOnly: "stooq", callErr: errLeaky}},
		"factory error":    fixedFactory{err: fmt.Errorf("market data for provider %q: %w", "stooq", errLeaky)},
	}
	for name, factory := range cases {
		for tool, a := range args {
			t.Run(name+"/"+tool, func(t *testing.T) {
				var logs bytes.Buffer
				session := connect(t, Deps{Logger: slog.New(slog.NewTextHandler(&logs, nil)), MarketData: factory})
				result := call(t, session, tool, a, nil)

				seen, err := json.Marshal(result)
				require.NoError(t, err)
				assert.NotContains(t, string(seen), "/srv/secret", "the client never sees the path")
				assert.Contains(t, string(seen), "failed; see the trader-mcp server log")
				assert.Contains(t, logs.String(), secretPath, "the full cause is logged server-side")
				assert.Contains(t, logs.String(), "mcp tool error withheld from client")
			})
		}
	}
}

func TestPublicMessage(t *testing.T) {
	ctx := context.Background()
	var logs bytes.Buffer
	s := &server{deps: Deps{Logger: slog.New(slog.NewTextHandler(&logs, nil))}}
	msg := func(err error) string { return s.publicMessage(ctx, "tool", "SYM", "thing", err) }

	for _, err := range []error{
		fmt.Errorf("%w: symbol is required", svcmarketdata.ErrInvalidRequest),
		fmt.Errorf("%w %q: expected one of M1", svcmarketdata.ErrInvalidInterval, "H99"),
		fmt.Errorf("%w %q: expected YYYY-MM-DD", svcmarketdata.ErrInvalidDate, "jan"),
		fmt.Errorf("%w %q: expected a 6-letter FX pair", svcmarketdata.ErrInvalidSymbol, "BAD"),
		fmt.Errorf("%w: %q", svcmarketdata.ErrNoListingDefault, "MSFT"),
		fmt.Errorf("%w: %q (supported: oanda)", ErrUnknownProvider, "x"),
		ErrMarketDataUnavailable,
		fmt.Errorf("tool: %w", ErrWritesDisabled),
	} {
		assert.Equal(t, err.Error(), msg(err), "input errors pass through")
	}
	assert.Empty(t, logs.String(), "input errors are not logged as withheld")

	assert.Equal(t, "a different instrument is already registered under this symbol",
		msg(fmt.Errorf("%w: %w", svcmarketdata.ErrListingConflict, errors.New("provider \"x\" venue \"\" symbol \"S\""))))
	for sentinel, want := range map[error]string{
		svcmarketdata.ErrArchiveNotFound:          "no archive for this symbol under the server's archive root",
		svcmarketdata.ErrAmbiguousArchive:         "several archives under the server's archive root match this symbol",
		svcmarketdata.ErrArchiveRootNotConfigured: "the server has no archive root configured",
		svcmarketdata.ErrArchiveMemberNotFound:    "the archive holds no data for this symbol",
	} {
		assert.Equal(t, want, msg(fmt.Errorf("%w: SPY under %q", sentinel, secretPath)), "archive errors name the root, so they get fixed text")
	}
	for _, err := range []error{
		fmt.Errorf("%w in [2024-03-01T00:00:00Z, 2024-04-01T00:00:00Z)", svcmarketdata.ErrNoRawData),
		svcmarketdata.ErrNoCanonicalData,
		fmt.Errorf("%w: 1 action(s) remain, first: download-raw 2024-02 (extend)", svcmarketdata.ErrUpdateIncomplete),
	} {
		assert.Equal(t, err.Error(), msg(err))
	}
	assert.Equal(t, "request canceled", msg(fmt.Errorf("search %q: %w", secretPath, context.Canceled)))
	assert.Equal(t, "request timed out", msg(context.DeadlineExceeded))

	assert.Equal(t, "thing failed; see the trader-mcp server log", msg(errLeaky))
	assert.Contains(t, logs.String(), secretPath)
	assert.Contains(t, logs.String(), "symbol=SYM")

	assert.EqualError(t, s.publicError(ctx, "tool", "thing", errLeaky), "thing failed; see the trader-mcp server log")
}
