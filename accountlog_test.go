package trader

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// obs is one account.jsonl line as a reader that knows nothing of the engine sees
// it: plain strings and numbers.
type obs struct {
	Seq        int       `json:"seq"`
	Time       time.Time `json:"time"`
	Kind       string    `json:"kind"`
	Account    string    `json:"account"`
	Balance    amount    `json:"balance"`
	Unrealized amount    `json:"unrealized"`
	Equity     amount    `json:"equity"`
	Open       int       `json:"open"`
	Stale      []string  `json:"stale"`
}

type amount struct{ Amount, Currency string }

func (a amount) rat(t *testing.T) *big.Rat {
	r, ok := new(big.Rat).SetString(a.Amount)
	require.True(t, ok, a.Amount)
	return r
}

func readObs(t *testing.T, journal string) []obs {
	t.Helper()
	var out []obs
	sc := bufio.NewScanner(strings.NewReader(journal))
	for sc.Scan() {
		var o obs
		require.NoError(t, json.Unmarshal(sc.Bytes(), &o), sc.Text())
		out = append(out, o)
	}
	return out
}

// replayDrawdown is the maximum drawdown computed from the persisted lines alone,
// with big rationals, as the documentation says a reader may.
func replayDrawdown(t *testing.T, obss []obs) (depth, pct *big.Rat, peakAt, troughAt time.Time) {
	t.Helper()
	require.NotEmpty(t, obss)
	peak, peakTime := obss[0].Equity.rat(t), obss[0].Time
	depth, pct, peakAt, troughAt = new(big.Rat), new(big.Rat), peakTime, peakTime
	for _, o := range obss[1:] {
		e := o.Equity.rat(t)
		if e.Cmp(peak) > 0 {
			peak, peakTime = e, o.Time
		}
		if d := new(big.Rat).Sub(peak, e); d.Cmp(depth) > 0 {
			depth, pct = d, new(big.Rat).Mul(new(big.Rat).Quo(d, peak), big.NewRat(100, 1))
			peakAt, troughAt = peakTime, o.Time
		}
	}
	return depth, pct, peakAt, troughAt
}

// observed replays the series through a trader running strat with an account log
// and returns the log, the lines it wrote and the trader.
func observed(t *testing.T, strat strategy, series ...marketdata.Series) (*accountLog, []obs, *trader) {
	t.Helper()
	captureLogs(t)
	var buf bytes.Buffer
	tr, _ := pairTrader(usd(10000), strat)
	tr.accountLog = &accountLog{w: &buf}
	require.NoError(t, (&backtest{}).runSeries(context.Background(), tr, series...))
	return tr.accountLog, readObs(t, buf.String()), tr
}

// agreesWithReplay checks the drawdown the log tracked against the replay of the
// lines it wrote, field by field.
func agreesWithReplay(t *testing.T, l *accountLog, obss []obs) {
	t.Helper()
	depth, pct, peakAt, troughAt := replayDrawdown(t, obss)
	assert.Equal(t, depth.Cmp(ratOf(l.dd.Amount)), 0, "amount: replay %s, tracked %s", depth.FloatString(8), l.dd.Amount)
	tracked, ok := new(big.Rat).SetString(l.dd.Pct.String())
	require.True(t, ok)
	half := big.NewRat(1, 200000000) // the tracked value is rounded to 8 places
	diff := new(big.Rat).Abs(new(big.Rat).Sub(pct, tracked))
	assert.LessOrEqual(t, diff.Cmp(half), 0, "percentage: replay %s, tracked %s", pct.FloatString(10), l.dd.Pct)
	assert.True(t, peakAt.Equal(l.dd.PeakTime), "peak time %s vs %s", peakAt, l.dd.PeakTime)
	assert.True(t, troughAt.Equal(l.dd.TroughTime), "trough time %s vs %s", troughAt, l.dd.TroughTime)
}

