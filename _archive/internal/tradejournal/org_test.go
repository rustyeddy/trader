package tradejournal

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/internal/accountactivity"
	"github.com/rustyeddy/trader/internal/executionquality"
	"github.com/rustyeddy/trader/internal/tradehistory"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
)

var (
	usdC = num.MustParseCurrency("USD")
	eurC = num.MustParseCurrency("EUR")
)

var t0 = time.Date(2026, 10, 3, 14, 2, 11, 500, time.UTC)

func key(id string) accountactivity.Key {
	return accountactivity.Key{Provider: "p", Environment: accountactivity.EnvironmentPractice, ProviderAccountID: "a", ProviderActivityID: id}
}

func usd(s string) accountactivity.Value[num.Money] {
	return accountactivity.Reported(num.MustParseMoney(s, usdC))
}

func px(s string) accountactivity.Value[num.Price] {
	return accountactivity.Reported(num.MustParsePrice(s))
}

func eurusd() instrument.ID { return instrument.CurrencyPairID(eurC, usdC) }

// trade is a completed long with every figure known unless a test changes it.
func trade() tradehistory.Trade {
	return tradehistory.Trade{
		ID: "p/practice/a/2#long", Instrument: eurusd(), Side: order.Buy,
		OpenedAt: t0, ClosedAt: t0.Add(time.Hour),
		Entries:       []tradehistory.Leg{{Source: key("2"), At: t0, Quantity: num.MustParseQuantity("1000"), Price: px("1.1010"), BrokerTradeID: "t1"}},
		Exits:         []tradehistory.Leg{{Source: key("3"), At: t0.Add(time.Hour), Quantity: num.MustParseQuantity("1000"), Price: px("1.1090"), BrokerTradeID: "t1"}},
		EntryQuantity: num.MustParseQuantity("1000"), ExitQuantity: num.MustParseQuantity("1000"),
		AvgEntryPrice: px("1.1010"), AvgExitPrice: px("1.1090"),
		GrossPL: usd("8"), SpreadCost: usd("0.15"), Commission: usd("-1"), Financing: usd("0"), OtherFees: usd("0"), Net: usd("7"),
		Sources: []accountactivity.Key{key("2"), key("3")},
	}
}

func render(t *testing.T, d Document) string {
	t.Helper()
	var b bytes.Buffer
	require.NoError(t, RenderOrg(&b, d))
	return b.String()
}

func prop(t *testing.T, out, name string) string {
	t.Helper()
	for _, l := range strings.Split(out, "\n") {
		if v, ok := strings.CutPrefix(l, ":"+name+": "); ok {
			return v
		}
	}
	t.Fatalf("no :%s: property in\n%s", name, out)
	return ""
}

func TestEntryShape(t *testing.T) {
	out := render(t, Document{Entries: []Entry{{Trade: trade()}}})
	lines := strings.Split(out, "\n")
	// The heading carries the open time, not the close time.
	assert.Equal(t, "* 2026-10-03T14:02:11.0000005Z fx:EUR/USD long", lines[0])
	assert.Equal(t, ":PROPERTIES:", lines[1])
	assert.Equal(t, "p/practice/a/2#long", prop(t, out, "TRADE_ID"))
	assert.Equal(t, "2026-10-03T14:02:11.0000005Z", prop(t, out, "OPENED"))
	assert.Equal(t, "2026-10-03T15:02:11.0000005Z", prop(t, out, "CLOSED"))
	assert.Equal(t, "8.00 USD", prop(t, out, "GROSS_PL"))
	assert.Equal(t, "0.15 USD", prop(t, out, "SPREAD_COST"))
	assert.Equal(t, "-1.00 USD", prop(t, out, "COMMISSION"))
	assert.Equal(t, "7.00 USD", prop(t, out, "NET"))
	assert.Contains(t, out, "| entry | 2026-10-03T14:02:11.0000005Z | 1000 | 1.101 | t1 | broker | p/practice/a/2 |")
	// Without a benchmark there is no slippage anywhere, not even "unknown".
	assert.NotContains(t, out, "SLIPPAGE")
	assert.NotContains(t, out, "BENCHMARK")
}

func TestUnknownIsNeverZeroAndKnownZeroIsNeverUnknown(t *testing.T) {
	tr := trade()
	tr.Commission, tr.Net, tr.AvgExitPrice = accountactivity.Value[num.Money]{}, accountactivity.Value[num.Money]{}, accountactivity.Value[num.Price]{}
	out := render(t, Document{Entries: []Entry{{Trade: tr}}})
	assert.Equal(t, "unknown", prop(t, out, "COMMISSION"))
	assert.Equal(t, "unknown", prop(t, out, "NET"))
	assert.Equal(t, "unknown", prop(t, out, "AVG_EXIT"))
	// Known zeros stay zeros.
	assert.Equal(t, "0.00 USD", prop(t, out, "FINANCING"))
	assert.Equal(t, "0.00 USD", prop(t, out, "OTHER_FEES"))
}

// How a figure was derived is the audit view's job; the journal only marks it.
func TestDerivedFiguresSayTheyAreDerived(t *testing.T) {
	tr := trade()
	d, err := accountactivity.Derived(num.MustParsePrice("1.1010"), "weighted average of entry legs")
	require.NoError(t, err)
	tr.AvgEntryPrice = d
	out := render(t, Document{Entries: []Entry{{Trade: tr}}})
	assert.Equal(t, "1.101 (derived)", prop(t, out, "AVG_ENTRY"))
	assert.Equal(t, "1.109", prop(t, out, "AVG_EXIT"), "a reported figure carries no suffix")
}

