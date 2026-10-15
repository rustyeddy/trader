package tradehistory

import (
	"math/rand"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/internal/accountactivity"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
)

var (
	usd   = num.MustParseCurrency("USD")
	t0    = time.Date(2024, 3, 5, 9, 0, 0, 0, time.UTC)
	eurus = instrument.CurrencyPairID(num.MustParseCurrency("EUR"), usd)
	gbpus = instrument.CurrencyPairID(num.MustParseCurrency("GBP"), usd)
)

func money(s string) num.Money { return num.MustParseMoney(s, usd) }
func rep(s string) accountactivity.Value[num.Money] {
	return accountactivity.Reported(money(s))
}

// spec describes one fill; empty strings leave a component unknown.
type spec struct {
	id        string
	at        time.Duration
	inst      instrument.ID
	side      order.Side
	qty       string
	price     string
	pl, fin   string
	spread    string
	comm, fee string
	effects   []accountactivity.TradeEffect
}

func fillOf(s spec) accountactivity.Activity {
	if s.inst.IsZero() {
		s.inst = eurus
	}
	a := accountactivity.Activity{
		Provider: "oanda", Environment: accountactivity.EnvironmentPractice, ProviderAccountID: "acct",
		ProviderActivityID: s.id, ProviderType: "ORDER_FILL", Kind: accountactivity.KindFill,
		OccurredAt: t0.Add(s.at), ImportedAt: t0.Add(24 * time.Hour),
		Instrument: s.inst, Side: s.side,
		Quantity:     accountactivity.Reported(num.MustParseQuantity(s.qty)),
		TradeEffects: s.effects,
	}
	if s.price != "" {
		a.Price = accountactivity.Reported(num.MustParsePrice(s.price))
	}
	for _, f := range []struct {
		dst *accountactivity.Value[num.Money]
		src string
	}{{&a.RealizedPL, s.pl}, {&a.Financing, s.fin}, {&a.SpreadCost, s.spread}, {&a.Commission, s.comm}, {&a.OtherFees, s.fee}} {
		if f.src != "" {
			*f.dst = rep(f.src)
		}
	}
	return a
}

func buy(id string, at time.Duration, qty, price string) spec {
	return spec{id: id, at: at, side: order.Buy, qty: qty, price: price}
}
func sell(id string, at time.Duration, qty, price string) spec {
	return spec{id: id, at: at, side: order.Sell, qty: qty, price: price}
}

func acts(ss ...spec) []accountactivity.Activity {
	out := make([]accountactivity.Activity, len(ss))
	for i, s := range ss {
		out[i] = fillOf(s)
	}
	return out
}

func mustRecon(t *testing.T, as []accountactivity.Activity) Result {
	t.Helper()
	r, err := Reconstruct(as)
	require.NoError(t, err)
	return r
}

func mstr(t *testing.T, v accountactivity.Value[num.Money]) string {
	t.Helper()
	m, ok := v.Get()
	require.True(t, ok, "value is unknown")
	return m.String()
}

func pstr(t *testing.T, v accountactivity.Value[num.Price]) string {
	t.Helper()
	p, ok := v.Get()
	require.True(t, ok, "price is unknown")
	return p.String()
}

func keys(ids ...string) []accountactivity.Key {
	var out []accountactivity.Key
	for _, id := range ids {
		out = append(out, accountactivity.Key{Provider: "oanda", Environment: accountactivity.EnvironmentPractice, ProviderAccountID: "acct", ProviderActivityID: id})
	}
	return out
}

func complete(sp spec) spec {
	if sp.pl == "" {
		sp.pl = "0"
	}
	if sp.fin == "" {
		sp.fin = "0"
	}
	if sp.comm == "" {
		sp.comm = "0"
	}
	if sp.fee == "" {
		sp.fee = "0"
	}
	if sp.spread == "" {
		sp.spread = "0"
	}
	return sp
}

func TestSimpleRoundTrip(t *testing.T) {
	in := acts(
		complete(spec{id: "1", at: 0, side: order.Buy, qty: "1000", price: "1.1000", comm: "-0.05", spread: "-0.25"}),
		complete(spec{id: "2", at: time.Hour, side: order.Sell, qty: "1000", price: "1.1100", pl: "10", fin: "-0.10", comm: "-0.05", spread: "-0.30"}),
	)
	r := mustRecon(t, in)
	require.Len(t, r.Completed, 1)
	require.Empty(t, r.Open)
	tr := r.Completed[0]

	assert.Equal(t, order.Buy, tr.Side)
	assert.Equal(t, "fx:EUR/USD", tr.Instrument.String())
	assert.Equal(t, t0, tr.OpenedAt)
	assert.Equal(t, t0.Add(time.Hour), tr.ClosedAt)
	assert.Equal(t, "1000", tr.EntryQuantity.String())
	assert.Equal(t, "1000", tr.ExitQuantity.String())
	assert.Equal(t, "1.1", pstr(t, tr.AvgEntryPrice))
	assert.Equal(t, "1.11", pstr(t, tr.AvgExitPrice))
	assert.Equal(t, "10 USD", mstr(t, tr.GrossPL))
	assert.Equal(t, "-0.55 USD", mstr(t, tr.SpreadCost))
	assert.Equal(t, "-0.1 USD", mstr(t, tr.Commission))
	assert.Equal(t, "-0.1 USD", mstr(t, tr.Financing))
	assert.Equal(t, "0 USD", mstr(t, tr.OtherFees))
	assert.Equal(t, "9.8 USD", mstr(t, tr.Net), "10 - 0.1 - 0.1 + 0; spread cost is not subtracted again")
	assert.Equal(t, keys("1", "2"), tr.Sources)
	assert.Equal(t, "oanda/practice/acct/1#long", tr.ID)
}

