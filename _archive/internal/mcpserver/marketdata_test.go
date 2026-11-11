package mcpserver

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/internal/clock"
	marketruntime "github.com/rustyeddy/trader/internal/marketdata"
	svcmarketdata "github.com/rustyeddy/trader/internal/service/marketdata"
	"github.com/rustyeddy/trader/marketdata"
)

// fixtureService is a real market-data Service over temp-dir stores, as
// the composition root builds one.
type fixtureService struct {
	*svcmarketdata.Service
	rawRoot, storeRoot string
}

// newFixtureService builds provider's Service. For oanda, its raw root
// holds a copy of service/marketdata's committed EURUSD fixture (January
// and February 2024 H1).
func newFixtureService(t *testing.T, provider string, opts ...svcmarketdata.Option) fixtureService {
	t.Helper()
	rawRoot, storeRoot := t.TempDir(), t.TempDir()
	cfg := marketruntime.Config{
		Clock: clock.NewSimulated(time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC)), StoreRoot: storeRoot,
		RawRoot: rawRoot, ProviderName: provider,
	}
	if provider == "oanda" {
		require.NoError(t, os.CopyFS(rawRoot, os.DirFS(filepath.Join("..", "service", "marketdata", "testdata", "raw", "oanda"))))
	} else {
		cfg.Calendar = marketdata.NewUSEquityCalendar(marketdata.StandardUSEquityHolidays(2024))
	}
	resolver := instrument.NewMemoryResolver()
	cfg.Resolver = resolver
	manager, err := marketruntime.New(cfg)
	require.NoError(t, err)
	s, err := svcmarketdata.New(manager, nil, append([]svcmarketdata.Option{svcmarketdata.WithResolver(resolver)}, opts...)...)
	require.NoError(t, err)
	return fixtureService{Service: s, rawRoot: rawRoot, storeRoot: storeRoot}
}

// serviceFactory serves fixed Services by provider, defaulting to oanda.
type serviceFactory map[string]*svcmarketdata.Service

func (f serviceFactory) DefaultProvider() string { return "oanda" }

func (f serviceFactory) ForProvider(provider string) (MarketData, error) {
	if provider == "" {
		provider = f.DefaultProvider()
	}
	s, ok := f[provider]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownProvider, provider)
	}
	return s, nil
}

// connect serves deps over an in-memory MCP transport and returns a
// client session.
func connect(t *testing.T, deps Deps) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := New(deps).Connect(ctx, serverTransport, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = serverSession.Close() })
	session, err := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v1"}, nil).Connect(ctx, clientTransport, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })
	return session
}

// call invokes tool and decodes its structured result into out, returning
// the raw result so a test can check IsError.
func call(t *testing.T, session *mcp.ClientSession, tool string, args map[string]any, out any) *mcp.CallToolResult {
	t.Helper()
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: tool, Arguments: args})
	require.NoError(t, err)
	if !result.IsError && out != nil {
		raw, err := json.Marshal(result.StructuredContent)
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(raw, out))
	}
	return result
}

func errorText(r *mcp.CallToolResult) string {
	for _, c := range r.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			return tc.Text
		}
	}
	return ""
}

func TestInstrumentsTool(t *testing.T) {
	oanda := newFixtureService(t, "oanda")
	stooq := newFixtureService(t, "stooq")
	session := connect(t, Deps{MarketData: serviceFactory{"oanda": oanda.Service, "stooq": stooq.Service}})

	var fx InstrumentsOutput
	require.False(t, call(t, session, "trader_instruments", map[string]any{"symbols": []string{"EURUSD", "eurusd", "BAD"}}, &fx).IsError)
	assert.Equal(t, "oanda", fx.Provider, "the server's default provider")
	require.Len(t, fx.Instruments, 3)
	assert.Equal(t, InstrumentOutput{Symbol: "EURUSD", InstrumentID: fx.Instruments[0].InstrumentID, Kind: "fx", ProviderSymbol: "EURUSD"}, fx.Instruments[0])
	assert.NotEmpty(t, fx.Instruments[0].InstrumentID)
	assert.Equal(t, fx.Instruments[0].InstrumentID, fx.Instruments[1].InstrumentID, "a repeated symbol resolves to the same instrument")
	assert.Equal(t, "BAD", fx.Instruments[2].Symbol)
	assert.Contains(t, fx.Instruments[2].Error, "6-letter FX pair", "a bad symbol fails only its own entry")

	var eq InstrumentsOutput
	require.False(t, call(t, session, "trader_instruments", map[string]any{"symbols": []string{"SPY", "AAPL", "MSFT"}, "provider": "stooq"}, &eq).IsError)
	assert.Equal(t, "stooq", eq.Provider)
	assert.Equal(t, InstrumentOutput{Symbol: "SPY", InstrumentID: instrument.ETFID("ARCA", "SPY").String(), Kind: "etf", Exchange: "ARCA", ProviderSymbol: "SPY"}, eq.Instruments[0])
	assert.Equal(t, "equity", eq.Instruments[1].Kind)
	assert.Contains(t, eq.Instruments[2].Error, "no listing default", "an equity without reference metadata")
}

