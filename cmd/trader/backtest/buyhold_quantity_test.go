package backtest_test

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/internal/journal"
	"github.com/rustyeddy/trader/num"
)

// buyHoldArgs runs buy-and-hold quantity mode on the EUR/USD H1
// fixture, 2024-01-08 00:00-12:00, with $10,000 at the default 1.0
// initial-margin ratio.
func buyHoldArgs(t *testing.T, journalPath string, extra ...string) []string {
	t.Helper()
	args := []string{
		"run",
		"--symbol", "EURUSD",
		"--interval", "H1",
		"--from", "2024-01-08T00:00:00Z",
		"--to", "2024-01-08T12:00:00Z",
		"--starting-cash", "10000",
		"--currency", "USD",
		"--adverse-distance", "0.01000", // required by config; unused in quantity mode
		"--data-raw-root", "testdata/raw/oanda",
		"--data-store-root", t.TempDir(),
		"--output-dir", t.TempDir(),
		"--format", "json",
	}
	if journalPath != "" {
		args = append(args, "--journal", journalPath)
	}
	return append(args, extra...)
}

type buyHoldReport struct {
	Run struct {
		StrategyName       string          `json:"strategy_name"`
		StrategyVersion    string          `json:"strategy_version"`
		StrategyParameters json.RawMessage `json:"strategy_parameters"`
	} `json:"run"`
	Performance struct {
		FinalEquity struct {
			Amount string `json:"amount"`
		} `json:"final_equity"`
	} `json:"performance"`
	ClosedTrades []struct {
		OpenedAt    time.Time `json:"opened_at"`
		ClosedAt    time.Time `json:"closed_at"`
		RealizedPnL struct {
			Amount string `json:"amount"`
		} `json:"realized_pnl"`
	} `json:"closed_trades"`
	OpenTrades []struct {
		OpenedAt time.Time `json:"opened_at"`
	} `json:"open_trades"`
	Account struct {
		OpenPositions []struct {
			Quantity string `json:"quantity"`
		} `json:"open_positions"`
	} `json:"account"`
	Margin struct {
		Rejections          int     `json:"margin_rejection_count"`
		AdmissionRejections int     `json:"admission_rejection_count"`
		PeakGrossLeverage   *string `json:"peak_gross_leverage"`
	} `json:"margin"`
}

func runBuyHold(t *testing.T, args []string) buyHoldReport {
	t.Helper()
	out, err := runReport(t, args)
	require.NoError(t, err)
	var r buyHoldReport
	require.NoError(t, json.Unmarshal([]byte(out), &r))
	return r
}

func usdAmount(s string) num.Money { return num.MustParseMoney(s, num.MustParseCurrency("USD")) }