// The user-facing example from the design discussion: spread cost is shown
// in the breakdown and already included in Gross P/L, never added to Net.
func TestSpreadCostIsShownButNotSubtractedTwice(t *testing.T) {
	in := acts(
		complete(spec{id: "1", at: 0, side: order.Buy, qty: "1000", price: "1.1", spread: "-4.20", comm: "-1.00"}),
		complete(spec{id: "2", at: time.Hour, side: order.Sell, qty: "1000", price: "1.2", pl: "125", fin: "-1.35", spread: "-4.20", comm: "-1.00"}),
	)
	tr := mustRecon(t, in).Completed[0]
	assert.Equal(t, "125 USD", mstr(t, tr.GrossPL))
	assert.Equal(t, "-8.4 USD", mstr(t, tr.SpreadCost))
	assert.Equal(t, "-2 USD", mstr(t, tr.Commission))
	assert.Equal(t, "-1.35 USD", mstr(t, tr.Financing))
	assert.Equal(t, "0 USD", mstr(t, tr.OtherFees))
	assert.Equal(t, "121.65 USD", mstr(t, tr.Net))
	assert.Equal(t, accountactivity.BasisDerived, tr.Net.Basis())
	assert.Contains(t, tr.Net.Source(), "spread cost already embedded in realized P/L")
}

func TestScaleInAndScaleOutAreOneTrade(t *testing.T) {
	in := acts(
		complete(buy("1", 0, "400", "1.10")),
		complete(buy("2", time.Hour, "600", "1.12")),
		complete(spec{id: "3", at: 2 * time.Hour, side: order.Sell, qty: "300", price: "1.13", pl: "9"}),
		complete(spec{id: "4", at: 3 * time.Hour, side: order.Sell, qty: "700", price: "1.15", pl: "31.5"}),
	)
	r := mustRecon(t, in)
	require.Len(t, r.Completed, 1, "scale-in and scale-out stay one trade")
	tr := r.Completed[0]
	require.Len(t, tr.Entries, 2)
	require.Len(t, tr.Exits, 2)
	assert.Equal(t, "1000", tr.EntryQuantity.String())
	assert.Equal(t, "1.112", pstr(t, tr.AvgEntryPrice), "(400*1.10 + 600*1.12) / 1000")
	assert.Equal(t, "1.144", pstr(t, tr.AvgExitPrice), "(300*1.13 + 700*1.15) / 1000")
	assert.Equal(t, "40.5 USD", mstr(t, tr.GrossPL))
	assert.Equal(t, keys("1", "2", "3", "4"), tr.Sources)
}

func TestShortTradeAndInstrumentsAreIndependent(t *testing.T) {
	g := func(s spec) spec { s.inst = gbpus; return complete(s) }
	in := acts(
		complete(sell("1", 0, "500", "1.10")),
		g(buy("2", time.Minute, "200", "1.30")),
		complete(spec{id: "3", at: time.Hour, side: order.Buy, qty: "500", price: "1.09", pl: "5"}),
		g(spec{id: "4", at: 2 * time.Hour, side: order.Sell, qty: "200", price: "1.31", pl: "2"}),
	)
	r := mustRecon(t, in)
	require.Len(t, r.Completed, 2)
	byInst := map[string]Trade{}
	for _, tr := range r.Completed {
		byInst[tr.Instrument.String()] = tr
	}
	short := byInst["fx:EUR/USD"]
	assert.Equal(t, order.Sell, short.Side, "entries were sells")
	assert.Equal(t, "5 USD", mstr(t, short.GrossPL))
	assert.Equal(t, "oanda/practice/acct/1#short", short.ID)
	long := byInst["fx:GBP/USD"]
	assert.Equal(t, order.Buy, long.Side)
	assert.Equal(t, "2 USD", mstr(t, long.GrossPL))
}

func TestOpenRemainderIsReportedNotForceClosed(t *testing.T) {
	in := acts(
		complete(buy("1", 0, "1000", "1.10")),
		complete(spec{id: "2", at: time.Hour, side: order.Sell, qty: "400", price: "1.12", pl: "8"}),
	)
	r := mustRecon(t, in)
	assert.Empty(t, r.Completed)
	require.Len(t, r.Open, 1)
	o := r.Open[0]
	assert.Equal(t, "600", o.Remaining.String())
	require.Len(t, o.Exits, 1)
	assert.Equal(t, "400", o.Exits[0].Quantity.String())
	assert.Equal(t, "1.1", pstr(t, o.AvgEntryPrice))
	assert.Equal(t, keys("1", "2"), o.Sources)
}

