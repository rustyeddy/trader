package main

import (
	"context"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/marketdata"
)

func TestParseFXListing_Valid(t *testing.T) {
	listing, err := parseFXListing("oanda", "eurusd")
	require.NoError(t, err)
	require.Equal(t, "EURUSD", listing.Symbol())
	require.Equal(t, "oanda", listing.Provider())
	require.False(t, listing.InstrumentID().IsZero())
}

func TestParseFXListing_JPYUsesFinerTickSize(t *testing.T) {
	listing, err := parseFXListing("oanda", "USDJPY")
	require.NoError(t, err)
	require.Equal(t, "0.001", listing.Spec().TickSize().String())
}

func TestParseFXListing_NonJPYUsesStandardTickSize(t *testing.T) {
	listing, err := parseFXListing("oanda", "EURUSD")
	require.NoError(t, err)
	require.Equal(t, "0.00001", listing.Spec().TickSize().String())
}

func TestParseFXListing_RejectsWrongLength(t *testing.T) {
	_, err := parseFXListing("oanda", "EURO")
	require.Error(t, err)
}

func TestParseFXListing_RejectsInvalidCurrencyCode(t *testing.T) {
	_, err := parseFXListing("oanda", "1URUSD")
	require.Error(t, err)
}

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