func TestAccountLog_FlatEquityWithNoTrades(t *testing.T) {
	l, obss, _ := observed(t, &reactStrategy{react: func(marketdata.Bar) *order { return nil }},
		wave("EURUSD", 1, 100000, 100000, 100000))

	require.Len(t, obss, 4, "the initial observation and one per frame")
	assert.Equal(t, observeInitial, obss[0].Kind)
	assert.True(t, obss[0].Time.Equal(hour(0)), "the initial observation is at the opening of the first frame")
	for i, o := range obss {
		assert.Equal(t, i+1, o.Seq)
		assert.Equal(t, "10000", o.Equity.Amount)
		assert.Equal(t, "USD", o.Equity.Currency)
		assert.Equal(t, "a", o.Account)
		assert.Zero(t, o.Open)
		if i > 0 {
			assert.Equal(t, observeFrame, o.Kind)
			assert.True(t, o.Time.Equal(hour(i)), "a frame is observed at its availability")
		}
	}
	assert.True(t, l.dd.Amount.IsZero())
	assert.True(t, l.dd.Pct.IsZero())
	assert.True(t, l.dd.PeakTime.Equal(hour(0)) && l.dd.TroughTime.Equal(hour(0)), "peak and trough are the initial observation")
	agreesWithReplay(t, l, obss)
}

// A long of 10000 EURUSD units (1% risk of 10000 over a stop 0.0100 away) opened at
// 1.00000 on the bar that follows the order: each 0.00100 of EURUSD is 10 USD.
func TestAccountLog_MonotonicRiseHasNoDrawdown(t *testing.T) {
	l, obss, _ := observed(t, openOn("EURUSD", "EURUSD", 100000, 99000),
		wave("EURUSD", 1, 100000, 100000, 100100, 100200, 100300))

	assert.Equal(t, 1, obss[len(obss)-1].Open)
	for i := 1; i < len(obss); i++ {
		assert.GreaterOrEqual(t, obss[i].Equity.rat(t).Cmp(obss[i-1].Equity.rat(t)), 0, "equity never falls")
	}
	assert.Equal(t, "10030", obss[len(obss)-1].Equity.Amount, "0.00300 on 10000 units")
	assert.True(t, l.dd.Amount.IsZero())
	assert.True(t, l.dd.Pct.IsZero())
	agreesWithReplay(t, l, obss)
	pct, ok, err := l.netReturnPct()
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "0.3", pct.String(), "30 over 10000")
}

// Equity is the close: 10030 after the rise, 9990 after the dip (a fall of 40,
// 40/10030 = 0.39880359%), then above the old peak again.
func TestAccountLog_DrawdownWithRecovery(t *testing.T) {
	l, obss, _ := observed(t, openOn("EURUSD", "EURUSD", 100000, 99000),
		wave("EURUSD", 1, 100000, 100000, 100300, 99900, 100100, 100500))

	assert.Equal(t, usd(40), l.dd.Amount)
	assert.Equal(t, "0.39880359", l.dd.Pct.String())
	assert.Equal(t, usd(10030), l.dd.PeakEquity)
	assert.Equal(t, usd(9990), l.dd.TroughEquity)
	assert.True(t, l.dd.PeakTime.Equal(hour(3)), l.dd.PeakTime.String())
	assert.True(t, l.dd.TroughTime.Equal(hour(4)), l.dd.TroughTime.String())
	assert.Equal(t, "10050", obss[len(obss)-1].Equity.Amount, "it recovered, and the drawdown stays")
	agreesWithReplay(t, l, obss)
}

func TestAccountLog_UnrecoveredDrawdownIsTheDrawdown(t *testing.T) {
	l, obss, _ := observed(t, openOn("EURUSD", "EURUSD", 100000, 99000),
		wave("EURUSD", 1, 100000, 100000, 100300, 100200, 100000, 99800))

	assert.Equal(t, usd(50), l.dd.Amount, "from 10030 to 9980 at the last frame")
	assert.Equal(t, usd(9980), l.dd.TroughEquity)
	assert.True(t, l.dd.TroughTime.Equal(obss[len(obss)-1].Time), "the trough is the last observation")
	agreesWithReplay(t, l, obss)
}

// A run that never rises above its start: the initial observation is the peak, and
// the open position's unrealized loss at the end counts, not only realized balance.
func TestAccountLog_OpenPositionLossesCountAndBalanceDoesNot(t *testing.T) {
	l, obss, tr := observed(t, openOn("EURUSD", "EURUSD", 100000, 99000),
		wave("EURUSD", 1, 100000, 100000, 99900, 99700))

	last := obss[len(obss)-1]
	assert.Equal(t, "10000", last.Balance.Amount, "nothing has realized")
	assert.Equal(t, "-30", last.Unrealized.Amount)
	assert.Equal(t, usd(30), l.dd.Amount)
	assert.Equal(t, usd(10000), l.dd.PeakEquity)
	assert.True(t, l.dd.PeakTime.Equal(hour(0)), "the initial observation took part in the peak")
	assert.Equal(t, "0.3", l.dd.Pct.String())
	assert.Len(t, tr.account.positions, 1, "the position is still open")
	agreesWithReplay(t, l, obss)
}

