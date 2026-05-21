package service

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rustyeddy/trader/brokers/oanda"
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
		RiskPct:    1.0,
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
		RiskPct:    1.0,
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
		RiskPct:    0.001,
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
		RiskPct:        1.0,
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
		RiskPct:        1.0,
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
		RiskPct:        1.0,
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
		RiskPct:    1.0,
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
		RiskPct:    0.1,
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
		RiskPct:    0.1,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "StopPrice or StopPips")
}
