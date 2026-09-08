package data_test

// This file is issue #331 (EQ-14)'s CLI-level test coverage: wiring
// the trader CLI to reach the "alpaca" provider, both credential
// configuration and non-FX instrument parsing. It complements
// args_test.go's own package-internal unit tests of
// registerRequestedInstrument/EquityRegistration dispatch.

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/marketdata"
)

// alpacaBarsJSONForCLITest builds a bars response in the shape the
// real Alpaca Market Data API returns (the same shape
// marketdata/alpaca_integration_test.go's own alpacaBarsJSONForTest
// uses) for dates, all under symbol.
func alpacaBarsJSONForCLITest(symbol string, dates []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, `{"bars":{%q:[`, symbol)
	for i, d := range dates {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, `{"t":"%sT04:00:00Z","o":100.00,"h":101.00,"l":99.00,"c":100.50,"v":123456}`, d)
	}
	b.WriteString(`]},"next_page_token":null}`)
	return b.String()
}

// TestDataSync_Alpaca_SucceedsAgainstFixtureBackedEndpoint is issue
// #331's own acceptance criterion: "trader data sync SPY d1 --provider
// alpaca ... succeeds against a real (or fixture-backed, for CI)
// Alpaca endpoint, given valid credentials." A real Alpaca account is
// not available in CI, so this points --alpaca-base-url at a local
// httptest.Server returning a real Alpaca-shaped response — the same
// technique marketdata/alpaca_integration_test.go's own fakeAlpacaDoer
// tests already establish at the package-internal level, exercised
// here through the actual CLI binary's command tree end to end:
// credential env vars, --provider/--exchange/--kind flag dispatch, and
// the real, unmodified marketdata.Manager sync path.
func TestDataSync_Alpaca_SucceedsAgainstFixtureBackedEndpoint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, alpacaBarsJSONForCLITest("SPY", []string{"2024-01-08", "2024-01-09"}))
	}))
	defer srv.Close()

	t.Setenv("TRADER_ALPACA_KEY_ID", "test-key")
	t.Setenv("TRADER_ALPACA_SECRET_KEY", "test-secret")

	storeRoot := t.TempDir()
	rawRoot := t.TempDir()

	out, err := runData(t, storeRoot, rawRoot,
		"sync", "SPY", "D1",
		"--provider", "alpaca", "--exchange", "ARCA", "--kind", "etf",
		"--alpaca-base-url", srv.URL,
		"--from", "2024-01-08", "--to", "2024-01-10",
		"--format", "json")
	require.NoError(t, err)

	var decoded struct {
		Downloaded []struct {
			RecordsWritten int `json:"records_written"`
		} `json:"downloaded"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &decoded))
	require.Len(t, decoded.Downloaded, 1)
	require.Equal(t, 2, decoded.Downloaded[0].RecordsWritten)
}

// TestDataSync_Alpaca_MissingCredentialsProducesClearError is issue
// #331's own acceptance criterion: missing/incomplete Alpaca
// credentials must produce a clear, classifiable CLI error, not a
// panic or a confusing failure. buildDataContext only ever forwards
// AlpacaCredential/AlpacaBaseURL to Manager together (see its own doc
// comment for why — a real regression this issue's development caught
// and fixed), so with neither TRADER_ALPACA_KEY_ID nor
// TRADER_ALPACA_SECRET_KEY set, Manager construction itself succeeds
// (nothing Alpaca-shaped was ever configured) and the classifiable
// marketdata.ErrInvalidConfig instead surfaces from Sync itself, once
// Sync discovers no Alpaca client was ever built — still exactly the
// same sentinel OANDA's own "credential and base URL must be supplied
// together" case wraps, just from a different call site. No httptest
// server is needed: this is rejected before any network call.
func TestDataSync_Alpaca_MissingCredentialsProducesClearError(t *testing.T) {
	storeRoot := t.TempDir()
	rawRoot := t.TempDir()

	_, err := runData(t, storeRoot, rawRoot,
		"sync", "SPY", "D1",
		"--provider", "alpaca", "--exchange", "ARCA", "--kind", "etf",
		"--from", "2024-01-08", "--to", "2024-01-10")
	require.Error(t, err)
	require.Contains(t, err.Error(), "Alpaca credential/base URL is not configured")
	require.True(t, errors.Is(err, marketdata.ErrInvalidConfig),
		"error must be classifiable via errors.Is(err, marketdata.ErrInvalidConfig), matching OANDA's own missing-credential behavior")
}

// TestDataSync_Alpaca_MissingExchangeProducesClearError proves the
// non-FX instrument-parsing half of #331: requesting a non-FX
// provider's instrument without --exchange fails with an actionable
// message before ever reaching Manager, rather than misinterpreting
// SPY as a malformed FX pair the way the pre-#331 CLI did.
func TestDataSync_Alpaca_MissingExchangeProducesClearError(t *testing.T) {
	storeRoot := t.TempDir()
	rawRoot := t.TempDir()

	_, err := runData(t, storeRoot, rawRoot,
		"sync", "SPY", "D1", "--provider", "alpaca", "--kind", "etf",
		"--from", "2024-01-08", "--to", "2024-01-10")
	require.Error(t, err)
	require.Contains(t, err.Error(), "--exchange is required")
}

// TestDataSync_Alpaca_MissingKindProducesClearError mirrors
// TestDataSync_Alpaca_MissingExchangeProducesClearError for --kind.
func TestDataSync_Alpaca_MissingKindProducesClearError(t *testing.T) {
	storeRoot := t.TempDir()
	rawRoot := t.TempDir()

	_, err := runData(t, storeRoot, rawRoot,
		"sync", "SPY", "D1", "--provider", "alpaca", "--exchange", "ARCA",
		"--from", "2024-01-08", "--to", "2024-01-10")
	require.Error(t, err)
	require.Contains(t, err.Error(), "--kind is required")
}

// TestDataSync_OANDA_StillDefaultsToFXParsing is issue #331's own "existing
// OANDA/FX CLI behavior is unaffected" acceptance criterion, exercised
// at the full command level: no --provider (defaults to oanda), no
// --exchange/--kind needed, EURUSD still parses as FX.
func TestDataSync_OANDA_StillDefaultsToFXParsing(t *testing.T) {
	rawRoot := copyFixtureRaw(t)
	storeRoot := t.TempDir()

	_, err := runData(t, storeRoot, rawRoot,
		"plan", "EURUSD", "H1", "--from", "2024-01-07", "--to", "2024-01-08")
	require.NoError(t, err)
}
