package account

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rustyeddy/trader/brokers/oanda"
	"github.com/rustyeddy/trader/types"
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

// newTestAccount returns an Account session backed by a fake OANDA server.
func newTestAccount(t *testing.T, nav, bid, ask float64) (*Account, *httptest.Server) {
	t.Helper()
	srv := oandaTestServer(t, nav, bid, ask)
	acc := NewSession("ACC1", &oanda.Client{BaseURL: srv.URL, Token: "tok", HTTP: srv.Client()}, nil)
	return acc, srv
}

// marginConstrainedTestAccount returns an Account session backed by a fake
// OANDA server whose account has ample NAV but scarce marginAvailable —
// used to verify PlaceMarketOrder applies a margin check (chunk 7: sizing
// routes through account.SizePosition, which caps by min(risk, margin)
// where the old float implementation only capped by risk).
func marginConstrainedTestAccount(t *testing.T, nav, marginAvailable, bid, ask float64) (*Account, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/summary"):
			json.NewEncoder(w).Encode(map[string]any{
				"account": map[string]any{
					"id":              "ACC1",
					"balance":         fmt.Sprintf("%.5f", nav),
					"NAV":             fmt.Sprintf("%.5f", nav),
					"marginUsed":      "0.00",
					"marginAvailable": fmt.Sprintf("%.5f", marginAvailable),
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
	acc := NewSession("ACC1", &oanda.Client{BaseURL: srv.URL, Token: "tok", HTTP: srv.Client()}, nil)
	return acc, srv
}

// ── margin cap (chunk 7 — live previously had no margin check) ──────────────

func TestPlaceMarketOrder_MarginCap_BindsBelowRiskSizing(t *testing.T) {
	// Risk-based sizing alone: 100 000 NAV × 1% risk / (5 pip stop × 0.0001)
	// = 2 000 000 units — far more than $50 of margin can support on a
	// ~2% margin-rate instrument (EURUSD). SizePosition must return the
	// margin-capped amount, not the risk-only amount.
	acc, srv := marginConstrainedTestAccount(t, 100_000, 50, 1.0850, 1.0852)
	defer srv.Close()

	result, err := acc.PlaceMarketOrder(t.Context(), PlaceMarketOrderRequest{
		Instrument: "EUR_USD",
		Side:       "long",
		RiskPct:    types.RateFromFloat(0.01),
		StopPips:   5,
		Confirm:    false,
	})
	require.NoError(t, err)
	units := result.Proposal.Units
	assert.Greater(t, units, int64(0))
	assert.Less(t, units, int64(10_000), "margin of $50 should cap units well below the risk-only figure of 2,000,000, got %d", units)
}

func TestPlaceMarketOrder_MarginCap_InsufficientMargin_Errors(t *testing.T) {
	// marginAvailable is far too small for even the instrument's minimum
	// trade size — SizePosition should reject the order rather than
	// silently sizing to 1 unit (the old float behavior).
	acc, srv := marginConstrainedTestAccount(t, 100_000, 0.0001, 1.0850, 1.0852)
	defer srv.Close()

	_, err := acc.PlaceMarketOrder(t.Context(), PlaceMarketOrderRequest{
		Instrument: "EUR_USD",
		Side:       "long",
		RiskPct:    types.RateFromFloat(0.01),
		StopPips:   20,
		Confirm:    false,
	})
	require.Error(t, err)
}

// ── MaxUnits cap ────────────────────────────────────────────────────────────

func TestPlaceMarketOrder_MaxUnits_Caps(t *testing.T) {
	// Risk-based sizing: 100 000 NAV × 1% risk / (20 pip stop × 0.0001) = 500 000 units.
	// MaxUnits=5000 should reduce that to 5 000.
	acc, srv := newTestAccount(t, 100_000, 1.0850, 1.0852)
	defer srv.Close()

	result, err := acc.PlaceMarketOrder(t.Context(), PlaceMarketOrderRequest{
		Instrument: "EUR_USD",
		Side:       "long",
		RiskPct:    types.RateFromFloat(0.01),
		StopPips:   20,
		MaxUnits:   5000,
		Confirm:    false,
	})
	require.NoError(t, err)
	assert.EqualValues(t, 5000, result.Proposal.Units, "MaxUnits should cap long units")
}

func TestPlaceMarketOrder_MaxUnits_Short_Caps(t *testing.T) {
	acc, srv := newTestAccount(t, 100_000, 1.0850, 1.0852)
	defer srv.Close()

	result, err := acc.PlaceMarketOrder(t.Context(), PlaceMarketOrderRequest{
		Instrument: "EUR_USD",
		Side:       "short",
		RiskPct:    types.RateFromFloat(0.01),
		StopPips:   20,
		MaxUnits:   3000,
		Confirm:    false,
	})
	require.NoError(t, err)
	assert.EqualValues(t, -3000, result.Proposal.Units, "MaxUnits should cap short units (negative)")
}

func TestPlaceMarketOrder_MaxUnits_NoEffect_WhenBelowCap(t *testing.T) {
	// With 0.001% risk and a tight 20-pip stop, risk-based units will be tiny.
	acc, srv := newTestAccount(t, 100_000, 1.0850, 1.0852)
	defer srv.Close()

	result, err := acc.PlaceMarketOrder(t.Context(), PlaceMarketOrderRequest{
		Instrument: "EUR_USD",
		Side:       "long",
		RiskPct:    types.RateFromFloat(0.00001),
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
	acc, srv := newTestAccount(t, 100_000, 1.0850, 1.0852)
	defer srv.Close()

	result, err := acc.PlaceMarketOrder(t.Context(), PlaceMarketOrderRequest{
		Instrument:     "EUR_USD",
		Side:           "long",
		RiskPct:        types.RateFromFloat(0.01),
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
	acc, srv := newTestAccount(t, 100_000, 1.0850, 1.0852)
	defer srv.Close()

	result, err := acc.PlaceMarketOrder(t.Context(), PlaceMarketOrderRequest{
		Instrument:     "EUR_USD",
		Side:           "short",
		RiskPct:        types.RateFromFloat(0.01),
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
	acc, srv := newTestAccount(t, 100_000, 1.0850, 1.0852)
	defer srv.Close()

	result, err := acc.PlaceMarketOrder(t.Context(), PlaceMarketOrderRequest{
		Instrument:     "EUR_USD",
		Side:           "long",
		RiskPct:        types.RateFromFloat(0.01),
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
	acc, srv := newTestAccount(t, 100_000, 1.0850, 1.0852)
	defer srv.Close()

	result, err := acc.PlaceMarketOrder(t.Context(), PlaceMarketOrderRequest{
		Instrument: "EUR_USD",
		Side:       "long",
		RiskPct:    types.RateFromFloat(0.01),
		StopPips:   20,
		Confirm:    false,
	})
	require.NoError(t, err)
	assert.EqualValues(t, 500_000, result.Proposal.Units)
}

// ── Validation guards ───────────────────────────────────────────────────────

func TestPlaceMarketOrder_BadSide(t *testing.T) {
	acc, srv := newTestAccount(t, 100_000, 1.0850, 1.0852)
	defer srv.Close()

	_, err := acc.PlaceMarketOrder(t.Context(), PlaceMarketOrderRequest{
		Instrument: "EUR_USD",
		Side:       "sideways",
		RiskPct:    types.RateFromFloat(0.001),
		StopPips:   20,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "side must be")
}

func TestPlaceMarketOrder_NoStop(t *testing.T) {
	acc, srv := newTestAccount(t, 100_000, 1.0850, 1.0852)
	defer srv.Close()

	_, err := acc.PlaceMarketOrder(t.Context(), PlaceMarketOrderRequest{
		Instrument: "EUR_USD",
		Side:       "long",
		RiskPct:    types.RateFromFloat(0.001),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "StopPrice or StopPips")
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

	acc := NewSession("ACC1", &oanda.Client{BaseURL: srv.URL, Token: "tok", HTTP: srv.Client()}, nil)

	// 1% of $2,000 = $20 risk; 600-pip stop on USDJPY at 150
	// Expected: ~500 units ($20 / (6 JPY × 0.0067 USD/JPY))
	result, err := acc.PlaceMarketOrder(t.Context(), PlaceMarketOrderRequest{
		Instrument: "USD_JPY",
		Side:       "long",
		RiskPct:    types.RateFromFloat(0.01),
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
