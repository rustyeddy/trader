package trader

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// approvedSummary is the tables in emax-validation.org, written out by hand as
// the example prints them. Pending counts the waiting stops of open positions
// as well as waiting opens.
const approvedSummary = `a-reversal-exit: 10 bars
  orders: submitted 4, filled 2, rejected 0, cancelled 1, pending 1
  trades: closed 1, wins 1, losses 0, break-even 0
  money: realized $33.00, unrealized $0.00, balance $10033.00, equity $10033.00
  open: none
b1-stop-wick: 8 bars
  orders: submitted 2, filled 2, rejected 0, cancelled 0, pending 0
  trades: closed 1, wins 0, losses 1, break-even 0
  money: realized -$102.09, unrealized $0.00, balance $9897.91, equity $9897.91
  open: none
b2-stop-cross: 9 bars
  orders: submitted 5, filled 3, rejected 1, cancelled 0, pending 1
  trades: closed 1, wins 0, losses 1, break-even 0
  money: realized -$102.09, unrealized $35.57, balance $9897.91, equity $9933.48
  open: short 9122 EURUSD from 1.08490, marked 1.08100
b3-stop-gap: 7 bars
  orders: submitted 2, filled 2, rejected 0, cancelled 0, pending 0
  trades: closed 1, wins 0, losses 1, break-even 0
  money: realized -$129.01, unrealized $0.00, balance $9870.99, equity $9870.99
  open: none
c-reversal: 12 bars
  orders: submitted 5, filled 3, rejected 0, cancelled 1, pending 1
  trades: closed 1, wins 1, losses 0, break-even 0
  money: realized $33.00, unrealized $42.43, balance $10033.00, equity $10075.43
  open: short 9087 EURUSD from 1.10437, marked 1.09970
d-short-stop: 7 bars
  orders: submitted 2, filled 2, rejected 0, cancelled 0, pending 0
  trades: closed 1, wins 0, losses 1, break-even 0
  money: realized -$101.82, unrealized $0.00, balance $9898.18, equity $9898.18
  open: none
`

func TestRun_PrintsTheApprovedSummaryOfEveryScenario(t *testing.T) {
	logs := captureLogs(t)

	var out bytes.Buffer
	require.NoError(t, run(&out))

	assert.Equal(t, approvedSummary, out.String())
	for _, scenario := range scenarios {
		assert.Contains(t, logs.String(), "scenario="+scenario)
	}
	assert.Contains(t, logs.String(), "order rejected", "B2's close is an expected rejection")
	assert.NotContains(t, logs.String(), "level=ERROR")
}

func TestRun_RunsTheScenariosTheTestsApprove(t *testing.T) {
	var names []string
	for _, s := range emaxScenarios {
		names = append(names, s.name)
	}
	assert.Equal(t, names, scenarios)
}

func TestRun_PrintsTheSameBytesEveryTime(t *testing.T) {
	captureLogs(t)

	var first, second bytes.Buffer
	require.NoError(t, run(&first))
	require.NoError(t, run(&second))

	assert.Equal(t, first.String(), second.String())
}

func TestRun_ReportsAMissingScenario(t *testing.T) {
	captureLogs(t)
	t.Chdir(t.TempDir())

	var out bytes.Buffer
	err := run(&out)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "scenario a-reversal-exit")
	assert.Empty(t, out.String())
}

func TestResult_ZeroPnLIsABreakEven(t *testing.T) {
	br := newBroker("sim", testMarkets())
	act := br.addAccount("a", usd(10000))
	act.trades = []trade{{pnl: cents(3)}, {pnl: cents(-1)}, {pnl: cents(0)}, {pnl: cents(0)}} // the balance is left alone

	r, err := newResult("s", 0, &trader{broker: br, account: act})
	require.NoError(t, err)

	assert.Equal(t, 4, r.trades)
	assert.Equal(t, 1, r.wins)
	assert.Equal(t, 1, r.losses)
	assert.Equal(t, 2, r.breakEven)
	assert.Equal(t, cents(2), r.realized, "the trades' sum, not the balance change")
}

