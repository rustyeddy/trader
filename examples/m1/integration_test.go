// Package m1_test is an external consumer of Trader's M1 public API: it
// imports only public packages (account, clock, config, id, instrument,
// logging, num, order, portfolio, and tradertest for incidental
// assertion/fixture support only), the way real code outside this
// module would. It exists for issue #32 (M1-14): proving M1 is a
// coherent, externally consumable foundation, not a collection of
// individually compiling packages.
//
// The central lifecycle (§1 of the #32 plan) is built almost entirely
// through the real domain constructors — instrument.New*,
// order.NewProposal/NewRequest/NewOrder/ApplyAcceptance/NewFill/
// ApplyFill, account.NewSnapshot, portfolio.NewPortfolio — rather than
// tradertest's builders, since the point of this package is to audit
// that API directly rather than exercise a convenience layer sitting
// in front of it.
package m1_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/rustyeddy/trader/account"
	"github.com/rustyeddy/trader/clock"
	"github.com/rustyeddy/trader/config"
	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/logging"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
	"github.com/rustyeddy/trader/portfolio"
	"github.com/rustyeddy/trader/tradertest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// m1Scenario is the result of building one complete, realistic M1
// vocabulary flow: an instrument and its listing, a filled order and
// the position it produced, two account snapshots at two different
// brokers in two different currencies, and the portfolio aggregating
// them.
type m1Scenario struct {
	listing     instrument.Listing
	filledOrder order.Order
	position    order.Position
	usdSnapshot account.Snapshot
	gbpSnapshot account.Snapshot
	portfolio   portfolio.Portfolio
}