// A fill that closes a long and opens a short contributes to two trades.
// Without per-trade units the split follows the net position; realized P/L
// and financing belong to the closed trade, and the execution costs are
// divided by units and say so.
func TestReversalWithoutBrokerEffectsSplitsByNetPosition(t *testing.T) {
	in := acts(
		complete(buy("1", 0, "1000", "1.10")),
		complete(spec{id: "2", at: time.Hour, side: order.Sell, qty: "1500", price: "1.12", pl: "20", fin: "-0.30", comm: "-0.15", spread: "-0.30", fee: "-0.60"}),
		complete(spec{id: "3", at: 2 * time.Hour, side: order.Buy, qty: "500", price: "1.10", pl: "10"}),
	)
	r := mustRecon(t, in)
	require.Len(t, r.Completed, 2)
	require.Empty(t, r.Open)

	long, short := r.Completed[0], r.Completed[1]
	assert.Equal(t, order.Buy, long.Side)
	assert.Equal(t, "1000", long.ExitQuantity.String(), "only the closing 1000 of the 1500")
	assert.Equal(t, "20 USD", mstr(t, long.GrossPL), "realized P/L belongs to the trade that was closed")
	assert.Equal(t, "-0.3 USD", mstr(t, long.Financing))
	assert.Equal(t, "-0.1 USD", mstr(t, long.Commission), "1000/1500 of -0.15, apportioned")
	assert.Equal(t, "-0.2 USD", mstr(t, long.SpreadCost))
	assert.Equal(t, "-0.4 USD", mstr(t, long.OtherFees))
	assert.Contains(t, long.Commission.Source(), "units-proportional shares from fills oanda/practice/acct/2")
	assert.True(t, long.Exits[0].Split, "the division came from the net position, not the broker")

	assert.Equal(t, order.Sell, short.Side)
	assert.Equal(t, "500", short.EntryQuantity.String())
	assert.Equal(t, "oanda/practice/acct/2#short", short.ID)
	assert.Equal(t, "-0.05 USD", mstr(t, short.Commission), "the remaining 500/1500, so the shares sum exactly to the fill")
	assert.Equal(t, "-0.1 USD", mstr(t, short.SpreadCost))
	assert.Equal(t, "-0.2 USD", mstr(t, short.OtherFees))
	assert.Equal(t, "10 USD", mstr(t, short.GrossPL), "only its own closing fill's P/L; the reversal fill's went to the long")
	assert.Equal(t, "0 USD", mstr(t, short.Financing), "the reversal fill's financing went to the long")
}

func eff(id string, role accountactivity.TradeRole, units, price string) accountactivity.TradeEffect {
	e := accountactivity.TradeEffect{BrokerTradeID: id, Role: role}
	e.Units = accountactivity.Reported(num.MustParseQuantity(units))
	if price != "" {
		e.Price = accountactivity.Reported(num.MustParsePrice(price))
	}
	return e
}

// With broker per-trade effects the split is the broker's, the legs carry
// each trade's exact price, and per-trade spread and fee values are reported
// exactly instead of apportioned. Commission has no per-trade value and is
// still apportioned.
func TestReversalWithBrokerEffectsUsesTheBrokersFactsWhereTheyExist(t *testing.T) {
	open := eff("t7", accountactivity.TradeRoleOpened, "500", "1.1210")
	open.SpreadCost = accountactivity.Reported(money("-0.10"))
	open.GuaranteedExecutionFee = accountactivity.Reported(money("-0.25"))
	closeE := eff("t1", accountactivity.TradeRoleClosed, "1000", "1.1200")
	closeE.SpreadCost = accountactivity.Reported(money("-0.20"))
	closeE.GuaranteedExecutionFee = accountactivity.Reported(money("-0.35"))

	rev := complete(spec{id: "2", at: time.Hour, side: order.Sell, qty: "1500", price: "1.1205",
		pl: "20", fin: "-0.30", comm: "-0.15", spread: "-0.30", fee: "-0.60",
		effects: []accountactivity.TradeEffect{open, closeE}})
	in := acts(
		complete(spec{id: "1", at: 0, side: order.Buy, qty: "1000", price: "1.10",
			effects: []accountactivity.TradeEffect{eff("t1", accountactivity.TradeRoleOpened, "1000", "1.10")}}),
		rev,
		complete(spec{id: "3", at: 2 * time.Hour, side: order.Buy, qty: "500", price: "1.1100", pl: "5",
			effects: []accountactivity.TradeEffect{eff("t7", accountactivity.TradeRoleClosed, "500", "1.11")}}),
	)
	r := mustRecon(t, in)
	require.Len(t, r.Completed, 2)
	long, short := r.Completed[0], r.Completed[1]

	// Legs come from the broker's effects, with their own prices and IDs.
	require.Len(t, long.Exits, 1)
	assert.Equal(t, "t1", long.Exits[0].BrokerTradeID)
	assert.Equal(t, "1.12", pstr(t, long.Exits[0].Price), "the per-trade price, not the fill-level 1.1205")
	assert.False(t, long.Exits[0].Split, "the division is the broker's")
	assert.Equal(t, "1.12", pstr(t, long.AvgExitPrice))
	require.Len(t, short.Entries, 1)
	assert.Equal(t, "t7", short.Entries[0].BrokerTradeID)
	assert.Equal(t, "1.121", pstr(t, short.AvgEntryPrice))

	// Per-trade values are exact; commission, which has none, is apportioned.
	assert.Equal(t, "-0.2 USD", mstr(t, long.SpreadCost))
	assert.NotContains(t, long.SpreadCost.Source(), "units-proportional")
	assert.Equal(t, "-0.35 USD", mstr(t, long.OtherFees))
	assert.Equal(t, "-0.1 USD", mstr(t, short.SpreadCost))
	assert.Equal(t, "-0.25 USD", mstr(t, short.OtherFees))
	assert.Equal(t, "-0.1 USD", mstr(t, long.Commission))
	assert.Contains(t, long.Commission.Source(), "units-proportional")
	assert.Equal(t, "-0.05 USD", mstr(t, short.Commission))
}