// Two instruments in one account: a frame is one coherent observation at its
// availability, and a position whose instrument has no bar in the frame is listed
// as stale instead of being marked again from nothing.
func TestAccountLog_OneObservationPerFrameForSeveralInstruments(t *testing.T) {
	eur, gbp := pairBars()
	strat := openOn("GBPUSD", "GBPUSD", 200000, 198000)
	l, obss, _ := observed(t, strat, eur, gbp)

	// pairBars: EURUSD has bars at hours 0-4, GBPUSD at 0, 1, 2 and 4: five frames.
	require.Len(t, obss, 6)
	for i, o := range obss[1:] {
		want := hour(i + 1)
		assert.True(t, o.Time.Equal(want), "frame %d at %s, got %s", i, want, o.Time)
	}
	for i, o := range obss {
		if i == 4 { // the frame whose only bar is EURUSD's
			assert.Equal(t, []string{"GBPUSD"}, o.Stale, "the open GBPUSD keeps its mark and says so")
			assert.Equal(t, 1, o.Open)
			assert.Equal(t, obss[3].Unrealized, o.Unrealized, "an unmarked position is not revalued")
		} else {
			assert.Empty(t, o.Stale, "frame %d", i)
		}
	}
	agreesWithReplay(t, l, obss)
}

func TestStaleInstruments_ListsOpenPositionsWithoutABarInTheFrame(t *testing.T) {
	_, act := pairTrader(usd(10000), nil)
	act.positions["EURUSD"] = longAt("EURUSD", 1000, 100000, 100000)
	act.positions["GBPUSD"] = longAt("GBPUSD", 1000, 200000, 200000)

	f := pairFrame(hbar("GBPUSD", hour(1), 200000, 200000))

	assert.Equal(t, []string{"EURUSD"}, staleInstruments(act, f))
	assert.Empty(t, staleInstruments(act, pairFrame(hbar("EURUSD", hour(1), 1, 1), hbar("GBPUSD", hour(1), 1, 1))))
}

func TestAccountLog_RefusesWhatItCannotMeasure(t *testing.T) {
	t.Run("a drawdown from an equity that is not positive has no percentage", func(t *testing.T) {
		l := &accountLog{}
		l.seq++
		require.NoError(t, l.track(hour(0), usd(0)))
		l.seq++
		require.ErrorContains(t, l.track(hour(1), usd(-5)), "no percentage")
	})
	t.Run("an account in another currency is not mixed in", func(t *testing.T) {
		l := &accountLog{}
		l.seq++
		require.NoError(t, l.track(hour(0), usd(100)))
		l.seq++
		assert.Error(t, l.track(hour(1), num.MustParseMoney("90", num.MustParseCurrency("EUR"))))
	})
	t.Run("an account with no valid money stops the run", func(t *testing.T) {
		l := &accountLog{}
		act := newAccount("a", num.Money{}, testMarkets())
		assert.Error(t, l.observe(observeInitial, hour(0), act, nil))
	})
}

type badAccountWriter struct{ fail error }

func (w badAccountWriter) Write(p []byte) (int, error) {
	if w.fail != nil {
		return 0, w.fail
	}
	return len(p) - 1, nil
}

func TestAccountLog_AWriteThatFailsStopsTheRun(t *testing.T) {
	captureLogs(t)
	for name, w := range map[string]badAccountWriter{"error": {fail: errors.New("disk full")}, "short write": {}} {
		t.Run(name, func(t *testing.T) {
			res, err := RunSeriesWith(context.Background(), RunOptions{Account: w}, "x", time.Time{}, wave("EURUSD", 1, upThenDown...))

			require.Error(t, err)
			assert.Nil(t, res)
			assert.ErrorContains(t, err, "account journal")
		})
	}
}