func TestSourcesAreFirstLastAndCount(t *testing.T) {
	tr := trade()
	tr.Sources = []accountactivity.Key{key("2"), key("7"), key("8"), key("3")}
	out := render(t, Document{Entries: []Entry{{Trade: tr}}})
	assert.Equal(t, "p/practice/a/2", prop(t, out, "SOURCES_FIRST"))
	assert.Equal(t, "p/practice/a/3", prop(t, out, "SOURCES_LAST"))
	assert.Equal(t, "4", prop(t, out, "SOURCES_COUNT"))
	assert.NotContains(t, out, "p/practice/a/7", "the middle activities are not listed")
}

// The Review section is a scaffold; the renderer must never put anything in it.
func TestReviewIsAnEmptyScaffold(t *testing.T) {
	out := render(t, Document{Entries: []Entry{{Trade: trade()}, {Trade: trade()}}})
	want := "** Review\n- Setup:\n- What went well:\n- What went wrong:\n- Screenshot:\n- Notes:\n"
	assert.Equal(t, 2, strings.Count(out, want), out)
	for _, sec := range strings.Split(out, "** Review\n")[1:] {
		review, _, _ := strings.Cut(sec, "\n* ")
		for _, l := range strings.Split(strings.TrimSpace(review), "\n") {
			assert.True(t, strings.HasSuffix(l, ":"), "review line has content: %q", l)
		}
	}
}

func TestOutsideTextCannotBreakTheStructure(t *testing.T) {
	tr := trade()
	evil := "x\n* INJECTED\n:END:\r\n** Review"
	tr.ID = evil
	tr.Entries[0].BrokerTradeID = "t|1\n* more"
	tr.Sources = []accountactivity.Key{{Provider: "p", Environment: accountactivity.EnvironmentPractice, ProviderAccountID: "a", ProviderActivityID: evil}, key("3")}
	out := render(t, Document{Preamble: []string{"line one\n* HEADING"}, Entries: []Entry{{Trade: tr}}})
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "* ") {
			assert.True(t, strings.HasPrefix(l, "* 2026-"), "only the trade's own heading may start with *: %q", l)
		}
	}
	lines := strings.Split(out, "\n")
	count := func(want string) (n int) {
		for _, l := range lines {
			if l == want {
				n++
			}
		}
		return n
	}
	assert.Equal(t, 1, count(":END:"), "exactly one property drawer is closed")
	assert.Equal(t, 1, count("** Review"), "no second Review section")
	assert.NotContains(t, out, "\r")
	assert.Contains(t, out, `t\vert{}1`, "a bar would split the table cell")
}

func TestDeterministic(t *testing.T) {
	d := Document{Preamble: []string{"a"}, Entries: []Entry{{Trade: trade(), Quality: quality()}, {Trade: trade()}}}
	assert.Equal(t, render(t, d), render(t, d))
}

func TestNoEntriesStillRendersThePreamble(t *testing.T) {
	assert.Equal(t, "# nothing here\n", render(t, Document{Preamble: []string{"nothing here"}}))
	assert.Empty(t, render(t, Document{}))
}

func quality() *Quality {
	bench := &executionquality.Benchmark{Kind: executionquality.BrokerFullVWAP, Price: num.MustParsePrice("1.1000"), Source: "broker fullVWAP"}
	return &Quality{
		Benchmark:    "broker-vwap",
		SlippageCost: usd("-0.10"),
		Fills: []Fill{
			{Source: key("2"), Result: &executionquality.Result{
				ExecutionPrice: px("1.1010"), Benchmark: bench,
				Slippage: accountactivity.Reported(num.MustParseRate("-0.0010")), SlippageCost: usd("-0.10"),
				BenchmarkAge: -time.Second, AgeKnown: true,
			}},
			{Source: key("3"), Reason: "no quote | at all"},
		},
	}
}

func TestQualityOnlyWhenRequestedAndUnmeasuredIsUnknown(t *testing.T) {
	out := render(t, Document{Entries: []Entry{{Trade: trade(), Quality: quality()}}})
	assert.Equal(t, "broker-vwap", prop(t, out, "BENCHMARK"))
	assert.Equal(t, "-0.10 USD", prop(t, out, "SLIPPAGE_COST"))
	assert.Contains(t, out, "| p/practice/a/2 | 1.101 | broker_full_vwap 1.10 (broker fullVWAP) | -1s | -0.001 | -0.10 USD |")
	assert.Contains(t, out, "| p/practice/a/3 | unknown | unknown | unknown | unknown | unknown |")
	assert.Contains(t, out, "- p/practice/a/3: unmeasured: no quote | at all")

	q := quality()
	q.SlippageCost, q.Reason = accountactivity.Value[num.Money]{}, "one fill is unmeasured"
	out = render(t, Document{Entries: []Entry{{Trade: trade(), Quality: q}}})
	assert.Equal(t, "unknown", prop(t, out, "SLIPPAGE_COST"))
	assert.Contains(t, out, "Why unknown: one fill is unmeasured")
}

func TestWriteFailureIsReported(t *testing.T) {
	err := RenderOrg(failWriter{}, Document{Entries: []Entry{{Trade: trade()}}})
	assert.ErrorContains(t, err, "disk full")
}

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("disk full") }

// The journal is a renderer of canonical history. It must not reach a
// provider, the ledger store, or the run journal.
func TestBoundary(t *testing.T) {
	imports := importsOfPackage(t)
	for _, p := range imports {
		for _, bad := range []string{"internal/adapters", "internal/journal", "internal/service", "cmd/", "internal/accountactivity/"} {
			assert.NotContains(t, p, bad, "tradejournal imports %s", p)
		}
	}
}