// If per-trade values are missing and there is no defensible basis, a
// component stays unknown rather than being invented.
func TestSplitFillWithNoFillLevelCostLeavesTheComponentUnknown(t *testing.T) {
	in := acts(
		complete(buy("1", 0, "1000", "1.10")),
		spec{id: "2", at: time.Hour, side: order.Sell, qty: "1500", price: "1.12", pl: "20", fin: "0", comm: "", spread: "", fee: "0"},
		complete(spec{id: "3", at: 2 * time.Hour, side: order.Buy, qty: "500", price: "1.10", pl: "10"}),
	)
	r := mustRecon(t, in)
	require.Len(t, r.Completed, 2)
	for _, tr := range r.Completed {
		assert.False(t, tr.Commission.Known(), "no commission was reported on the reversal fill")
		assert.False(t, tr.SpreadCost.Known())
		assert.False(t, tr.Net.Known(), "Net needs commission")
		assert.True(t, tr.GrossPL.Known())
	}
}

// A hedging account can hold a long and a short in the same instrument at
// once; the broker's opened effect says a sell opens a short beside the long.
func TestHedgedPositionsAreSeparateTrades(t *testing.T) {
	hedgeSell := complete(spec{id: "2", at: time.Hour, side: order.Sell, qty: "500", price: "1.11",
		effects: []accountactivity.TradeEffect{eff("t2", accountactivity.TradeRoleOpened, "500", "1.11")}})
	in := acts(
		complete(spec{id: "1", at: 0, side: order.Buy, qty: "1000", price: "1.10",
			effects: []accountactivity.TradeEffect{eff("t1", accountactivity.TradeRoleOpened, "1000", "1.10")}}),
		hedgeSell,
		complete(spec{id: "3", at: 2 * time.Hour, side: order.Sell, qty: "1000", price: "1.13", pl: "30",
			effects: []accountactivity.TradeEffect{eff("t1", accountactivity.TradeRoleClosed, "1000", "1.13")}}),
		complete(spec{id: "4", at: 3 * time.Hour, side: order.Buy, qty: "500", price: "1.12", pl: "-5",
			effects: []accountactivity.TradeEffect{eff("t2", accountactivity.TradeRoleClosed, "500", "1.12")}}),
	)
	r := mustRecon(t, in)
	require.Len(t, r.Completed, 2, "the short did not net against the long")
	assert.Equal(t, order.Buy, r.Completed[0].Side)
	assert.Equal(t, "30 USD", mstr(t, r.Completed[0].GrossPL))
	assert.Equal(t, order.Sell, r.Completed[1].Side)
	assert.Equal(t, "-5 USD", mstr(t, r.Completed[1].GrossPL))
}

// Unknown stays unknown: a component total is known only if every fill that
// contributes has it, a reported zero is known, and an unknown SpreadCost
// does not make Net unknown because it is not part of Net.
func TestUnknownPropagation(t *testing.T) {
	// No spread cost reported at all; everything Net needs is reported.
	a := complete(buy("1", 0, "1000", "1.10"))
	a.spread = ""
	b := complete(spec{id: "2", at: time.Hour, side: order.Sell, qty: "1000", price: "1.11", pl: "10", comm: "-0.1", fin: "0"})
	b.spread = ""
	tr := mustRecon(t, acts(a, b)).Completed[0]
	assert.False(t, tr.SpreadCost.Known())
	assert.Equal(t, "9.9 USD", mstr(t, tr.Net), "an unknown spread cost does not make Net unknown")
	assert.Equal(t, "0 USD", mstr(t, tr.Financing), "a reported zero is known")

	// One fill lacks commission: the total is unknown, never a partial sum, and so is Net.
	c := complete(buy("1", 0, "1000", "1.10"))
	c.comm = ""
	d := complete(spec{id: "2", at: time.Hour, side: order.Sell, qty: "1000", price: "1.11", pl: "10", comm: "-0.1"})
	tr = mustRecon(t, acts(c, d)).Completed[0]
	assert.False(t, tr.Commission.Known(), "one fill without commission makes the total unknown")
	assert.False(t, tr.Net.Known())
	assert.True(t, tr.GrossPL.Known())

	// Likewise for each Net component missing from the closing fill, which
	// is the fill that realizes P/L and financing and so owes them.
	for name, drop := range map[string]func(*spec){
		"financing": func(s *spec) { s.fin = "" }, "other fees": func(s *spec) { s.fee = "" }, "realized P/L": func(s *spec) { s.pl = "" },
	} {
		x := complete(buy("1", 0, "1000", "1.10"))
		y := complete(spec{id: "2", at: time.Hour, side: order.Sell, qty: "1000", price: "1.11", pl: "10"})
		drop(&y)
		tr = mustRecon(t, acts(x, y)).Completed[0]
		assert.False(t, tr.Net.Known(), "Net is unknown without %s", name)
	}
}