// buildM1Scenario builds one EUR/USD order through to a fill and a
// resulting position, then aggregates it alongside a second, unrelated
// GBP account into one USD-denominated portfolio. Every domain value is
// built through its own package's real constructor; g supplies
// deterministic identity only.
func buildM1Scenario(g *id.Generator) (m1Scenario, error) {
	// instrument / listing.
	inst, err := instrument.NewCurrencyPair(num.MustParseCurrency("EUR"), num.MustParseCurrency("USD"))
	if err != nil {
		return m1Scenario{}, err
	}
	spec, err := instrument.NewSpec(
		num.MustParsePrice("0.00001"),
		num.MustParseQuantity("1"),
		num.MustParseRate("1"),
		num.MustParseCurrency("USD"),
	)
	if err != nil {
		return m1Scenario{}, err
	}
	listing, err := instrument.NewListing(instrument.ListingParams{
		Instrument: inst,
		Provider:   "OANDA",
		Symbol:     "EUR_USD",
		Spec:       spec,
		Tradable:   true,
	})
	if err != nil {
		return m1Scenario{}, err
	}

	// proposal -> request -> order lifecycle: PendingSubmit -> Working -> Filled.
	accountID, err := id.GenerateAccountID(g)
	if err != nil {
		return m1Scenario{}, err
	}
	proposal, err := order.NewProposal(order.Proposal{
		Listing:     listing,
		AccountID:   accountID,
		Side:        order.Buy,
		Type:        order.Market,
		TimeInForce: order.GTC,
		Quantity:    num.MustParseQuantity("1000"),
	})
	if err != nil {
		return m1Scenario{}, err
	}
	orderID, err := id.GenerateOrderID(g)
	if err != nil {
		return m1Scenario{}, err
	}
	request, err := order.NewRequest(proposal, orderID)
	if err != nil {
		return m1Scenario{}, err
	}
	pendingOrder, err := order.NewOrder(order.Order{
		Request: request,
		Status:  order.StatusPendingSubmit,
	})
	if err != nil {
		return m1Scenario{}, err
	}
	workingOrder, err := order.ApplyAcceptance(pendingOrder, "broker-order-1", request.Quantity, nil, nil)
	if err != nil {
		return m1Scenario{}, err
	}

	fillID, err := id.GenerateFillID(g)
	if err != nil {
		return m1Scenario{}, err
	}
	fillPrice := num.MustParsePrice("1.10000")
	fill, err := order.NewFill(order.Fill{
		FillID:        fillID,
		OrderID:       workingOrder.Request.OrderID,
		BrokerOrderID: workingOrder.BrokerOrderID,
		AccountID:     workingOrder.Request.AccountID,
		Listing:       workingOrder.Request.Listing,
		Side:          workingOrder.Request.Side,
		Price:         fillPrice,
		Quantity:      request.Quantity,
	})
	if err != nil {
		return m1Scenario{}, err
	}
	filledOrder, err := order.ApplyFill(workingOrder, fill)
	if err != nil {
		return m1Scenario{}, err
	}

	// position resulting from the fill.
	position, err := order.NewPosition(order.Position{
		AccountID: accountID,
		Listing:   listing,
		Side:      order.Long,
		Quantity:  request.Quantity,
		AvgPrice:  &fillPrice,
	})
	if err != nil {
		return m1Scenario{}, err
	}

	// account snapshot holding that position.
	asOf := time.Date(2026, time.January, 2, 12, 0, 0, 0, time.UTC)
	usd := num.MustParseCurrency("USD")
	usdSnapshot, err := account.NewSnapshot(account.SnapshotParams{
		AccountID:       accountID,
		Broker:          "OANDA",
		Currency:        usd,
		AsOf:            asOf,
		CashBalances:    []num.Money{num.MustParseMoney("10000", usd)},
		Equity:          num.MustParseMoney("10000", usd),
		BuyingPower:     num.MustParseMoney("9000", usd),
		MarginUsed:      num.MustParseMoney("1000", usd),
		MarginAvailable: num.MustParseMoney("9000", usd),
		RealizedPnL:     num.MustParseMoney("0", usd),
		UnrealizedPnL:   num.MustParseMoney("0", usd),
		Fees:            num.MustParseMoney("0", usd),
		Financing:       num.MustParseMoney("0", usd),
		Positions:       []order.Position{position},
	})
	if err != nil {
		return m1Scenario{}, err
	}

	// a second, unrelated account at a different broker in a different
	// currency, so the portfolio actually exercises cross-currency
	// aggregation rather than a trivial same-currency sum.
	gbpAccountID, err := id.GenerateAccountID(g)
	if err != nil {
		return m1Scenario{}, err
	}
	gbp := num.MustParseCurrency("GBP")
	gbpSnapshot, err := account.NewSnapshot(account.SnapshotParams{
		AccountID:       gbpAccountID,
		Broker:          "IBKR",
		Currency:        gbp,
		AsOf:            asOf,
		CashBalances:    []num.Money{num.MustParseMoney("5000", gbp)},
		Equity:          num.MustParseMoney("5000", gbp),
		BuyingPower:     num.MustParseMoney("5000", gbp),
		MarginUsed:      num.MustParseMoney("0", gbp),
		MarginAvailable: num.MustParseMoney("5000", gbp),
		RealizedPnL:     num.MustParseMoney("0", gbp),
		UnrealizedPnL:   num.MustParseMoney("0", gbp),
		Fees:            num.MustParseMoney("0", gbp),
		Financing:       num.MustParseMoney("0", gbp),
	})
	if err != nil {
		return m1Scenario{}, err
	}

	p, err := portfolio.NewPortfolio(portfolio.PortfolioParams{
		BaseCurrency: usd,
		AsOf:         asOf,
		Accounts:     []account.Snapshot{usdSnapshot, gbpSnapshot},
		Rates: []portfolio.ConversionRate{
			{From: gbp, To: usd, Rate: num.MustParseRate("1.25"), AsOf: asOf, Source: "example"},
		},
	})
	if err != nil {
		return m1Scenario{}, err
	}

	return m1Scenario{
		listing:     listing,
		filledOrder: filledOrder,
		position:    position,
		usdSnapshot: usdSnapshot,
		gbpSnapshot: gbpSnapshot,
		portfolio:   p,
	}, nil
}

// Example_m1Vocabulary is the runnable example issue #32 requires: it
// exercises the integrated M1 vocabulary end to end, using only public
// packages.
func Example_m1Vocabulary() {
	g := id.NewGenerator(clock.NewSimulated(time.Now()), id.NewDeterministic(1, 2))

	scenario, err := buildM1Scenario(g)
	if err != nil {
		panic(err)
	}

	equity, ok := scenario.portfolio.Equity()
	if !ok {
		panic("expected complete portfolio conversion")
	}

	fmt.Println(scenario.filledOrder.Status, scenario.portfolio.ConversionStatus(), equity)
	// Output: filled complete 16250 USD
}