// TestBuyHoldQuantity_BaselineOpensUnleveredAndHoldsToRunEnd is #417's
// baseline: $10,000 at ratio 1.0, 8500 EUR/USD (about 9,350 USD, with
// headroom for the next-open fill) fills with no margin rejections and
// stays open at the run's end, valued at the final mark.
func TestBuyHoldQuantity_BaselineOpensUnleveredAndHoldsToRunEnd(t *testing.T) {
	r := runBuyHold(t, buyHoldArgs(t, "", "--quantity", "8500"))

	assert.Equal(t, "buy-and-hold", r.Run.StrategyName)
	assert.Equal(t, "quantity-v1", r.Run.StrategyVersion)
	assert.JSONEq(t, `{"name":"buy-and-hold","mode":"quantity","quantity":"8500"}`, string(r.Run.StrategyParameters),
		"the quantity choice is recorded in the manifest")

	assert.Zero(t, r.Margin.Rejections)
	require.NotNil(t, r.Margin.PeakGrossLeverage)
	assert.Equal(t, -1, num.MustParseRate(*r.Margin.PeakGrossLeverage).Cmp(num.MustParseRate("1")), "unlevered")

	assert.Empty(t, r.ClosedTrades, "run end is not a sell: no closing trade is synthesized")
	require.Len(t, r.OpenTrades, 1)
	require.Len(t, r.Account.OpenPositions, 1)
	assert.Equal(t, "8500", r.Account.OpenPositions[0].Quantity, "exactly the requested quantity")

	// Decided on the 00:00 bar, filled at the 01:00 open; valued at the
	// last bar's (11:00) close.
	fill := fillBarFor(t, "testdata/raw/oanda", "2024-01-08T01:00:00Z").Open
	last := fillBarFor(t, "testdata/raw/oanda", "2024-01-08T11:00:00Z").Close
	qty := num.MustParseQuantity("8500")
	cost, err := fill.MulQuantity(qty, num.MustParseCurrency("USD"))
	require.NoError(t, err)
	value, err := last.MulQuantity(qty, num.MustParseCurrency("USD"))
	require.NoError(t, err)
	pnl, err := value.Sub(cost)
	require.NoError(t, err)
	want, err := usdAmount("10000").Add(pnl)
	require.NoError(t, err)
	assert.True(t, want.Equal(usdAmount(r.Performance.FinalEquity.Amount)),
		"final equity %s, want %s (open position at the final mark)", r.Performance.FinalEquity.Amount, want)
	assert.True(t, r.OpenTrades[0].OpenedAt.Equal(time.Date(2024, 1, 8, 1, 0, 0, 0, time.UTC)))
}

func TestBuyHoldQuantity_SellDateExits(t *testing.T) {
	r := runBuyHold(t, buyHoldArgs(t, "", "--quantity", "8500", "--sell-date", "2024-01-08T06:00:00Z"))
	assert.Empty(t, r.OpenTrades)
	assert.Empty(t, r.Account.OpenPositions)
	require.Len(t, r.ClosedTrades, 1)
	// Exit decided on the 06:00 bar, filled at the 07:00 open.
	assert.True(t, r.ClosedTrades[0].ClosedAt.Equal(time.Date(2024, 1, 8, 7, 0, 0, 0, time.UTC)))

	entry := fillBarFor(t, "testdata/raw/oanda", "2024-01-08T01:00:00Z").Open
	exit := fillBarFor(t, "testdata/raw/oanda", "2024-01-08T07:00:00Z").Open
	qty := num.MustParseQuantity("8500")
	in, err := entry.MulQuantity(qty, num.MustParseCurrency("USD"))
	require.NoError(t, err)
	out, err := exit.MulQuantity(qty, num.MustParseCurrency("USD"))
	require.NoError(t, err)
	want, err := out.Sub(in)
	require.NoError(t, err)
	assert.True(t, want.Equal(usdAmount(r.ClosedTrades[0].RealizedPnL.Amount)))
}

func TestBuyHoldQuantity_BuyDateDelaysEntry(t *testing.T) {
	r := runBuyHold(t, buyHoldArgs(t, "", "--quantity", "8500", "--buy-date", "2024-01-08T04:00:00Z"))
	require.Len(t, r.OpenTrades, 1)
	// Decided on the 04:00 bar, filled at the 05:00 open.
	assert.True(t, r.OpenTrades[0].OpenedAt.Equal(time.Date(2024, 1, 8, 5, 0, 0, 0, time.UTC)))
}

