package service

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rustyeddy/trader/brokers/oanda"
	"github.com/rustyeddy/trader/market"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// oandaTestServer builds a minimal OANDA-like HTTP server that returns
// fixed pricing and account data so PlaceMarketOrder can run without a
// real network connection.
//
// nav is the account NAV; bid/ask are the quoted prices for EUR_USD.
func oandaTestServer(t *testing.T, nav, bid, ask float64) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/summary"):
			json.NewEncoder(w).Encode(map[string]any{
				"account": map[string]any{
					"id":              "ACC1",
					"balance":         "100000.00",
					"NAV":             fmt.Sprintf("%.5f", nav),
					"marginUsed":      "0.00",
					"marginAvailable": fmt.Sprintf("%.5f", nav),
				},
			})
		case strings.HasSuffix(r.URL.Path, "/pricing"):
			json.NewEncoder(w).Encode(map[string]any{
				"prices": []any{
					map[string]any{
						"instrument": "EUR_USD",
						"bids":       []any{map[string]any{"price": fmt.Sprintf("%.5f", bid)}},
						"asks":       []any{map[string]any{"price": fmt.Sprintf("%.5f", ask)}},
						"status":     "tradeable",
					},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
}

// newTestService returns a Service backed by a fake OANDA server.
func newTestService(t *testing.T, nav, bid, ask float64) (*Service, *httptest.Server) {
	t.Helper()
	srv := oandaTestServer(t, nav, bid, ask)
	svc := &Service{
		AccountID: "ACC1",
		OANDA:     &oanda.Client{BaseURL: srv.URL, Token: "tok", HTTP: srv.Client()},
	}
	return svc, srv
}

// ── MaxUnits cap ────────────────────────────────────────────────────────────

func TestPlaceMarketOrder_MaxUnits_Caps(t *testing.T) {
	// Risk-based sizing: 100 000 NAV × 1% risk / (20 pip stop × 0.0001) = 500 000 units.
	// MaxUnits=5000 should reduce that to 5 000.
	svc, srv := newTestService(t, 100_000, 1.0850, 1.0852)
	defer srv.Close()

	result, err := svc.PlaceMarketOrder(t.Context(), PlaceMarketOrderRequest{
		Instrument: "EUR_USD",
		Side:       "long",
		RiskPct:    market.RateFromFloat(0.01),
		StopPips:   20,
		MaxUnits:   5000,
		Confirm:    false,
	})
	require.NoError(t, err)
	assert.EqualValues(t, 5000, result.Proposal.Units, "MaxUnits should cap long units")
}

func TestPlaceMarketOrder_MaxUnits_Short_Caps(t *testing.T) {
	svc, srv := newTestService(t, 100_000, 1.0850, 1.0852)
	defer srv.Close()

	result, err := svc.PlaceMarketOrder(t.Context(), PlaceMarketOrderRequest{
		Instrument: "EUR_USD",
		Side:       "short",
		RiskPct:    market.RateFromFloat(0.01),
		StopPips:   20,
		MaxUnits:   3000,
		Confirm:    false,
	})
	require.NoError(t, err)
	assert.EqualValues(t, -3000, result.Proposal.Units, "MaxUnits should cap short units (negative)")
}

func TestPlaceMarketOrder_MaxUnits_NoEffect_WhenBelowCap(t *testing.T) {
	// With 0.001% risk and a tight 20-pip stop, risk-based units will be tiny.
	svc, srv := newTestService(t, 100_000, 1.0850, 1.0852)
	defer srv.Close()

	result, err := svc.PlaceMarketOrder(t.Context(), PlaceMarketOrderRequest{
		Instrument: "EUR_USD",
		Side:       "long",
		RiskPct:    market.RateFromFloat(0.00001),
		StopPips:   20,
		MaxUnits:   100_000, // very high cap — should not trigger
		Confirm:    false,
	})
	require.NoError(t, err)
	// Risk-based units = (100_000 × 0.001/100) / (20×0.0001) = 1/0.002 = 5
	assert.Greater(t, result.Proposal.Units, int64(0))
	assert.LessOrEqual(t, result.Proposal.Units, int64(100_000))
}

// ── MaxPositionUSD cap ──────────────────────────────────────────────────────

func TestPlaceMarketOrder_MaxPositionUSD_Caps(t *testing.T) {
	// Entry ≈ 1.0852; MaxPositionUSD=5000 → max units = floor(5000/1.0852) = 4607.
	// Risk-based units at 1% risk with 20-pip stop = 50 000 → should be capped.
	svc, srv := newTestService(t, 100_000, 1.0850, 1.0852)
	defer srv.Close()

	result, err := svc.PlaceMarketOrder(t.Context(), PlaceMarketOrderRequest{
		Instrument:     "EUR_USD",
		Side:           "long",
		RiskPct:        market.RateFromFloat(0.01),
		StopPips:       20,
		MaxPositionUSD: 5000,
		Confirm:        false,
	})
	require.NoError(t, err)
	// floor(5000 / 1.0852) = 4607
	assert.LessOrEqual(t, result.Proposal.Units, int64(4608))
	assert.Greater(t, result.Proposal.Units, int64(4000))
}

func TestPlaceMarketOrder_MaxPositionUSD_Short_Caps(t *testing.T) {
	// Short: entry = bid = 1.0850. MaxPositionUSD=2000 → floor(2000/1.085) = 1843 units.
	svc, srv := newTestService(t, 100_000, 1.0850, 1.0852)
	defer srv.Close()

	result, err := svc.PlaceMarketOrder(t.Context(), PlaceMarketOrderRequest{
		Instrument:     "EUR_USD",
		Side:           "short",
		RiskPct:        market.RateFromFloat(0.01),
		StopPips:       20,
		MaxPositionUSD: 2000,
		Confirm:        false,
	})
	require.NoError(t, err)
	// floor(2000 / 1.0850) = 1843; units should be negative for short
	assert.LessOrEqual(t, result.Proposal.Units, int64(-1800))
	assert.GreaterOrEqual(t, result.Proposal.Units, int64(-1850))
}

func TestPlaceMarketOrder_BothCaps_TighterWins(t *testing.T) {
	// MaxUnits=10000, MaxPositionUSD=5000 (→~4607). The USD cap is tighter.
	svc, srv := newTestService(t, 100_000, 1.0850, 1.0852)
	defer srv.Close()

	result, err := svc.PlaceMarketOrder(t.Context(), PlaceMarketOrderRequest{
		Instrument:     "EUR_USD",
		Side:           "long",
		RiskPct:        market.RateFromFloat(0.01),
		StopPips:       20,
		MaxUnits:       10_000,
		MaxPositionUSD: 5000,
		Confirm:        false,
	})
	require.NoError(t, err)
	assert.LessOrEqual(t, result.Proposal.Units, int64(4608))
}

func TestPlaceMarketOrder_NoCaps_UsesRiskBased(t *testing.T) {
	// 100k NAV, 1% risk → $1000 risked. 20-pip stop = 0.002 distance.
	// Units = round(1000 / 0.002) = 500 000. No cap applies.
	svc, srv := newTestService(t, 100_000, 1.0850, 1.0852)
	defer srv.Close()

	result, err := svc.PlaceMarketOrder(t.Context(), PlaceMarketOrderRequest{
		Instrument: "EUR_USD",
		Side:       "long",
		RiskPct:    market.RateFromFloat(0.01),
		StopPips:   20,
		Confirm:    false,
	})
	require.NoError(t, err)
	assert.EqualValues(t, 500_000, result.Proposal.Units)
}

// ── Validation guards ───────────────────────────────────────────────────────

func TestPlaceMarketOrder_BadSide(t *testing.T) {
	svc, srv := newTestService(t, 100_000, 1.0850, 1.0852)
	defer srv.Close()

	_, err := svc.PlaceMarketOrder(t.Context(), PlaceMarketOrderRequest{
		Instrument: "EUR_USD",
		Side:       "sideways",
		RiskPct:    market.RateFromFloat(0.001),
		StopPips:   20,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "side must be")
}

func TestPlaceMarketOrder_NoStop(t *testing.T) {
	svc, srv := newTestService(t, 100_000, 1.0850, 1.0852)
	defer srv.Close()

	_, err := svc.PlaceMarketOrder(t.Context(), PlaceMarketOrderRequest{
		Instrument: "EUR_USD",
		Side:       "long",
		RiskPct:    market.RateFromFloat(0.001),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "StopPrice or StopPips")
}

// ── quoteToUSDRate ──────────────────────────────────────────────────────────

func TestQuoteToUSDRate_USDQuoted(t *testing.T) {
	t.Parallel()
	// EUR_USD, GBP_USD — quote is USD, rate must be 1.0
	assert.InDelta(t, 1.0, quoteToUSDRate("EUR_USD").Float64(), 1e-9)
	assert.InDelta(t, 1.0, quoteToUSDRate("GBP_USD").Float64(), 1e-9)
}

func TestQuoteToUSDRate_JPYQuoted(t *testing.T) {
	t.Parallel()
	// USD_JPY, AUD_JPY, EUR_JPY — quote is JPY ≈ 0.0067
	for _, inst := range []string{"USD_JPY", "AUD_JPY", "EUR_JPY"} {
		r := quoteToUSDRate(inst).Float64()
		assert.Greater(t, r, 0.0, "%s: rate must be > 0", inst)
		assert.Less(t, r, 0.1, "%s: JPY rate must be < 0.1", inst)
	}
}

func TestQuoteToUSDRate_GBPQuoted(t *testing.T) {
	t.Parallel()
	// EUR_GBP — quote is GBP ≈ 1.26
	r := quoteToUSDRate("EUR_GBP").Float64()
	assert.Greater(t, r, 1.0, "GBP rate must be > 1")
	assert.Less(t, r, 2.0, "GBP rate must be < 2")
}

// TestPlaceMarketOrder_JPYSizing verifies that a USD_JPY order with a 600-pip
// stop on a $2,000 account at 1% risk produces ~500 units (not ~3).
func TestPlaceMarketOrder_JPYSizing(t *testing.T) {
	// Build a test server that returns USD_JPY pricing.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/summary"):
			json.NewEncoder(w).Encode(map[string]any{
				"account": map[string]any{
					"id": "ACC1", "balance": "2000.00",
					"NAV": "2000.00", "marginUsed": "0.00", "marginAvailable": "2000.00",
				},
			})
		case strings.HasSuffix(r.URL.Path, "/pricing"):
			json.NewEncoder(w).Encode(map[string]any{
				"prices": []any{map[string]any{
					"instrument": "USD_JPY",
					"bids":       []any{map[string]any{"price": "150.000"}},
					"asks":       []any{map[string]any{"price": "150.010"}},
					"status":     "tradeable",
				}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	svc := &Service{
		AccountID: "ACC1",
		OANDA:     &oanda.Client{BaseURL: srv.URL, Token: "tok", HTTP: srv.Client()},
	}

	// 1% of $2,000 = $20 risk; 600-pip stop on USDJPY at 150
	// Expected: ~500 units ($20 / (6 JPY × 0.0067 USD/JPY))
	result, err := svc.PlaceMarketOrder(t.Context(), PlaceMarketOrderRequest{
		Instrument: "USD_JPY",
		Side:       "long",
		RiskPct:    market.RateFromFloat(0.01),
		StopPips:   600,
		Confirm:    false,
	})
	require.NoError(t, err)
	units := result.Proposal.Units
	// Should be in the hundreds, not single digits.
	assert.Greater(t, units, int64(100), "JPY sizing should produce >100 units, got %d", units)
	assert.Less(t, units, int64(2000), "JPY sizing should produce <2000 units, got %d", units)
	t.Logf("USD_JPY 600-pip stop, $2000 account, 1%% risk → %d units", units)
}