// TestM1VocabularyReachesExpectedState re-runs the same scenario and
// asserts on it with testify plus a couple of tradertest's assertions
// (AssertTerminal, AssertStatus, AssertMoneyEqual) — the kind of
// incidental, verification-only use of tradertest the #32 plan calls
// for, as opposed to using it to build the scenario itself.
func TestM1VocabularyReachesExpectedState(t *testing.T) {
	g := id.NewGenerator(clock.NewSimulated(time.Now()), id.NewDeterministic(3, 4))

	scenario, err := buildM1Scenario(g)
	require.NoError(t, err)

	tradertest.AssertTerminal(t, scenario.filledOrder)
	tradertest.AssertStatus(t, order.StatusFilled, scenario.filledOrder)

	require.NotNil(t, scenario.filledOrder.AcceptedQuantity)
	assert.True(t, scenario.filledOrder.FilledQuantity.Equal(*scenario.filledOrder.AcceptedQuantity))

	assert.Equal(t, order.Long, scenario.position.Side)

	require.Len(t, scenario.usdSnapshot.Positions(), 1)
	require.Empty(t, scenario.usdSnapshot.OpenOrders(), "the order is Filled/terminal, so it must not appear as an open order")

	require.Equal(t, portfolio.ConversionComplete, scenario.portfolio.ConversionStatus())
	equity, ok := scenario.portfolio.Equity()
	require.True(t, ok)
	tradertest.AssertMoneyEqual(t, num.MustParseMoney("16250", num.MustParseCurrency("USD")), equity)

	assert.Len(t, scenario.portfolio.Accounts(), 2)
}

// appConfig is a small, realistic composition-root configuration type:
// one plain field, one field with a default, and one secret.
type appConfig struct {
	Broker   string `config:"broker" default:"OANDA"`
	APIKey   string `config:"api_key" secret:"true" required:"true"`
	LogLevel string `config:"log_level" default:"info" enum:"debug,info,warn,error"`
}

// TestConfigRedactsSecrets exercises config.Load and config.Sprint: a
// secret-tagged field's real value must never appear in rendered
// configuration output.
func TestConfigRedactsSecrets(t *testing.T) {
	const secretValue = "sk_live_deadbeefdeadbeef"

	cfg, err := config.Load[appConfig](config.Options{
		Environ:     []string{},
		FileContent: []byte("broker: OANDA\napi_key: " + secretValue + "\nlog_level: debug\n"),
	})
	require.NoError(t, err)
	require.Equal(t, secretValue, cfg.APIKey, "Load itself must still resolve the real value")

	out := config.Sprint(cfg)
	assert.Contains(t, out, config.Redacted)
	assert.NotContains(t, out, secretValue, "the secret's real value must never reach rendered output")
	assert.Contains(t, out, "OANDA", "non-secret fields still render normally")
}

// TestLoggingStructuredFieldsAndInjectedLogger exercises an injected
// (not global) *slog.Logger built by logging.New, and confirms
// structured fields — including one wrapped by logging.Secret — are
// captured correctly rather than interpolated into a formatted string.
func TestLoggingStructuredFieldsAndInjectedLogger(t *testing.T) {
	logger, closer, err := logging.New(logging.Config{Format: "json", Output: "stderr"})
	require.NoError(t, err)
	defer closer.Close()
	require.NotNil(t, logger, "New returns a logger a caller injects into its own components, not a package-level global")

	// logging.Capture is the public testkit counterpart to New: same
	// structured-record shape, but assertable instead of writing to an
	// io.Writer.
	captured, rec := logging.Capture()
	captured.Info("order filled",
		"order_status", "filled",
		"broker", "OANDA",
		"api_key", logging.Secret("sk_live_deadbeefdeadbeef"),
	)

	records := rec.Records()
	require.Len(t, records, 1)
	assert.Equal(t, "order filled", records[0].Message)
	assert.Equal(t, "filled", records[0].Attrs["order_status"])
	assert.Equal(t, "OANDA", records[0].Attrs["broker"])
	assert.Equal(t, "REDACTED", records[0].Attrs["api_key"])
}