// TestBuyHoldQuantity_UnaffordableQuantityRejectedNotResized: 20000 EUR
// (about 21,800 USD) on $10,000 at ratio 1.0 is rejected at admission,
// requested at exactly 20000, and never retried or resized. With a
// sell_date, the refused entry produces no exit.
func TestBuyHoldQuantity_UnaffordableQuantityRejectedNotResized(t *testing.T) {
	journalPath := filepath.Join(t.TempDir(), "run.jsonl")
	r := runBuyHold(t, buyHoldArgs(t, journalPath, "--quantity", "20000", "--sell-date", "2024-01-08T06:00:00Z"))

	assert.Equal(t, 1, r.Margin.Rejections)
	assert.Equal(t, 1, r.Margin.AdmissionRejections)
	assert.Empty(t, r.OpenTrades)
	assert.Empty(t, r.ClosedTrades)

	var proposals, noActions int
	for _, e := range journalEntries(t, journalPath) {
		switch e.Kind {
		case journal.KindProposal:
			proposals++
			assert.Equal(t, "20000", e.Proposal.Quantity.String(), "requested exactly, not resized")
		case journal.KindNoAction:
			noActions++
		}
	}
	assert.Equal(t, 1, proposals, "attempted once")
	assert.Zero(t, noActions, "no exit is sent for a position that was never opened")
}

func TestBuyHoldQuantity_ConfigFile(t *testing.T) {
	configPath := writeConfigFile(t, `
backtest:
  symbol: EURUSD
  interval: H1
  from: 2024-01-08T00:00:00Z
  to: 2024-01-08T12:00:00Z
  starting_capital: 10000
  adverse_distance: 0.01000

strategy:
  name: buy-and-hold
  quantity: 8500
  buy_date: 2024-01-08T02:00:00Z
  sell_date: 2024-01-08T08:00:00Z
`)
	r := runBuyHold(t, []string{"run", "--config", configPath, "--data-raw-root", "testdata/raw/oanda", "--data-store-root", t.TempDir(), "--output-dir", t.TempDir(), "--format", "json"})
	assert.JSONEq(t, `{"name":"buy-and-hold","mode":"quantity","quantity":"8500","buy_date":"2024-01-08T02:00:00Z","sell_date":"2024-01-08T08:00:00Z"}`, string(r.Run.StrategyParameters))
	require.Len(t, r.ClosedTrades, 1)
	assert.True(t, r.ClosedTrades[0].OpenedAt.Equal(time.Date(2024, 1, 8, 3, 0, 0, 0, time.UTC)))
	assert.True(t, r.ClosedTrades[0].ClosedAt.Equal(time.Date(2024, 1, 8, 9, 0, 0, 0, time.UTC)))
}