// The one-cent run's two opens are refused at sizing and never reach the broker;
// its close is queued and then rejected. All three count as rejected, and only the
// close as submitted.
func TestResult_CountsRefusalsWithoutDuplication(t *testing.T) {
	refused := runEMAXWithBalance(t, "a-reversal-exit", cents(1))

	r, err := newResult("s", 10, refused.trader)
	require.NoError(t, err)

	assert.Equal(t, 3, r.rejected)
	assert.Equal(t, 1, r.submitted)
	assert.Zero(t, r.filled)
	assert.Zero(t, r.pending)
	assert.Zero(t, r.trades)
}

func TestResult_CountsOnlyItsAccountOrders(t *testing.T) {
	br := newBroker("sim", testMarkets())
	a := br.addAccount("a", usd(10000))
	br.addAccount("b", usd(20000))
	for _, id := range []string{"a", "b"} {
		br.completed = append(br.completed,
			&order{accountID: id, state: stateFilled},
			&order{accountID: id, state: stateRejected},
			&order{accountID: id, state: stateCancelled})
		for _, symbol := range []string{"EURUSD", "GBPUSD"} {
			br.orders[symbol] = append(br.orders[symbol],
				&order{accountID: id, instrument: symbol, state: stateSubmitted})
		}
	}
	tr := &trader{broker: br, account: a, rejected: []*order{{accountID: "a", state: stateRejected}}}

	r, err := newResult("a", 0, tr)
	require.NoError(t, err)

	assert.Equal(t, 5, r.submitted, "three completed and two pending for account a")
	assert.Equal(t, 1, r.filled)
	assert.Equal(t, 2, r.rejected, "one before submission and one at the broker")
	assert.Equal(t, 1, r.cancelled)
	assert.Equal(t, 2, r.pending)
	assert.Equal(t, usd(10000), r.balance)
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("disk full") }

func TestResult_WriteReturnsTheWriterError(t *testing.T) {
	zero := zeroMoney(usdCurrency)
	r := &result{realized: zero, unrealized: zero, balance: zero, equity: zero}
	assert.EqualError(t, r.write(failingWriter{}), "disk full")
}

func TestFormatMoney(t *testing.T) {
	for want, m := range map[string]money{
		"$0.00":     cents(0),
		"$0.05":     cents(5),
		"-$0.05":    cents(-5),
		"$10033.00": cents(1003300),
		"-$102.09":  cents(-10209),
		// Cents are floored toward negative infinity (ADR-086): a loss is never understated.
		"-$102.09 floor": num.MustParseMoney("-102.08316", usdCurrency),
		"$9897.91 floor": num.MustParseMoney("9897.91684", usdCurrency),
		"-$0.01 floor":   num.MustParseMoney("-0.00000001", usdCurrency),
		"$0.00 floor":    num.MustParseMoney("0.00999999", usdCurrency),
		"-$5.00 floor":   num.MustParseMoney("-5.00000000", usdCurrency),
	} {
		c, err := floorCents(m)
		require.NoError(t, err)
		assert.Equal(t, strings.TrimSuffix(want, " floor"), formatCents(c))
	}
}

func TestFloorCents_RejectsInvalidMoney(t *testing.T) {
	_, err := floorCents(num.Money{})
	assert.Error(t, err)
}

func TestFormatPrice(t *testing.T) {
	assert.Equal(t, "1.10437", formatPrice(px(110437)))
	assert.Equal(t, "0.00005", formatPrice(px(5)))
}

func TestRun_ExportedEntryPointPrintsTheApprovedSummary(t *testing.T) {
	captureLogs(t)

	var out bytes.Buffer
	require.NoError(t, Run(&out))

	assert.Equal(t, approvedSummary, out.String())
}

const barsHeader = "# schema=design-bars-v1 source=synthetic instrument=EURUSD tf=d1 year=2010 month=01 scenario=test\n" +
	"time,open,high,low,close,volume\n"

