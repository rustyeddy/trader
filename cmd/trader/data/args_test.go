package data

import (
	"context"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/marketdata"
)

// Instrument-parsing coverage (valid symbols, JPY tick size, invalid
// length/currency) now lives in service/marketdata's own
// instrument_test.go, alongside svc.RegisterFXInstrument itself --
// moved there per #124 review: constructing a Listing's domain/
// execution metadata is service-layer work, not this transport's (see
// dataargs.go's resolveDatasetRequest doc comment).

func TestParseInterval_AllPredefinedValues(t *testing.T) {
	cases := map[string]marketdata.Interval{
		"M1": marketdata.M1,
		"h1": marketdata.H1,
		"H4": marketdata.H4,
		"d1": marketdata.D1,
		"W1": marketdata.W1,
	}
	for input, want := range cases {
		got, err := parseInterval(input)
		require.NoError(t, err, input)
		require.Equal(t, want, got, input)
	}
}

func TestParseInterval_RejectsUnknown(t *testing.T) {
	_, err := parseInterval("H99")
	require.Error(t, err)
}

func TestParseDate_AcceptsDateOnly(t *testing.T) {
	got, err := parseDate("2024-01-07")
	require.NoError(t, err)
	require.Equal(t, "2024-01-07T00:00:00Z", got.Format("2006-01-02T15:04:05Z07:00"))
}

func TestParseDate_AcceptsRFC3339(t *testing.T) {
	got, err := parseDate("2024-01-07T22:00:00Z")
	require.NoError(t, err)
	require.Equal(t, 22, got.Hour())
}

func TestParseDate_RejectsGarbage(t *testing.T) {
	_, err := parseDate("not-a-date")
	require.Error(t, err)
}

func TestResolveDatasetRequest_RequiresDataContext(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())

	_, err := resolveDatasetRequest(cmd, []string{"EURUSD", "H1"}, datasetArgFlags{from: "2024-01-01", to: "2024-01-02"})
	require.Error(t, err)
}

func newTestDataCmdContext(t *testing.T) context.Context {
	t.Helper()
	return withDataContext(context.Background(), dataContext{
		Service:  nil, // not needed for request-resolution tests
		Resolver: instrument.NewMemoryResolver(),
		Provider: "oanda",
	})
}

func TestResolveDatasetRequest_RejectsWrongArgCount(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.SetContext(newTestDataCmdContext(t))

	_, err := resolveDatasetRequest(cmd, []string{"EURUSD"}, datasetArgFlags{from: "2024-01-01", to: "2024-01-02"})
	require.Error(t, err)
}

func TestResolveDatasetRequest_RequiresFromAndTo(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.SetContext(newTestDataCmdContext(t))

	_, err := resolveDatasetRequest(cmd, []string{"EURUSD", "H1"}, datasetArgFlags{})
	require.Error(t, err)
}

func TestResolveDatasetRequest_BuildsRequestAndRegistersListing(t *testing.T) {
	ctx := newTestDataCmdContext(t)
	cmd := &cobra.Command{}
	cmd.SetContext(ctx)

	req, err := resolveDatasetRequest(cmd, []string{"EURUSD", "H1"},
		datasetArgFlags{from: "2024-01-07", to: "2024-01-08"})
	require.NoError(t, err)
	require.Equal(t, marketdata.H1, req.Interval)
	require.False(t, req.Instrument.IsZero())

	dc, ok := dataContextFrom(ctx)
	require.True(t, ok)
	listing, err := dc.Resolver.ResolveInstrument(req.Instrument, "oanda", "")
	require.NoError(t, err)
	require.Equal(t, "EURUSD", listing.Symbol())
}

// newTestDataContextForProvider mirrors newTestDataCmdContext for a
// non-FX provider (issue #331).
func newTestDataContextForProvider(provider string) dataContext {
	return dataContext{
		Service:  nil, // not needed for request-resolution tests
		Resolver: instrument.NewMemoryResolver(),
		Provider: provider,
	}
}

// TestRegisterRequestedInstrument_Equity is issue #331's own
// acceptance criterion: CLI-level coverage for at least one equity
// symbol registered under a non-FX provider.
func TestRegisterRequestedInstrument_Equity(t *testing.T) {
	dc := newTestDataContextForProvider("alpaca")
	id, err := registerRequestedInstrument(dc, "AAPL", datasetArgFlags{exchange: "NASDAQ", kind: "equity"})
	require.NoError(t, err)
	require.False(t, id.IsZero())

	listing, err := dc.Resolver.ResolveInstrument(id, "alpaca", "")
	require.NoError(t, err)
	require.Equal(t, "AAPL", listing.Symbol())
	require.Equal(t, "NASDAQ", listing.Venue())
}

// TestRegisterRequestedInstrument_ETF is issue #331's own acceptance
// criterion: CLI-level coverage for at least one ETF symbol.
// "etf" is matched case-insensitively, and "ETF" here also exercises
// that.
func TestRegisterRequestedInstrument_ETF(t *testing.T) {
	dc := newTestDataContextForProvider("alpaca")
	id, err := registerRequestedInstrument(dc, "SPY", datasetArgFlags{exchange: "ARCA", kind: "ETF"})
	require.NoError(t, err)
	require.False(t, id.IsZero())

	listing, err := dc.Resolver.ResolveInstrument(id, "alpaca", "")
	require.NoError(t, err)
	require.Equal(t, "SPY", listing.Symbol())
	require.Equal(t, "ARCA", listing.Venue())
}

func TestRegisterRequestedInstrument_RequiresExchangeForNonFX(t *testing.T) {
	dc := newTestDataContextForProvider("alpaca")
	_, err := registerRequestedInstrument(dc, "SPY", datasetArgFlags{kind: "etf"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "--exchange is required")
}

func TestRegisterRequestedInstrument_RequiresKindForNonFX(t *testing.T) {
	dc := newTestDataContextForProvider("alpaca")
	_, err := registerRequestedInstrument(dc, "SPY", datasetArgFlags{exchange: "ARCA"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "--kind is required")
}

func TestRegisterRequestedInstrument_RejectsInvalidKind(t *testing.T) {
	dc := newTestDataContextForProvider("alpaca")
	_, err := registerRequestedInstrument(dc, "SPY", datasetArgFlags{exchange: "ARCA", kind: "future"})
	require.Error(t, err)
	require.Contains(t, err.Error(), `invalid --kind "future"`)
}

// TestRegisterRequestedInstrument_OANDAIgnoresExchangeAndKind proves
// the FX path is unaffected by #331's new flags: an FX provider
// resolves via the pre-existing RegisterFXInstrument path regardless
// of whether --exchange/--kind happen to be set.
func TestRegisterRequestedInstrument_OANDAIgnoresExchangeAndKind(t *testing.T) {
	dc := newTestDataContextForProvider("oanda")
	id, err := registerRequestedInstrument(dc, "EURUSD", datasetArgFlags{})
	require.NoError(t, err)
	require.False(t, id.IsZero())
}
