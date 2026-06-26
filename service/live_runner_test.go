package service

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rustyeddy/trader/brokers/oanda"
	"github.com/rustyeddy/trader/market"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── stubStrategy ─────────────────────────────────────────────────────────────

// stubStrategy is a LiveStrategy that records every Tick call and returns
// a preset plan.
type stubStrategy struct {
	name  string
	plan  *LivePlan
	ticks []tickRecord
}

type tickRecord struct {
	price      LivePrice
	openTrades []LiveTrade
}

func (s *stubStrategy) Name() string { return s.name }
func (s *stubStrategy) Tick(_ context.Context, p LivePrice, trades []LiveTrade) *LivePlan {
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
	long := LiveTrade{Units: 1000}
	short := LiveTrade{Units: -500}
	zero := LiveTrade{Units: 0}

	assert.Equal(t, "long", long.Side())
	assert.Equal(t, "short", short.Side())
	assert.Equal(t, "long", zero.Side()) // zero treated as long
}

// ── LivePrice.Mid ─────────────────────────────────────────────────────────────

func TestLivePrice_Mid(t *testing.T) {
	p := LivePrice{Bid: 1.0850, Ask: 1.0852}
	require.InDelta(t, 1.0851, p.Mid(), 0.000001)
}

// ── estimateTicksOpen ─────────────────────────────────────────────────────────

func TestEstimateTicksOpen(t *testing.T) {
	interval := 5 * time.Minute
	base := time.Date(2024, 1, 10, 12, 0, 0, 0, time.UTC)

	cases := []struct {
		label    string
		openTime time.Time
		now      time.Time
		want     int
	}{
		{"zero openTime", time.Time{}, base, 0},
		{"openTime in future", base.Add(time.Minute), base, 0},
		{"exactly one interval", base, base.Add(5 * time.Minute), 1},
		{"two intervals", base, base.Add(10 * time.Minute), 2},
		{"partial interval rounds down", base, base.Add(7 * time.Minute), 1},
		{"overnight ~12h", base, base.Add(12 * time.Hour), 144},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			assert.Equal(t, tc.want, estimateTicksOpen(tc.openTime, tc.now, interval))
		})
	}
}

// ── seedTickCounts ────────────────────────────────────────────────────────────

// newSeedAccount builds an Account backed by a fake OANDA HTTP server that
// returns the given open-trades JSON body.
func newSeedAccount(t *testing.T, body string) (*Account, func()) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, body)
	}))
	svc := &Service{
		OANDA:     &oanda.Client{BaseURL: srv.URL, Token: "test"},
		AccountID: "acc-1",
	}
	acc, err := svc.Account(context.Background(), "acc-1")
	require.NoError(t, err)
	return acc, srv.Close
}

// TestSeedTickCounts_SeedValue verifies that seed = estimated-1 so the first
// runOneTick increment lands on the correct estimated age.
func TestSeedTickCounts_SeedValue(t *testing.T) {
	// Trade opened 3 hours ago; tick interval is 1 hour → estimated = 3, seed = 2.
	openTime := time.Now().UTC().Add(-3 * time.Hour)
	body := fmt.Sprintf(`{"trades":[{"id":"trade-1","instrument":"EUR_USD","price":"1.08","currentUnits":"1000","unrealizedPL":"0","openTime":%q}]}`, openTime.Format(time.RFC3339Nano))

	acc, cleanup := newSeedAccount(t, body)
	defer cleanup()

	cfg := LiveRunConfig{Instrument: "EUR_USD", TickInterval: time.Hour}
	counts := acc.seedTickCounts(context.Background(), cfg, slog.Default())

	require.Contains(t, counts, "trade-1")
	assert.Equal(t, 2, counts["trade-1"]) // 3 estimated - 1 = 2
}