func TestAverageIsUnknownWithoutPricesAndRoundsHalfEven(t *testing.T) {
	// A leg with no price makes the average unknown. The reversal fill has
	// no fill-level price and its closing effect has none either, but the
	// opening effect does, so the fill is valid.
	closeNoPrice := eff("t1", accountactivity.TradeRoleClosed, "1000", "")
	openPriced := eff("t2", accountactivity.TradeRoleOpened, "500", "1.12")
	rev := complete(spec{id: "2", at: time.Hour, side: order.Sell, qty: "1500", price: "", pl: "5",
		effects: []accountactivity.TradeEffect{closeNoPrice, openPriced}})
	r := mustRecon(t, acts(
		complete(spec{id: "1", at: 0, side: order.Buy, qty: "1000", price: "1.10",
			effects: []accountactivity.TradeEffect{eff("t1", accountactivity.TradeRoleOpened, "1000", "1.10")}}),
		rev,
		complete(spec{id: "3", at: 2 * time.Hour, side: order.Buy, qty: "500", price: "1.11",
			effects: []accountactivity.TradeEffect{eff("t2", accountactivity.TradeRoleClosed, "500", "1.11")}})))
	require.Len(t, r.Completed, 2)
	long, short := r.Completed[0], r.Completed[1]
	assert.Equal(t, "1.1", pstr(t, long.AvgEntryPrice))
	assert.False(t, long.AvgExitPrice.Known(), "an exit leg with no price makes the average unknown")
	assert.Equal(t, "1.12", pstr(t, short.AvgEntryPrice))

	// Three entries whose weighted average is not exactly representable.
	in := acts(
		complete(buy("1", 0, "1", "1.00000001")),
		complete(buy("2", time.Minute, "1", "1.00000002")),
		complete(buy("3", 2*time.Minute, "1", "1.00000002")),
		complete(spec{id: "4", at: time.Hour, side: order.Sell, qty: "3", price: "1.1"}),
	)
	tr := mustRecon(t, in).Completed[0]
	// (1.00000001 + 1.00000002 + 1.00000002) / 3 = 1.000000016666..., half-even to 8 decimals.
	assert.Equal(t, "1.00000002", pstr(t, tr.AvgEntryPrice))
	assert.Contains(t, tr.AvgEntryPrice.Source(), "half-even")
	assert.Equal(t, accountactivity.BasisDerived, tr.AvgEntryPrice.Basis())
}

// An exit whose entry the data never shows is an orphan, not a trade, and
// the trade it touches cannot claim complete costs.
func TestOrphanExitIsListedAndNeverInventsATrade(t *testing.T) {
	in := acts(complete(spec{id: "1", at: 0, side: order.Sell, qty: "300", price: "1.10", pl: "5",
		effects: []accountactivity.TradeEffect{eff("t0", accountactivity.TradeRoleClosed, "300", "1.10")}}))
	r := mustRecon(t, in)
	assert.Empty(t, r.Completed)
	assert.Empty(t, r.Open)
	require.Len(t, r.Orphans, 1)
	assert.Equal(t, keys("1")[0], r.Orphans[0].Source)
	assert.Equal(t, "300", r.Orphans[0].Quantity.String())

	// A window that starts mid-trade: the exit closes more than is open.
	in = acts(
		complete(spec{id: "1", at: 0, side: order.Buy, qty: "100", price: "1.10",
			effects: []accountactivity.TradeEffect{eff("t0", accountactivity.TradeRoleOpened, "100", "1.10")}}),
		complete(spec{id: "2", at: time.Hour, side: order.Sell, qty: "300", price: "1.12", pl: "5",
			effects: []accountactivity.TradeEffect{eff("t0", accountactivity.TradeRoleClosed, "300", "1.12")}}),
	)
	r = mustRecon(t, in)
	require.Len(t, r.Completed, 1)
	tr := r.Completed[0]
	assert.Equal(t, "100", tr.ExitQuantity.String(), "only what was open is matched")
	assert.False(t, tr.GrossPL.Known(), "the fill's P/L cannot be attributed; the total is not claimed")
	assert.False(t, tr.Net.Known())
	require.Len(t, r.Orphans, 1)
	assert.Equal(t, "200", r.Orphans[0].Quantity.String())
}

// Standalone financing and fee activities, and non-trade events, are not
// folded into any trade.
func TestStandaloneActivitiesAreListedNotAttributed(t *testing.T) {
	in := acts(complete(buy("1", 0, "1000", "1.10")), complete(spec{id: "3", at: time.Hour, side: order.Sell, qty: "1000", price: "1.11", pl: "10"}))
	base := func(id string, kind accountactivity.Kind) accountactivity.Activity {
		return accountactivity.Activity{
			Provider: "oanda", Environment: accountactivity.EnvironmentPractice, ProviderAccountID: "acct",
			ProviderActivityID: id, ProviderType: "X", Kind: kind, OccurredAt: t0.Add(30 * time.Minute), ImportedAt: t0,
		}
	}
	fin := base("f", accountactivity.KindFinancing)
	fin.Financing = rep("-1.37")
	fee := base("e", accountactivity.KindFee)
	fee.OtherFees = rep("-2")
	dep := base("d", accountactivity.KindDeposit)
	dep.Cash = rep("500")
	oth := base("o", accountactivity.KindOther)
	r := mustRecon(t, append(in, fin, fee, dep, oth))

	require.Len(t, r.Completed, 1)
	assert.Equal(t, "0 USD", mstr(t, r.Completed[0].Financing), "daily financing is not attributed to the trade (issue #522)")
	assert.Equal(t, keys("e", "f"), r.Unattributed, "in the ledger's deterministic order (same instant: by activity ID)")
}

func TestInputRules(t *testing.T) {
	r, err := Reconstruct(nil)
	require.NoError(t, err)
	assert.Empty(t, r.Completed)

	// A different account is a different stream.
	other := fillOf(complete(buy("9", 0, "1", "1.1")))
	other.ProviderAccountID = "someone-else"
	_, err = Reconstruct(append(acts(complete(buy("1", 0, "1", "1.1"))), other))
	assert.ErrorIs(t, err, ErrMixedStreams)

	// An invalid activity is refused, not skipped.
	bad := fillOf(complete(buy("1", 0, "1", "1.1")))
	bad.ProviderType = ""
	_, err = Reconstruct([]accountactivity.Activity{bad})
	assert.ErrorIs(t, err, ErrInvalidActivity)

	// An identical repeat is harmless; a conflicting one is not.
	a := fillOf(complete(buy("1", 0, "1000", "1.10")))
	s := fillOf(complete(spec{id: "2", at: time.Hour, side: order.Sell, qty: "1000", price: "1.11", pl: "10"}))
	res, err := Reconstruct([]accountactivity.Activity{a, s, a, s})
	require.NoError(t, err)
	assert.Len(t, res.Completed, 1)
	changed := a
	changed.Price = accountactivity.Reported(num.MustParsePrice("1.5"))
	_, err = Reconstruct([]accountactivity.Activity{a, changed})
	assert.ErrorIs(t, err, ErrConflict)
}