func TestReadBars_ParsesExactPointsInOrder(t *testing.T) {
	md, err := readBars(strings.NewReader(barsHeader +
		"2010-01-04T00:00:00Z,1.10323,1.10650,1.09197,1.10001,1000\n" +
		"2010-01-05T00:00:00Z,1.10015,1.10130,1.10005,1.10100,0\n"))
	require.NoError(t, err)

	assert.Equal(t, "EURUSD", md.Instrument)
	assert.Equal(t, "d1", md.Interval)
	first := time.Date(2010, time.January, 4, 0, 0, 0, 0, time.UTC)
	second := first.AddDate(0, 0, 1)
	assert.Equal(t, []marketdata.Bar{
		{Instrument: "EURUSD", Time: first, Volume: 1000, Mid: marketdata.OHLC{Open: px(110323), High: px(110650), Low: px(109197), Close: px(110001)}, AvailableAt: second},
		{Instrument: "EURUSD", Time: second, Mid: marketdata.OHLC{Open: px(110015), High: px(110130), Low: px(110005), Close: px(110100)}, AvailableAt: second.AddDate(0, 0, 1)},
	}, md.Bars)
}

func TestReadBars_AvailableWhenTheIntervalEnds(t *testing.T) {
	open := time.Date(2010, time.January, 4, 10, 0, 0, 0, time.UTC)
	tests := []struct {
		tf   string
		want time.Time
	}{
		{"h1", open.Add(time.Hour)},
		{"d1", open.AddDate(0, 0, 1)},
	}
	for _, tt := range tests {
		t.Run(tt.tf, func(t *testing.T) {
			md, err := readBars(strings.NewReader("# schema=design-bars-v1 instrument=EURUSD tf=" + tt.tf + "\n" +
				"time,open,high,low,close,volume\n" +
				"2010-01-04T10:00:00Z,1.10000,1.10100,1.09900,1.10050,1000\n"))
			require.NoError(t, err)
			require.Len(t, md.Bars, 1)
			assert.Equal(t, open, md.Bars[0].Time, "the opening time is unchanged")
			assert.Equal(t, tt.want, md.Bars[0].AvailableAt)
		})
	}
}

func TestReadBars_RefusesAnUnsupportedInterval(t *testing.T) {
	for _, tf := range []string{"w1", "h2", "H1"} {
		t.Run(tf, func(t *testing.T) {
			_, err := readBars(strings.NewReader("# schema=design-bars-v1 instrument=EURUSD tf=" + tf + "\n" +
				"time,open,high,low,close,volume\n" +
				"2010-01-04T10:00:00Z,1.10000,1.10100,1.09900,1.10050,1000\n"))
			require.ErrorContains(t, err, "unsupported tf")
		})
	}
}