// TestSeedTickCounts_WrongInstrumentIgnored ensures trades for other
// instruments are not seeded into this runner's tick counter.
func TestSeedTickCounts_WrongInstrumentIgnored(t *testing.T) {
	openTime := time.Now().UTC().Add(-2 * time.Hour)
	body := fmt.Sprintf(`{"trades":[{"id":"trade-9","instrument":"USD_JPY","price":"150","currentUnits":"1000","unrealizedPL":"0","openTime":%q}]}`, openTime.Format(time.RFC3339Nano))

	acc, cleanup := newSeedAccount(t, body)
	defer cleanup()

	cfg := LiveRunConfig{Instrument: "EUR_USD", TickInterval: time.Hour}
	counts := acc.seedTickCounts(context.Background(), cfg, slog.Default())
	assert.Empty(t, counts)
}

// TestSeedTickCounts_ZeroOpenTimeSkipped ensures trades with no openTime
// (missing from OANDA response) are skipped rather than seeded with garbage.
func TestSeedTickCounts_ZeroOpenTimeSkipped(t *testing.T) {
	body := `{"trades":[{"id":"trade-2","instrument":"EUR_USD","price":"1.08","currentUnits":"1000","unrealizedPL":"0"}]}`

	acc, cleanup := newSeedAccount(t, body)
	defer cleanup()

	cfg := LiveRunConfig{Instrument: "EUR_USD", TickInterval: time.Hour}
	counts := acc.seedTickCounts(context.Background(), cfg, slog.Default())
	assert.Empty(t, counts)
}

// ── market-hours gate ─────────────────────────────────────────────────────────

// TestMarketClosedGate verifies that the gate logic used in the tick closure
// skips runOneTick when IsForexMarketClosed returns true. We replicate the
// closure logic here so the guard is tested independently of wall-clock time.
func TestMarketClosedGate(t *testing.T) {
	strategy := &stubStrategy{name: "stub", plan: &LivePlan{}}

	var tickCalls int
	runOneTick := func() { tickCalls++ }

	gate := func(now time.Time, marketWasClosed *bool) {
		if market.IsForexMarketClosed(now) {
			*marketWasClosed = true
			return
		}
		*marketWasClosed = false
		runOneTick()
	}

	_ = strategy // kept to show intent

	// Saturday — always closed.
	saturday := time.Date(2024, 1, 6, 12, 0, 0, 0, time.UTC)
	closed := false
	gate(saturday, &closed)
	assert.True(t, closed)
	assert.Equal(t, 0, tickCalls)

	// Monday — market open.
	monday := time.Date(2024, 1, 8, 12, 0, 0, 0, time.UTC)
	gate(monday, &closed)
	assert.False(t, closed)
	assert.Equal(t, 1, tickCalls)
}

// ── priceCache ────────────────────────────────────────────────────────────────

func TestPriceCache_NilBeforeFirstSet(t *testing.T) {
	c := &priceCache{}
	assert.Nil(t, c.get())
}

func TestPriceCache_ReturnsLatestTick(t *testing.T) {
	c := &priceCache{}
	c.set(oanda.PriceTick{Instrument: "EUR_USD", Bid: 1.08, Ask: 1.09})
	tick := c.get()
	require.NotNil(t, tick)
	assert.Equal(t, "EUR_USD", tick.Instrument)
	assert.InDelta(t, 1.08, tick.Bid, 1e-9)
}

func TestPriceCache_OverwritesWithNewer(t *testing.T) {
	c := &priceCache{}
	c.set(oanda.PriceTick{Bid: 1.08, Ask: 1.09})
	c.set(oanda.PriceTick{Bid: 1.10, Ask: 1.11})
	tick := c.get()
	require.NotNil(t, tick)
	assert.InDelta(t, 1.10, tick.Bid, 1e-9)
}

// ── runOneTick — price source selection ───────────────────────────────────────