// Every derived trade is traceable: its sources are real input activities,
// every leg's source is among them, and every contributing fill appears.
func TestEveryTradeReferencesItsSourceActivities(t *testing.T) {
	in := acts(
		complete(buy("1", 0, "400", "1.10")),
		complete(buy("2", time.Hour, "600", "1.12")),
		complete(spec{id: "3", at: 2 * time.Hour, side: order.Sell, qty: "1500", price: "1.13", pl: "30", comm: "-0.3"}),
		complete(spec{id: "4", at: 3 * time.Hour, side: order.Buy, qty: "500", price: "1.10", pl: "6"}),
	)
	known := map[accountactivity.Key]bool{}
	for _, a := range in {
		known[a.Key()] = true
	}
	r := mustRecon(t, in)
	require.Len(t, r.Completed, 2)
	for _, tr := range r.Completed {
		require.NotEmpty(t, tr.Sources)
		in := map[accountactivity.Key]bool{}
		for _, k := range tr.Sources {
			assert.True(t, known[k], "%s cites an unknown activity %s", tr.ID, k)
			assert.False(t, in[k], "duplicate source")
			in[k] = true
		}
		for _, l := range append(append([]Leg{}, tr.Entries...), tr.Exits...) {
			assert.True(t, in[l.Source], "leg source %s is not in the trade's sources", l.Source)
		}
	}
	assert.Equal(t, keys("1", "2", "3"), r.Completed[0].Sources)
	assert.Equal(t, keys("3", "4"), r.Completed[1].Sources, "the reversal fill is a source of both trades")
}

// The same activities in any order produce identical output.
func TestReplayIsDeterministicInAnyOrder(t *testing.T) {
	in := acts(
		complete(buy("1", 0, "400", "1.10")),
		complete(buy("2", time.Hour, "600", "1.12")),
		complete(spec{id: "3", at: 2 * time.Hour, side: order.Sell, qty: "1500", price: "1.13", pl: "30", comm: "-0.3", fin: "-0.2", spread: "-0.4"}),
		complete(spec{id: "4", at: 3 * time.Hour, side: order.Buy, qty: "200", price: "1.10", pl: "2"}),
		complete(spec{id: "5", at: 4 * time.Hour, side: order.Buy, qty: "300", price: "1.09", pl: "3"}),
		complete(spec{id: "6", at: 90 * time.Minute, inst: gbpus, side: order.Buy, qty: "100", price: "1.30"}),
	)
	want := mustRecon(t, in)
	require.Len(t, want.Completed, 2)
	require.Len(t, want.Open, 1)

	rng := rand.New(rand.NewSource(7))
	for i := 0; i < 50; i++ {
		shuffled := append([]accountactivity.Activity(nil), in...)
		rng.Shuffle(len(shuffled), func(a, b int) { shuffled[a], shuffled[b] = shuffled[b], shuffled[a] })
		assert.Equal(t, want, mustRecon(t, shuffled), "permutation %d", i)
	}
}

// Activities with the same timestamp are ordered by activity ID (numerically
// for digit IDs), so equal-time fills reconstruct the same way every time.
func TestSameInstantFillsAreOrderedByActivityID(t *testing.T) {
	in := acts(
		complete(buy("9", 0, "100", "1.10")),
		complete(spec{id: "10", at: 0, side: order.Sell, qty: "100", price: "1.12", pl: "2"}),
	)
	r := mustRecon(t, in)
	require.Len(t, r.Completed, 1, "9 sorts before 10, so the buy opens before the sell closes")
	assert.Equal(t, strconv.Itoa(9), r.Completed[0].Entries[0].Source.ProviderActivityID)
}

// Realized P/L and financing are realized by the closing. A pure entry that
// omits them says nothing about the trade's totals, so the completed trade is
// still known when the closing fill reports them; and the converse, a closing
// fill that omits them, leaves the total unknown.
func TestAbsentPLAndFinancingOnAPureEntryDoNotMakeTheTradeUnknown(t *testing.T) {
	entry := spec{id: "1", at: 0, side: order.Buy, qty: "1000", price: "1.10", comm: "-0.05", spread: "-0.25", fee: "0"} // no pl, no financing
	exit := spec{id: "2", at: time.Hour, side: order.Sell, qty: "1000", price: "1.11", pl: "10", fin: "-0.10", comm: "-0.05", spread: "-0.30", fee: "0"}

	tr := mustRecon(t, acts(entry, exit)).Completed[0]
	assert.Equal(t, "10 USD", mstr(t, tr.GrossPL), "the closing fill's P/L is the trade's")
	assert.Equal(t, "-0.1 USD", mstr(t, tr.Financing))
	assert.Equal(t, "9.8 USD", mstr(t, tr.Net), "10 - 0.1 commission - 0.1 financing")
	assert.Contains(t, tr.GrossPL.Source(), "over 1 fills", "only the closing fill contributed")

	// An entry that does report them keeps them (a reported zero is a fact).
	withZero := entry
	withZero.pl, withZero.fin = "0", "0"
	tr = mustRecon(t, acts(withZero, exit)).Completed[0]
	assert.Equal(t, "10 USD", mstr(t, tr.GrossPL))
	assert.Contains(t, tr.GrossPL.Source(), "over 2 fills")

	// A closing fill that omits them leaves the totals, and Net, unknown.
	noPL := exit
	noPL.pl = ""
	tr = mustRecon(t, acts(entry, noPL)).Completed[0]
	assert.False(t, tr.GrossPL.Known())
	assert.False(t, tr.Net.Known())
	noFin := exit
	noFin.fin = ""
	tr = mustRecon(t, acts(entry, noFin)).Completed[0]
	assert.False(t, tr.Financing.Known())
	assert.False(t, tr.Net.Known())
	assert.True(t, tr.GrossPL.Known())

	// The execution costs are still required of an entry.
	noComm := entry
	noComm.comm = ""
	tr = mustRecon(t, acts(noComm, exit)).Completed[0]
	assert.False(t, tr.Commission.Known(), "an entry that omits commission leaves it unknown")
}