func TestReadBars_RefusesMalformedInput(t *testing.T) {
	const row = "2010-01-04T00:00:00Z,1.10000,1.10100,1.09900,1.10050,1000\n"
	for name, in := range map[string]string{
		"empty":            "",
		"header only line": "# schema=design-bars-v1 instrument=EURUSD tf=d1",
		"not a comment":    "schema=design-bars-v1 instrument=EURUSD tf=d1\n" + row,
		"wrong schema":     "# schema=canonical-v1 instrument=EURUSD tf=d1\ntime,open,high,low,close,volume\n" + row,
		"no instrument":    "# schema=design-bars-v1 tf=d1\ntime,open,high,low,close,volume\n" + row,
		"no interval":      "# schema=design-bars-v1 instrument=EURUSD\ntime,open,high,low,close,volume\n" + row,
		"no columns":       "# schema=design-bars-v1 instrument=EURUSD tf=d1\n",
		"wrong columns":    "# schema=design-bars-v1 instrument=EURUSD tf=d1\ntime,high,open,low,close,volume\n" + row,
		"quoted five-field columns": "# schema=design-bars-v1 instrument=EURUSD tf=d1\n\"time,open\",high,low,close,volume\n" +
			"2010-01-04T00:00:00Z,1.10000,1.10100,1.09900,1.10050\n",
		"quoted one-field columns": "# schema=design-bars-v1 instrument=EURUSD tf=d1\n\"time,open,high,low,close,volume\"\n" +
			"2010-01-04T00:00:00Z\n",
		"no rows":          barsHeader,
		"short row":        barsHeader + "2010-01-04T00:00:00Z,1.10000,1.10100\n",
		"date only":        barsHeader + "2010-01-04,1.10000,1.10100,1.09900,1.10050,1000\n",
		"offset time":      barsHeader + "2010-01-04T00:00:00+01:00,1.10000,1.10100,1.09900,1.10050,1000\n",
		"four decimals":    barsHeader + "2010-01-04T00:00:00Z,1.1000,1.10100,1.09900,1.10050,1000\n",
		"six decimals":     barsHeader + "2010-01-04T00:00:00Z,1.100000,1.10100,1.09900,1.10050,1000\n",
		"no whole part":    barsHeader + "2010-01-04T00:00:00Z,.10000,1.10100,1.09900,1.10050,1000\n",
		"signed price":     barsHeader + "2010-01-04T00:00:00Z,-1.10000,1.10100,1.09900,1.10050,1000\n",
		"signed fraction":  barsHeader + "2010-01-04T00:00:00Z,1.+1000,1.10100,1.09900,1.10050,1000\n",
		"zero price":       barsHeader + "2010-01-04T00:00:00Z,0.00000,1.10100,1.09900,1.10050,1000\n",
		"overflow":         barsHeader + "2010-01-04T00:00:00Z,99999999999999999.00000,1.10100,1.09900,1.10050,1000\n",
		"above the bound":  barsHeader + "2010-01-04T00:00:00Z,10000.00001,10000.00002,10000.00001,10000.00001,1000\n",
		"high below open":  barsHeader + "2010-01-04T00:00:00Z,1.10200,1.10100,1.09900,1.10050,1000\n",
		"high below close": barsHeader + "2010-01-04T00:00:00Z,1.10000,1.10100,1.09900,1.10150,1000\n",
		"low above open":   barsHeader + "2010-01-04T00:00:00Z,1.09800,1.10100,1.09900,1.10050,1000\n",
		"low above close":  barsHeader + "2010-01-04T00:00:00Z,1.10000,1.10100,1.09900,1.09800,1000\n",
		"bad volume":       barsHeader + "2010-01-04T00:00:00Z,1.10000,1.10100,1.09900,1.10050,many\n",
		"negative volume":  barsHeader + "2010-01-04T00:00:00Z,1.10000,1.10100,1.09900,1.10050,-1\n",
		"duplicate time":   barsHeader + row + row,
		"out of order":     barsHeader + row + "2010-01-03T00:00:00Z,1.10000,1.10100,1.09900,1.10050,1000\n",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := readBars(strings.NewReader(in))
			require.Error(t, err)
		})
	}
}

func TestLoadBars_NamesTheFileInErrors(t *testing.T) {
	dir := t.TempDir()

	_, err := loadBars(filepath.Join(dir, "missing.csv"))
	require.ErrorIs(t, err, os.ErrNotExist)

	path := filepath.Join(dir, "bad.csv")
	require.NoError(t, os.WriteFile(path, []byte(barsHeader), 0o600))
	_, err = loadBars(path)
	require.ErrorContains(t, err, path)
}

func TestParsePrice_RefusesWhatTheBarsFileDoesNot(t *testing.T) {
	for name, in := range map[string]string{
		"four decimals":  "1.1000",
		"six decimals":   "1.100000",
		"no decimals":    "1",
		"no whole part":  ".10000",
		"signed":         "-1.10000",
		"plus sign":      "+1.10000",
		"letters":        "1.1a000",
		"zero":           "0.00000",
		"above the max":  "10000.00001",
		"not a decimal":  "x.xxxxx",
		"empty":          "",
		"exponent":       "1e1.0000",
		"embedded space": "1 .10000",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := parsePrice(in)
			assert.Error(t, err, in)
		})
	}
	got, err := parsePrice("10000.00000")
	require.NoError(t, err, "the maximum itself is allowed")
	assert.Equal(t, maxPrice, got)
	got, err = parsePrice("0.00001")
	require.NoError(t, err)
	assert.Equal(t, px(1), got)
}