func TestCoverageTool(t *testing.T) {
	ctx := context.Background()
	oanda := newFixtureService(t, "oanda")
	session := connect(t, Deps{MarketData: serviceFactory{"oanda": oanda.Service}})
	args := map[string]any{"symbols": []string{"EURUSD", "BAD"}, "interval": "h1"}

	var before CoverageOutput
	require.False(t, call(t, session, "trader_marketdata_coverage", args, &before).IsError)
	assert.Equal(t, "oanda", before.Provider)
	assert.Equal(t, "H1", before.Interval)
	require.Len(t, before.Results, 2)
	eur := before.Results[0]
	assert.Empty(t, eur.Error)
	assert.Nil(t, eur.Range, "nothing built yet: no canonical span to cover")
	assert.Empty(t, eur.Partitions)
	assert.Nil(t, eur.Canonical)
	require.NotNil(t, eur.Raw)
	assert.Equal(t, DataSpanOutput{First: "2024-01-07T22:00:00Z", Last: "2024-02-05T00:00:00Z", End: "2024-02-05T01:00:00Z", Partitions: 2}, *eur.Raw)
	assert.NotEmpty(t, before.Results[1].Error)

	span, err := marketdata.NewTimeRange(time.Date(2024, 1, 7, 22, 0, 0, 0, time.UTC), time.Date(2024, 1, 19, 22, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	_, err = oanda.CanonicalizeDatasets(ctx, svcmarketdata.DatasetsRequest{
		Instruments: []svcmarketdata.InstrumentRequest{{Symbol: "EURUSD"}}, Interval: marketdata.H1, Range: span,
	}, svcmarketdata.CanonicalizeOptions{})
	require.NoError(t, err)

	var after CoverageOutput
	require.False(t, call(t, session, "trader_marketdata_coverage", args, &after).IsError)
	eur = after.Results[0]
	require.NotNil(t, eur.Range)
	assert.Equal(t, TimeSpan{Start: "2024-01-07T22:00:00Z", End: eur.Canonical.End}, *eur.Range, "defaults to the canonical span")
	require.Len(t, eur.Partitions, 1)
	assert.Equal(t, "2024-01", eur.Partitions[0].Month)
	assert.Equal(t, "current", eur.Partitions[0].Status)
	assert.Positive(t, eur.Partitions[0].BarCount)

	// The tool reports exactly what the service operation the CLI's
	// `trader data coverage` runs reports for the same dataset.
	direct, err := oanda.DatasetsCoverage(ctx, svcmarketdata.DatasetsRequest{
		Instruments: []svcmarketdata.InstrumentRequest{{Symbol: "EURUSD"}, {Symbol: "BAD"}}, Interval: marketdata.H1,
	})
	require.NoError(t, err)
	require.Len(t, direct.Results, 2)
	translate := &server{deps: Deps{Logger: slog.New(slog.DiscardHandler)}}
	assert.Equal(t, translate.symbolCoverage(ctx, direct.Results[0]), after.Results[0])
	assert.Equal(t, translate.symbolCoverage(ctx, direct.Results[1]), after.Results[1])

	var ranged CoverageOutput
	require.False(t, call(t, session, "trader_marketdata_coverage", map[string]any{
		"symbols": []string{"EURUSD"}, "interval": "H1", "from": "2024-01-08", "to": "2024-03-01",
	}, &ranged).IsError)
	assert.Equal(t, TimeSpan{Start: "2024-01-08T00:00:00Z", End: "2024-03-01T00:00:00Z"}, *ranged.Results[0].Range)
	require.Len(t, ranged.Results[0].Partitions, 2)
	assert.Equal(t, "missing", ranged.Results[0].Partitions[1].Status, "February was never built")
}

func TestCoverageTool_CallFailures(t *testing.T) {
	oanda := newFixtureService(t, "oanda")
	session := connect(t, Deps{MarketData: serviceFactory{"oanda": oanda.Service}})

	tests := map[string]struct {
		args map[string]any
		want string
	}{
		"invalid interval": {map[string]any{"symbols": []string{"EURUSD"}, "interval": "H99"}, "invalid interval"},
		"one range end":    {map[string]any{"symbols": []string{"EURUSD"}, "interval": "H1", "from": "2024-01-01"}, "from and to"},
		"invalid date":     {map[string]any{"symbols": []string{"EURUSD"}, "interval": "H1", "from": "jan", "to": "feb"}, "invalid date"},
		"unknown provider": {map[string]any{"symbols": []string{"EURUSD"}, "interval": "H1", "provider": "bloomberg"}, "unknown market-data provider"},
		"no symbols":       {map[string]any{"symbols": []string{}, "interval": "H1"}, "at least one instrument"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			result := call(t, session, "trader_marketdata_coverage", tc.args, nil)
			require.True(t, result.IsError)
			assert.Contains(t, errorText(result), tc.want)
		})
	}

	result := call(t, session, "trader_instruments", map[string]any{"symbols": []string{}}, nil)
	require.True(t, result.IsError)
	assert.Contains(t, errorText(result), "at least one instrument")
}

func TestMarketDataTools_Unavailable(t *testing.T) {
	session := connect(t, Deps{})
	for tool, args := range map[string]map[string]any{
		"trader_instruments":         {"symbols": []string{"EURUSD"}},
		"trader_marketdata_coverage": {"symbols": []string{"EURUSD"}, "interval": "H1"},
	} {
		result := call(t, session, tool, args, nil)
		require.True(t, result.IsError, tool)
		assert.Contains(t, errorText(result), ErrMarketDataUnavailable.Error(), tool)
	}
}

// fingerprint hashes every file under root, keyed by relative path.
func fingerprint(t *testing.T, root string) map[string][32]byte {
	t.Helper()
	out := map[string][32]byte{}
	require.NoError(t, filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		out[rel] = sha256.Sum256(b)
		return nil
	}))
	return out
}