// A closing leg that names a broker trade closes only that trade. Trade 7 is
// reduced by 300; trades 8888 and 9999 were never seen opening, so they are
// orphans and do not consume trade 7's remaining 200 units.
func TestAClosingLegNamingAnUnseenTradeIsAnOrphanNotAnotherTradesQuantity(t *testing.T) {
	entry := complete(spec{id: "1", at: 0, side: order.Buy, qty: "500", price: "1.08",
		effects: []accountactivity.TradeEffect{eff("7", accountactivity.TradeRoleOpened, "500", "1.08")}})
	reduce := eff("7", accountactivity.TradeRoleReduced, "300", "1.09")
	c1 := eff("8888", accountactivity.TradeRoleClosed, "100", "1.09")
	c2 := eff("9999", accountactivity.TradeRoleClosed, "100", "1.09")
	exit := complete(spec{id: "2", at: time.Hour, side: order.Sell, qty: "500", price: "1.09", pl: "1.2",
		effects: []accountactivity.TradeEffect{reduce, c1, c2}})

	r := mustRecon(t, acts(entry, exit))
	assert.Empty(t, r.Completed, "trade 7 still has 200 units open")
	require.Len(t, r.Open, 1)
	assert.Equal(t, "200", r.Open[0].Remaining.String())
	require.Len(t, r.Open[0].Exits, 1)
	assert.Equal(t, "300", r.Open[0].Exits[0].Quantity.String())
	assert.Equal(t, "7", r.Open[0].Exits[0].BrokerTradeID)
	require.Len(t, r.Orphans, 1)
	assert.Equal(t, "200", r.Orphans[0].Quantity.String(), "100 + 100 for the two unseen trades")

	// Closing trade 7's remaining 200 completes the trade, with only the
	// matched units; the unrelated trades' P/L never entered it.
	rest := complete(spec{id: "3", at: 2 * time.Hour, side: order.Sell, qty: "200", price: "1.10", pl: "4",
		effects: []accountactivity.TradeEffect{eff("7", accountactivity.TradeRoleClosed, "200", "1.10")}})
	r = mustRecon(t, acts(entry, exit, rest))
	require.Len(t, r.Completed, 1)
	assert.Equal(t, "500", r.Completed[0].ExitQuantity.String())
	assert.False(t, r.Completed[0].GrossPL.Known(), "the first exit fill included unattributable P/L, so the total is not claimed")
}

// Without any broker linkage, matching falls back to quantity in entry order.
func TestQuantityOnlyMatchingStillWorksWithoutTradeIDs(t *testing.T) {
	r := mustRecon(t, acts(
		complete(buy("1", 0, "300", "1.10")),
		complete(buy("2", time.Minute, "200", "1.11")),
		complete(spec{id: "3", at: time.Hour, side: order.Sell, qty: "500", price: "1.12", pl: "7"}),
	))
	require.Len(t, r.Completed, 1)
	assert.Empty(t, r.Orphans)
}

// A cost is divided with one rounding, not two: 1 of 3 units of -3 USD is
// exactly -1 and -2, not -0.99999999 and -2.00000001.
func TestApportionRoundsOnceAndTheSharesSumToTheTotal(t *testing.T) {
	q := func(s string) num.Quantity { return num.MustParseQuantity(s) }
	cases := []struct {
		total, part, whole, want string
	}{
		{"-3", "1", "3", "-1"},
		{"-3", "2", "3", "-2"},
		{"-0.15", "1000", "1500", "-0.1"},
		{"-0.15", "500", "1500", "-0.05"},
		{"10", "1", "3", "3.33333333"},
		{"0.00000001", "1", "2", "0"},          // tie: half to even (0)
		{"0.00000003", "1", "2", "0.00000002"}, // tie: half to even (2)
		{"-0.00000003", "1", "2", "-0.00000002"},
		{"0", "7", "9", "0"},
		{"123456.78901234", "999999", "1000000", "123456.66555555"}, // 123456.78901234 - 0.12345678901234, rounded once
	}
	for _, c := range cases {
		got, err := apportion(money(c.total), q(c.part), q(c.whole))
		require.NoError(t, err, "%s * %s / %s", c.total, c.part, c.whole)
		assert.Equal(t, money(c.want).String(), got.String(), "%s * %s / %s", c.total, c.part, c.whole)
	}
	_, err := apportion(money("1"), q("1"), q("0"))
	assert.Error(t, err)

	// End to end: the closing and opening shares of a split fill sum exactly.
	in := acts(
		complete(buy("1", 0, "1", "1.10")),
		complete(spec{id: "2", at: time.Hour, side: order.Sell, qty: "3", price: "1.12", comm: "-3", pl: "1"}),
		complete(spec{id: "3", at: 2 * time.Hour, side: order.Buy, qty: "2", price: "1.10"}),
	)
	r := mustRecon(t, in)
	require.Len(t, r.Completed, 2)
	assert.Equal(t, "-1 USD", mstr(t, r.Completed[0].Commission))
	assert.Equal(t, "-2 USD", mstr(t, r.Completed[1].Commission))
}