func TestBuyHoldQuantity_InvalidCombinationsRejected(t *testing.T) {
	cases := []struct {
		name  string
		extra []string
		want  string
	}{
		{"buy date without quantity", []string{"--buy-date", "2024-01-08"}, "require strategy.quantity"},
		{"sell date without quantity", []string{"--sell-date", "2024-01-08"}, "require strategy.quantity"},
		{"sell before buy", []string{"--quantity", "1", "--buy-date", "2024-01-08T06:00:00Z", "--sell-date", "2024-01-08T02:00:00Z"}, "must be after the effective buy date"},
		{"sell before default buy date", []string{"--quantity", "1", "--sell-date", "2024-01-07"}, "must be after the effective buy date (2024-01-08T00:00:00Z)"},
		{"explicit zero quantity", []string{"--quantity", "0"}, "strategy.quantity must be positive"},
		{"bad date", []string{"--quantity", "1", "--buy-date", "tomorrow"}, "strategy.buy_date"},
		{"non-numeric quantity", []string{"--quantity", "lots"}, "quantity"},
		{"two symbols", []string{"--quantity", "1", "--symbol", "GBPUSD"}, "exactly one instrument"},
		{"with strategy-exec", []string{"--quantity", "1", "--strategy-exec", flipFlopPath}, "--strategy-exec"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := runReport(t, buyHoldArgs(t, "", tc.extra...))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

// weekendArgs spans a weekend: EUR/USD's first bar after
// 2024-01-06 00:00 (a Saturday) is Sunday evening.
func weekendArgs(t *testing.T, extra ...string) []string {
	args := buyHoldArgs(t, "", extra...)
	for i, a := range args {
		if a == "--from" {
			args[i+1] = "2024-01-06T00:00:00Z"
		}
	}
	return args
}

// TestBuyHoldQuantity_EntryPrecedesPassedSellDate: when both dates fall
// before the first available bar, the strategy still buys first, then
// exits once it holds the position.
func TestBuyHoldQuantity_EntryPrecedesPassedSellDate(t *testing.T) {
	t.Run("both dates before the first bar", func(t *testing.T) {
		r := runBuyHold(t, weekendArgs(t, "--quantity", "8500", "--buy-date", "2024-01-06", "--sell-date", "2024-01-07T00:00:00Z"))
		require.Len(t, r.ClosedTrades, 1, "entered, then exited")
		assert.Empty(t, r.OpenTrades)
		assert.True(t, r.ClosedTrades[0].ClosedAt.After(r.ClosedTrades[0].OpenedAt))
	})
	t.Run("default buy date, sell date before the first bar", func(t *testing.T) {
		r := runBuyHold(t, weekendArgs(t, "--quantity", "8500", "--sell-date", "2024-01-07T00:00:00Z"))
		require.Len(t, r.ClosedTrades, 1)
	})
}

// TestBuyHoldQuantity_ExplicitZeroNeverFallsBackToDemo: an explicit
// zero is rejected on every configuration path, never treated as
// "unset" (which would silently run the fixed-fraction demo).
func TestBuyHoldQuantity_ExplicitZeroNeverFallsBackToDemo(t *testing.T) {
	t.Run("flag", func(t *testing.T) {
		_, err := runReport(t, buyHoldArgs(t, "", "--quantity", "0"))
		require.ErrorContains(t, err, "strategy.quantity must be positive")
	})
	t.Run("config file", func(t *testing.T) {
		configPath := writeConfigFile(t, `
backtest:
  symbol: EURUSD
  interval: H1
  from: 2024-01-08T00:00:00Z
  to: 2024-01-08T12:00:00Z
  adverse_distance: 0.01000

strategy:
  name: buy-and-hold
  quantity: 0
`)
		_, err := runReport(t, []string{"run", "--config", configPath, "--data-raw-root", "testdata/raw/oanda", "--data-store-root", t.TempDir(), "--output-dir", t.TempDir()})
		require.ErrorContains(t, err, "strategy.quantity must be positive")
	})
	t.Run("environment", func(t *testing.T) {
		t.Setenv("TRADER_STRATEGY_QUANTITY", "0")
		_, err := runReport(t, buyHoldArgs(t, ""))
		require.ErrorContains(t, err, "strategy.quantity must be positive")
	})
}

// TestBuyHoldQuantity_DatesRecordCanonically: equivalent date spellings
// record identical strategy parameters and config digests.
func TestBuyHoldQuantity_DatesRecordCanonically(t *testing.T) {
	store := t.TempDir() // shared: config_digest embeds dataset BuiltAt (ADR-042)
	run := func(buy, sell string) (json.RawMessage, string) {
		out, err := runReport(t, buyHoldArgs(t, "", "--quantity", "8500", "--buy-date", buy, "--sell-date", sell, "--data-store-root", store))
		require.NoError(t, err)
		var r struct {
			Run struct {
				StrategyParameters json.RawMessage `json:"strategy_parameters"`
				ConfigDigest       string          `json:"config_digest"`
			} `json:"run"`
		}
		require.NoError(t, json.Unmarshal([]byte(out), &r))
		return r.Run.StrategyParameters, r.Run.ConfigDigest
	}
	p1, d1 := run("2024-01-08", "2024-01-08T06:00:00Z")
	p2, d2 := run("2024-01-08T00:00:00Z", "2024-01-08T01:00:00-05:00")
	assert.JSONEq(t, `{"name":"buy-and-hold","mode":"quantity","quantity":"8500","buy_date":"2024-01-08T00:00:00Z","sell_date":"2024-01-08T06:00:00Z"}`, string(p1))
	assert.JSONEq(t, string(p1), string(p2))
	assert.Equal(t, d1, d2)
}