// Through the engine's entry point: the summary's drawdown is the one a replay of
// the written lines gives, the last line is the final account, and the warm-up
// bars are not observed.
func TestRunSeriesWith_SummaryDrawdownEqualsTheReplayOfTheAccountJournal(t *testing.T) {
	captureLogs(t)
	var acct, events bytes.Buffer
	eur, gbp := wave("EURUSD", 1, upThenDown...), wave("GBPUSD", 2, upThenDown...)

	res, err := RunSeriesWith(context.Background(), RunOptions{Account: &acct, Journal: &events}, "x", hour(4), eur, gbp)

	require.NoError(t, err)
	sum := res.Summary()
	obss := readObs(t, acct.String())
	require.Len(t, obss, 1+(len(upThenDown)-4), "the initial observation and a frame per traded hour")
	assert.True(t, obss[0].Time.Equal(hour(4)), "the first traded frame, not the warm-up")
	require.NotNil(t, sum.MaxDrawdown)
	depth, _, peakAt, troughAt := replayDrawdown(t, obss)
	assert.Equal(t, 0, depth.Cmp(ratOf(sum.MaxDrawdown.Amount)))
	assert.True(t, peakAt.Equal(sum.MaxDrawdown.PeakTime))
	assert.True(t, troughAt.Equal(sum.MaxDrawdown.TroughTime))
	last := obss[len(obss)-1]
	assert.Equal(t, sum.Balance.String(), last.Balance.Amount+" USD", "the last line is the final account")
	assert.Equal(t, sum.Equity.String(), last.Equity.Amount+" USD")
	assert.Equal(t, sum.Unrealized.String(), last.Unrealized.Amount+" USD")
	assert.Equal(t, len(sum.Open), last.Open)
}

func TestRunSeriesWith_TheSummaryIsTheSameWithOrWithoutAnAccountWriter(t *testing.T) {
	captureLogs(t)
	eur := wave("EURUSD", 1, upThenDown...)
	var buf bytes.Buffer
	with, err := RunSeriesWith(context.Background(), RunOptions{Account: &buf}, "x", time.Time{}, eur)
	require.NoError(t, err)
	without, err := RunSeries(context.Background(), "x", time.Time{}, eur)
	require.NoError(t, err)

	assert.Equal(t, without.Summary(), with.Summary())
	assert.NotNil(t, without.Summary().MaxDrawdown, "the drawdown does not depend on a writer")
}

func TestAccountLog_IsDeterministic(t *testing.T) {
	captureLogs(t)
	run := func() string {
		var buf bytes.Buffer
		_, err := RunSeriesWith(context.Background(), RunOptions{Account: &buf}, "x", time.Time{}, wave("EURUSD", 1, upThenDown...))
		require.NoError(t, err)
		return buf.String()
	}
	assert.Equal(t, run(), run())
}

func TestSummary_WinRateAndNetReturn(t *testing.T) {
	captureLogs(t)
	res, err := RunSeries(context.Background(), "x", time.Time{}, wave("EURUSD", 1, 100000, 100000, 100000))
	require.NoError(t, err)
	s := res.Summary()
	assert.Nil(t, s.WinRate, "no closed trade: the rate is undefined, not zero")
	assert.True(t, s.NetReturnPct.IsZero())

	res, err = RunSeries(context.Background(), "x", time.Time{}, wave("EURUSD", 1, upThenDown...))
	require.NoError(t, err)
	s = res.Summary()
	require.NotZero(t, s.Trades)
	require.NotNil(t, s.WinRate)
	want := num.MustParseRate(strconv.Itoa(s.Wins))
	want, err = want.DivRate(num.MustParseRate(strconv.Itoa(s.Trades)))
	require.NoError(t, err)
	assert.Equal(t, want, *s.WinRate)
	gain, err := s.Equity.Sub(s.InitialBalance)
	require.NoError(t, err)
	assert.Equal(t, 0, ratOf(gain).Cmp(new(big.Rat).Mul(ratOf(s.InitialBalance), mustRat(t, s.NetReturnPct.String(), 100))),
		"net return is the gain over the initial equity, in percent")
}

func mustRat(t *testing.T, pct string, div int64) *big.Rat {
	r, ok := new(big.Rat).SetString(pct)
	require.True(t, ok)
	return r.Quo(r, big.NewRat(div, 1))
}