// Repeats of one activity written with equivalent timestamps are the same
// record, and the output cannot depend on which was seen first.
func TestEquivalentTimestampRepresentationsGiveIdenticalOutput(t *testing.T) {
	zone := time.FixedZone("east", 5*3600)
	a := fillOf(complete(buy("1", 0, "1000", "1.10")))
	b := fillOf(complete(spec{id: "2", at: time.Hour, side: order.Sell, qty: "1000", price: "1.11", pl: "10"}))
	aZoned, bZoned := a, b
	aZoned.OccurredAt = a.OccurredAt.In(zone)
	bZoned.OccurredAt = b.OccurredAt.In(zone)

	want := mustRecon(t, []accountactivity.Activity{a, b})
	for name, in := range map[string][]accountactivity.Activity{
		"zoned first":   {aZoned, a, bZoned, b},
		"utc first":     {a, aZoned, b, bZoned},
		"only zoned":    {aZoned, bZoned},
		"mixed reorder": {bZoned, a, b, aZoned},
	} {
		got := mustRecon(t, in)
		assert.Equal(t, want, got, name)
		require.Len(t, got.Completed, 1)
		assert.Equal(t, time.UTC, got.Completed[0].OpenedAt.Location(), name)
		assert.Equal(t, time.UTC, got.Completed[0].Entries[0].At.Location(), name)
	}
}

// A broker role is a fact even when the effect's units are missing; the net
// position must not reverse its meaning.
func TestRolesWithoutUnitsAreNotOverriddenByTheNetPosition(t *testing.T) {
	noUnits := func(id string, role accountactivity.TradeRole, price string) accountactivity.TradeEffect {
		e := accountactivity.TradeEffect{BrokerTradeID: id, Role: role}
		e.Price = accountactivity.Reported(num.MustParsePrice(price))
		return e
	}

	// A sell whose only effect is a close, with nothing open: an orphan exit,
	// not an invented short.
	r := mustRecon(t, acts(complete(spec{id: "1", at: 0, side: order.Sell, qty: "300", price: "1.10", pl: "5",
		effects: []accountactivity.TradeEffect{noUnits("t0", accountactivity.TradeRoleClosed, "1.10")}})))
	assert.Empty(t, r.Open, "no short was invented")
	assert.Empty(t, r.Completed)
	require.Len(t, r.Orphans, 1)
	assert.Equal(t, "300", r.Orphans[0].Quantity.String())

	// A sell whose only effect is an open, while a long is open (a hedging
	// account): it opens a short and leaves the long alone.
	hedged := mustRecon(t, acts(
		complete(spec{id: "1", at: 0, side: order.Buy, qty: "1000", price: "1.10",
			effects: []accountactivity.TradeEffect{eff("t1", accountactivity.TradeRoleOpened, "1000", "1.10")}}),
		complete(spec{id: "2", at: time.Hour, side: order.Sell, qty: "400", price: "1.11",
			effects: []accountactivity.TradeEffect{noUnits("t2", accountactivity.TradeRoleOpened, "1.11")}}),
	))
	assert.Empty(t, hedged.Completed)
	require.Len(t, hedged.Open, 2)
	byID := map[order.Side]OpenPosition{}
	for _, o := range hedged.Open {
		byID[o.Side] = o
	}
	assert.Equal(t, "1000", byID[order.Buy].Remaining.String(), "the long was not reduced")
	assert.Equal(t, "400", byID[order.Sell].Remaining.String())
	assert.Empty(t, byID[order.Buy].Exits)

	// A role-only close that names its trade closes that trade.
	closed := mustRecon(t, acts(
		complete(spec{id: "1", at: 0, side: order.Buy, qty: "400", price: "1.10",
			effects: []accountactivity.TradeEffect{eff("t1", accountactivity.TradeRoleOpened, "400", "1.10")}}),
		complete(spec{id: "2", at: time.Hour, side: order.Sell, qty: "400", price: "1.12", pl: "8",
			effects: []accountactivity.TradeEffect{noUnits("t1", accountactivity.TradeRoleClosed, "1.12")}}),
	))
	require.Len(t, closed.Completed, 1)
	assert.Equal(t, "t1", closed.Completed[0].Exits[0].BrokerTradeID)
	assert.False(t, closed.Completed[0].Exits[0].Split, "the direction was the broker's")

	// With roles in both directions and no units there is nothing to size the
	// split, so the net position decides and the leg says so.
	mixed := mustRecon(t, acts(
		complete(buy("1", 0, "1000", "1.10")),
		complete(spec{id: "2", at: time.Hour, side: order.Sell, qty: "1500", price: "1.12", pl: "5",
			effects: []accountactivity.TradeEffect{noUnits("t1", accountactivity.TradeRoleClosed, "1.12"), noUnits("t2", accountactivity.TradeRoleOpened, "1.12")}}),
	))
	require.Len(t, mixed.Completed, 1)
	assert.True(t, mixed.Completed[0].Exits[0].Split)
	require.Len(t, mixed.Open, 1)
	assert.Equal(t, "500", mixed.Open[0].Remaining.String())
}
