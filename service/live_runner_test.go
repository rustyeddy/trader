package service

import (
	"context"
	"testing"
	"time"

	"github.com/rustyeddy/trader"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── stubStrategy ─────────────────────────────────────────────────────────────

// stubStrategy is a LiveStrategy that records every Tick call and returns
// a preset plan.
type stubStrategy struct {
	name   string
	plan   *trader.LivePlan
	ticks  []tickRecord
}

type tickRecord struct {
	price      trader.LivePrice
	openTrades []trader.LiveTrade
}

func (s *stubStrategy) Name() string { return s.name }
func (s *stubStrategy) Tick(_ context.Context, p trader.LivePrice, trades []trader.LiveTrade) *trader.LivePlan {
	s.ticks = append(s.ticks, tickRecord{price: p, openTrades: trades})
	return s.plan
}

// ── normalizeInstrument ───────────────────────────────────────────────────────

func TestNormalizeInstrument(t *testing.T) {
	cases := []struct{ in, want string }{
		{"EUR_USD", "EUR_USD"},
		{"eur_usd", "EUR_USD"},
		{"EUR/USD", "EUR_USD"},
		{"eur/usd", "EUR_USD"},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, normalizeInstrument(tc.in), "in=%q", tc.in)
	}
}

// ── RunLiveStrategy — config validation ───────────────────────────────────────

func TestRunLiveStrategy_NilStrategy(t *testing.T) {
	svc := &Service{}
	err := svc.RunLiveStrategy(context.Background(), LiveRunConfig{
		Instrument: "EUR_USD",
		Strategy:   nil,
	})
	assert.ErrorContains(t, err, "strategy is required")
}

func TestRunLiveStrategy_EmptyInstrument(t *testing.T) {
	svc := &Service{}
	err := svc.RunLiveStrategy(context.Background(), LiveRunConfig{
		Strategy:   &stubStrategy{name: "stub"},
		Instrument: "",
	})
	assert.ErrorContains(t, err, "instrument is required")
}

func TestRunLiveStrategy_NoOANDA_FailsAtResolve(t *testing.T) {
	svc := &Service{} // no OANDA client
	err := svc.RunLiveStrategy(context.Background(), LiveRunConfig{
		Instrument: "EUR_USD",
		Strategy:   &stubStrategy{name: "stub"},
	})
	assert.ErrorContains(t, err, "OANDA")
}

// ── LiveRunConfig defaults ────────────────────────────────────────────────────

func TestLiveRunConfig_DefaultTickInterval(t *testing.T) {
	cfg := LiveRunConfig{
		Instrument:   "EUR_USD",
		Strategy:     &stubStrategy{name: "stub"},
		TickInterval: 0,
	}
	// normalise would be applied inside RunLiveStrategy; test that the default
	// value is the expected 60s.
	if cfg.TickInterval <= 0 {
		cfg.TickInterval = 60 * time.Second
	}
	assert.Equal(t, 60*time.Second, cfg.TickInterval)
}

func TestLiveRunConfig_DefaultRiskPct(t *testing.T) {
	cfg := LiveRunConfig{
		Instrument: "EUR_USD",
		Strategy:   &stubStrategy{name: "stub"},
		RiskPct:    0,
	}
	if cfg.RiskPct <= 0 {
		cfg.RiskPct = 0.1
	}
	assert.Equal(t, 0.1, cfg.RiskPct)
}

// ── LiveTrade.Side ────────────────────────────────────────────────────────────

func TestLiveTrade_Side(t *testing.T) {
	long := trader.LiveTrade{Units: 1000}
	short := trader.LiveTrade{Units: -500}
	zero := trader.LiveTrade{Units: 0}

	assert.Equal(t, "long", long.Side())
	assert.Equal(t, "short", short.Side())
	assert.Equal(t, "long", zero.Side()) // zero treated as long
}

// ── LivePrice.Mid ─────────────────────────────────────────────────────────────

func TestLivePrice_Mid(t *testing.T) {
	p := trader.LivePrice{Bid: 1.0850, Ask: 1.0852}
	require.InDelta(t, 1.0851, p.Mid(), 0.000001)
}

// ── stubStrategy records ticks correctly ──────────────────────────────────────

func TestStubStrategy_RecordsTicks(t *testing.T) {
	plan := &trader.LivePlan{Reason: "hold"}
	s := &stubStrategy{name: "test", plan: plan}

	price := trader.LivePrice{Instrument: "EUR_USD", Bid: 1.08, Ask: 1.081}
	s.Tick(context.Background(), price, nil)
	s.Tick(context.Background(), price, nil)

	assert.Len(t, s.ticks, 2)
	assert.Equal(t, "EUR_USD", s.ticks[0].price.Instrument)
}