func TestMarketDataTools_NeverMutate(t *testing.T) {
	ctx := context.Background()
	oanda := newFixtureService(t, "oanda")
	session := connect(t, Deps{MarketData: serviceFactory{"oanda": oanda.Service}})
	calls := func() {
		call(t, session, "trader_instruments", map[string]any{"symbols": []string{"EURUSD", "USDJPY"}}, nil)
		call(t, session, "trader_marketdata_coverage", map[string]any{"symbols": []string{"EURUSD", "USDJPY"}, "interval": "H1"}, nil)
		call(t, session, "trader_marketdata_coverage", map[string]any{"symbols": []string{"EURUSD"}, "interval": "H1", "from": "2024-01-01", "to": "2024-03-01"}, nil)
		call(t, session, "trader_marketdata_coverage", map[string]any{"symbols": []string{"EURUSD"}, "interval": "W1"}, nil)
	}

	// Before anything is built: raw data present, canonical store empty.
	raw, store := fingerprint(t, oanda.rawRoot), fingerprint(t, oanda.storeRoot)
	calls()
	assert.Equal(t, raw, fingerprint(t, oanda.rawRoot))
	assert.Empty(t, fingerprint(t, oanda.storeRoot), "coverage never builds")
	assert.Equal(t, store, fingerprint(t, oanda.storeRoot))

	// With canonical data present.
	span, err := marketdata.NewTimeRange(time.Date(2024, 1, 7, 22, 0, 0, 0, time.UTC), time.Date(2024, 1, 19, 22, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	_, err = oanda.CanonicalizeDatasets(ctx, svcmarketdata.DatasetsRequest{
		Instruments: []svcmarketdata.InstrumentRequest{{Symbol: "EURUSD"}}, Interval: marketdata.H1, Range: span,
	}, svcmarketdata.CanonicalizeOptions{})
	require.NoError(t, err)
	raw, store = fingerprint(t, oanda.rawRoot), fingerprint(t, oanda.storeRoot)
	require.NotEmpty(t, store)
	calls()
	assert.Equal(t, raw, fingerprint(t, oanda.rawRoot))
	assert.Equal(t, store, fingerprint(t, oanda.storeRoot))
}