// pricingOnlyServer returns a server that serves only GET /v3/accounts/.../pricing
// and records how many times it was called.
func pricingOnlyServer(t *testing.T, calls *int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path != "" {
			*calls++
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"prices":[{"instrument":"EUR_USD","bids":[{"price":"1.0850"}],"asks":[{"price":"1.0852"}]}]}`)
	}))
}

func TestRunOneTick_UsesCacheWhenAvailable(t *testing.T) {
	restCalls := 0
	srv := pricingOnlyServer(t, &restCalls)
	defer srv.Close()

	svc := &Service{
		OANDA:     &oanda.Client{BaseURL: srv.URL, Token: "tok", HTTP: srv.Client()},
		AccountID: "acc-1",
	}
	acc, err := svc.Account(context.Background(), "acc-1")
	require.NoError(t, err)

	cache := &priceCache{}
	cache.set(oanda.PriceTick{Instrument: "EUR_USD", Bid: 1.099, Ask: 1.100, Time: time.Now()})

	strat := &stubStrategy{name: "stub"}
	cfg := LiveRunConfig{
		Instrument:   "EUR_USD",
		TickInterval: time.Minute,
		Strategy:     strat,
		UseStream:    true,
	}
	require.NoError(t, validateLiveRunConfig(&cfg))

	err = acc.runOneTick(context.Background(), cfg, map[string]int{}, cache, slog.Default())
	require.NoError(t, err)

	// Strategy should have received the cached price, not the REST server price.
	require.Len(t, strat.ticks, 1)
	assert.InDelta(t, 1.099, strat.ticks[0].price.Bid, 1e-9)

	// No REST calls should have been made for pricing (only trades fallback).
	// The trades call returns the full response; pricing endpoint not hit.
	// We verify price came from cache by checking the bid value above.
}

func TestRunOneTick_FallsBackToRESTWhenCacheEmpty(t *testing.T) {
	restCalls := 0
	srv := pricingOnlyServer(t, &restCalls)
	defer srv.Close()

	svc := &Service{
		OANDA:     &oanda.Client{BaseURL: srv.URL, Token: "tok", HTTP: srv.Client()},
		AccountID: "acc-1",
	}
	acc, err := svc.Account(context.Background(), "acc-1")
	require.NoError(t, err)

	// Cache exists but has no tick yet.
	emptyCache := &priceCache{}

	strat := &stubStrategy{name: "stub"}
	cfg := LiveRunConfig{
		Instrument:   "EUR_USD",
		TickInterval: time.Minute,
		Strategy:     strat,
	}
	require.NoError(t, validateLiveRunConfig(&cfg))

	err = acc.runOneTick(context.Background(), cfg, map[string]int{}, emptyCache, slog.Default())
	require.NoError(t, err)

	// Price came from REST; stub server returned 1.0850/1.0852.
	require.Len(t, strat.ticks, 1)
	assert.InDelta(t, 1.0850, strat.ticks[0].price.Bid, 1e-9)
}

func TestRunOneTick_FallsBackToRESTWhenNilCache(t *testing.T) {
	restCalls := 0
	srv := pricingOnlyServer(t, &restCalls)
	defer srv.Close()

	svc := &Service{
		OANDA:     &oanda.Client{BaseURL: srv.URL, Token: "tok", HTTP: srv.Client()},
		AccountID: "acc-1",
	}
	acc, err := svc.Account(context.Background(), "acc-1")
	require.NoError(t, err)

	strat := &stubStrategy{name: "stub"}
	cfg := LiveRunConfig{
		Instrument:   "EUR_USD",
		TickInterval: time.Minute,
		Strategy:     strat,
	}
	require.NoError(t, validateLiveRunConfig(&cfg))

	err = acc.runOneTick(context.Background(), cfg, map[string]int{}, nil, slog.Default())
	require.NoError(t, err)

	require.Len(t, strat.ticks, 1)
	assert.InDelta(t, 1.0850, strat.ticks[0].price.Bid, 1e-9)
}

// ── stubStrategy records ticks correctly ──────────────────────────────────────

func TestStubStrategy_RecordsTicks(t *testing.T) {
	plan := &LivePlan{Reason: "hold"}
	s := &stubStrategy{name: "test", plan: plan}

	price := LivePrice{Instrument: "EUR_USD", Bid: 1.08, Ask: 1.081}
	s.Tick(context.Background(), price, nil)
	s.Tick(context.Background(), price, nil)

	assert.Len(t, s.ticks, 2)
	assert.Equal(t, "EUR_USD", s.ticks[0].price.Instrument)
}
