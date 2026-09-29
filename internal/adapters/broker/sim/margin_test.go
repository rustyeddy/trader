package sim

import (
	"context"
	"testing"
	"time"

	"github.com/rustyeddy/trader/internal/account"
	"github.com/rustyeddy/trader/internal/clock"
	runtimeorder "github.com/rustyeddy/trader/internal/order"
	"github.com/rustyeddy/trader/internal/risk"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// marginAccount opens a $10,000 simulated account with the given
// initial-margin ratio ("" for no margin model).
func marginAccount(t *testing.T, ratio string) (*Broker, Deps, *accountHandle) {
	t.Helper()
	deps := testDeps()
	accountID := mustAccountID(t, deps.IDs)
	cfg := AccountConfig{AccountID: accountID, StartingCash: usd("10000")}
	if ratio != "" {
		r := num.MustParseRate(ratio)
		cfg.InitialMarginRatio = &r
	}
	b, err := NewBroker("sim", deps, cfg)
	require.NoError(t, err)
	acc, err := b.OpenAccount(context.Background(), accountID)
	require.NoError(t, err)
	return b, deps, acc.(*accountHandle)
}

func submitMarket(t *testing.T, deps Deps, h *accountHandle, symbol string, side order.Side, qty string) {
	t.Helper()
	listing := mustEurUsdListing(t)
	if symbol == "GBP_USD" {
		listing = mustGbpUsdListing(t)
	}
	_, err := h.Submit(context.Background(), mustMarketRequestFor(t, deps.IDs, h.Reference().AccountID, listing, side, qty))
	require.NoError(t, err)
}

func snapshot(t *testing.T, h *accountHandle) account.Snapshot {
	t.Helper()
	s, err := h.Snapshot(context.Background())
	require.NoError(t, err)
	return s
}

// assertMargin checks equity, margin used, margin available, and
// buying power, in that order.
func assertMargin(t *testing.T, s account.Snapshot, equity, used, available, buyingPower string) {
	t.Helper()
	assert.True(t, s.Equity().Equal(usd(equity)), "equity: want %s, got %s", equity, s.Equity())
	assert.True(t, s.MarginUsed().Equal(usd(used)), "margin used: want %s, got %s", used, s.MarginUsed())
	assert.True(t, s.MarginAvailable().Equal(usd(available)), "margin available: want %s, got %s", available, s.MarginAvailable())
	assert.True(t, s.BuyingPower().Equal(usd(buyingPower)), "buying power: want %s, got %s", buyingPower, s.BuyingPower())
}

func TestAccountConfig_InitialMarginRatioValidation(t *testing.T) {
	deps := testDeps()
	for _, bad := range []string{"0", "-0.5"} {
		r := num.MustParseRate(bad)
		_, err := NewBroker("sim", deps, AccountConfig{AccountID: mustAccountID(t, deps.IDs), StartingCash: usd("1"), InitialMarginRatio: &r})
		assert.ErrorIs(t, err, ErrInvalidConfig, bad)
	}
}

func TestAccountConfig_MarginModelInfo(t *testing.T) {
	assert.Equal(t, ModelInfo{Name: "none"}, AccountConfig{}.MarginModelInfo())

	half := num.MustParseRate("0.5")
	one := num.MustParseRate("1")
	infoHalf := AccountConfig{InitialMarginRatio: &half}.MarginModelInfo()
	assert.Equal(t, ModelInfo{Name: "initial-margin-ratio", Version: "v1", Config: "ratio=0.5"}, infoHalf)
	assert.NotEqual(t, infoHalf, AccountConfig{InitialMarginRatio: &one}.MarginModelInfo(), "different ratios are distinguishable")
}

func TestSnapshotMarginFields(t *testing.T) {
	t.Run("no margin model keeps legacy fields", func(t *testing.T) {
		_, deps, h := marginAccount(t, "")
		submitMarket(t, deps, h, "EUR_USD", order.Buy, "40000") // 44000 notional, 4.4× equity
		// Legacy: buying power and margin available mirror cash. The
		// simulator's cash moves only by realized PnL and fees, so it
		// still reports the full 10000 — the gap behind #409.
		assertMargin(t, snapshot(t, h), "10000", "0", "10000", "10000")
	})
	t.Run("flat at 1.0", func(t *testing.T) {
		_, _, h := marginAccount(t, "1")
		assertMargin(t, snapshot(t, h), "10000", "0", "10000", "10000")
	})
	t.Run("flat at 0.5", func(t *testing.T) {
		_, _, h := marginAccount(t, "0.5")
		assertMargin(t, snapshot(t, h), "10000", "0", "10000", "20000")
	})
	t.Run("long at 1.0", func(t *testing.T) {
		_, deps, h := marginAccount(t, "1")
		submitMarket(t, deps, h, "EUR_USD", order.Buy, "5000") // 5500 notional
		assertMargin(t, snapshot(t, h), "10000", "5500", "4500", "4500")
	})
	t.Run("long at 0.5", func(t *testing.T) {
		_, deps, h := marginAccount(t, "0.5")
		submitMarket(t, deps, h, "EUR_USD", order.Buy, "5000")
		assertMargin(t, snapshot(t, h), "10000", "2750", "7250", "14500")
	})
	t.Run("short at 1.0", func(t *testing.T) {
		_, deps, h := marginAccount(t, "1")
		submitMarket(t, deps, h, "GBP_USD", order.Sell, "4000") // 5000 notional
		assertMargin(t, snapshot(t, h), "10000", "5000", "5000", "5000")
	})
	t.Run("over the limit at entry", func(t *testing.T) {
		// #412 only reports; refusing the fill is #415. 44000 notional
		// at 1.0 leaves margin available at -34000 and no buying power.
		_, deps, h := marginAccount(t, "1")
		submitMarket(t, deps, h, "EUR_USD", order.Buy, "40000")
		assertMargin(t, snapshot(t, h), "10000", "44000", "-34000", "0")
	})
	t.Run("multi-instrument long and short is gross", func(t *testing.T) {
		_, deps, h := marginAccount(t, "1")
		submitMarket(t, deps, h, "EUR_USD", order.Buy, "5000")  // 5500
		submitMarket(t, deps, h, "GBP_USD", order.Sell, "2000") // 2500
		assertMargin(t, snapshot(t, h), "10000", "8000", "2000", "2000")
	})
	t.Run("multi-instrument at 0.5", func(t *testing.T) {
		_, deps, h := marginAccount(t, "0.5")
		submitMarket(t, deps, h, "EUR_USD", order.Buy, "5000")  // 5500
		submitMarket(t, deps, h, "GBP_USD", order.Sell, "2000") // 2500
		assertMargin(t, snapshot(t, h), "10000", "4000", "6000", "12000")
	})
}

func TestSnapshotOverLimitAfterAdverseMoveTakesNoAction(t *testing.T) {
	b, deps, h := marginAccount(t, "0.5")
	// 15000 EUR at 1.1 = 16500 notional, 8250 margin: within 2× of 10000.
	submitMarket(t, deps, h, "EUR_USD", order.Buy, "15000")
	assertMargin(t, snapshot(t, h), "10000", "8250", "1750", "3500")

	// Close at 0.6: equity 10000 − 7500 = 2500, gross 9000, margin 4500.
	require.NoError(t, deps.Clock.(*clock.Simulated).Advance(time.Hour))
	require.NoError(t, b.Advance(context.Background(), mustObservation(t, mustEurUsdListing(t), "0.60000", "0.60000", "0.60000", "0.60000", barTime)))

	s := snapshot(t, h)
	assertMargin(t, s, "2500", "4500", "-2000", "0")
	require.Len(t, s.Positions(), 1, "no forced liquidation in v1")
	assert.True(t, s.Positions()[0].Quantity.Equal(num.MustParseQuantity("15000")))
	assert.Empty(t, s.OpenOrders())
}

func TestSnapshotMarks(t *testing.T) {
	b, deps, h := marginAccount(t, "")
	eur, gbp := mustEurUsdListing(t), mustGbpUsdListing(t)

	submitMarket(t, deps, h, "GBP_USD", order.Buy, "100")
	submitMarket(t, deps, h, "EUR_USD", order.Buy, "100")

	t.Run("fill sets the mark at the fill price and time", func(t *testing.T) {
		s := snapshot(t, h)
		m, ok := s.Mark(account.KeyOf(eur))
		require.True(t, ok)
		assert.True(t, m.Price.Equal(num.MustParsePrice("1.10000")))
		assert.True(t, m.AsOf.Equal(testStart))
	})
	t.Run("marks follow positions order and are deterministic", func(t *testing.T) {
		s := snapshot(t, h)
		marks := s.Marks()
		require.Len(t, marks, 2)
		for i, p := range s.Positions() {
			assert.Equal(t, account.KeyOf(p.Listing), marks[i].Listing)
		}
		assert.Equal(t, marks, snapshot(t, h).Marks())
	})

	sim := deps.Clock.(*clock.Simulated)
	t.Run("Advance updates only the observed listing", func(t *testing.T) {
		require.NoError(t, sim.Advance(time.Hour))
		require.NoError(t, b.Advance(context.Background(), mustObservation(t, eur, "1.11000", "1.12000", "1.10000", "1.11500", barTime)))
		s := snapshot(t, h)
		m, _ := s.Mark(account.KeyOf(eur))
		assert.True(t, m.Price.Equal(num.MustParsePrice("1.11500")))
		assert.True(t, m.AsOf.Equal(testStart.Add(time.Hour)))
		g, _ := s.Mark(account.KeyOf(gbp))
		assert.True(t, g.AsOf.Equal(testStart), "GBP/USD was not observed, so its mark is older")
	})
	t.Run("ObserveMark updates the mark and its time", func(t *testing.T) {
		require.NoError(t, sim.Advance(time.Hour))
		require.NoError(t, h.ObserveMark(context.Background(), gbp.InstrumentID(), num.MustParsePrice("1.30000"), testStart.Add(2*time.Hour)))
		g, _ := snapshot(t, h).Mark(account.KeyOf(gbp))
		assert.True(t, g.Price.Equal(num.MustParsePrice("1.30000")))
		assert.True(t, g.AsOf.Equal(testStart.Add(2*time.Hour)))
	})
}

// TestFullNotionalSizerRespectsMarginRatio verifies that the
// full-notional sizer (ADR-061), which sizes from min(Equity,
// BuyingPower), respects the configured ratio through the simulator's
// margin-aware BuyingPower.
func TestFullNotionalSizerRespectsMarginRatio(t *testing.T) {
	ref := num.MustParsePrice("1.10000")
	size := func(t *testing.T, ratio string) num.Quantity {
		t.Helper()
		_, _, h := marginAccount(t, ratio)
		q, err := risk.NewFullNotionalSizer().Size(context.Background(), risk.SizeInput{
			Account:        snapshot(t, h),
			Listing:        mustEurUsdListing(t),
			ReferencePrice: &ref,
		})
		require.NoError(t, err)
		return q
	}
	// Ratio 1.0: buying power = equity = 10000 → 9090 units (9999 USD).
	assert.True(t, size(t, "1").Equal(num.MustParseQuantity("9090")))
	// Ratio 0.5 permits more buying power, but the sizer still caps at
	// equity: fully invested, unlevered.
	assert.True(t, size(t, "0.5").Equal(num.MustParseQuantity("9090")))
	// Ratio 2.0 halves buying power to 5000 → 4545 units.
	assert.True(t, size(t, "2").Equal(num.MustParseQuantity("4545")))
}

// TestZeroFillPriceLeavesAccountUnchanged: a zero fill price would
// become a mark that account.Snapshot rejects, so the fill must fail
// before any state is committed.
func TestZeroFillPriceLeavesAccountUnchanged(t *testing.T) {
	ctx := context.Background()
	deps := testDeps()
	prices := &mutablePriceSource{prices: map[string]num.Price{"EUR_USD": num.MustParsePrice("0")}}
	deps.Prices = prices
	accountID := mustAccountID(t, deps.IDs)
	b, err := NewBroker("sim", deps, AccountConfig{AccountID: accountID, StartingCash: usd("10000")})
	require.NoError(t, err)
	acc, err := b.OpenAccount(ctx, accountID)
	require.NoError(t, err)
	h := acc.(*accountHandle)
	before := snapshot(t, h)

	req := mustMarketRequest(t, deps.IDs, accountID, order.Buy, "100")
	_, err = acc.Submit(ctx, req)
	require.ErrorIs(t, err, runtimeorder.ErrInvalidFill)

	after := snapshot(t, h)
	assert.Empty(t, after.Positions())
	assert.Empty(t, after.Marks())
	assert.Empty(t, after.OpenOrders())
	assert.True(t, before.AsOf().Equal(after.AsOf()))
	assert.Empty(t, h.state.orders, "the order was never stored")

	// The same request can still fill once a valid price exists.
	prices.set("EUR_USD", num.MustParsePrice("1.10000"))
	_, err = acc.Submit(ctx, req)
	require.NoError(t, err)
	require.Len(t, snapshot(t, h).Positions(), 1)
}

func TestZeroObservationPricesRejectedBeforeMutation(t *testing.T) {
	ctx := context.Background()
	b, deps, h := marginAccount(t, "")
	submitMarket(t, deps, h, "EUR_USD", order.Buy, "100")
	before := snapshot(t, h)
	eur := mustEurUsdListing(t)

	t.Run("ObserveMark", func(t *testing.T) {
		err := h.ObserveMark(ctx, eur.InstrumentID(), num.MustParsePrice("0"), testStart)
		assert.ErrorIs(t, err, ErrInvalidObservation)
	})
	t.Run("Advance", func(t *testing.T) {
		obs := Observation{Listing: eur, Open: num.MustParsePrice("1"), High: num.MustParsePrice("1"), Low: num.MustParsePrice("0"), Close: num.MustParsePrice("0"), Time: barTime}
		assert.ErrorIs(t, b.Advance(ctx, obs), ErrInvalidObservation)
	})
	t.Run("AdvanceBar", func(t *testing.T) {
		zero, one := num.MustParsePrice("0"), num.MustParsePrice("1")
		assert.ErrorIs(t, h.AdvanceBar(ctx, eur, one, one, zero, zero, barTime), ErrInvalidObservation)
	})

	after := snapshot(t, h)
	assert.Equal(t, before.Marks(), after.Marks(), "marks unchanged")
	assert.True(t, before.AsOf().Equal(after.AsOf()), "as-of unchanged")
}
