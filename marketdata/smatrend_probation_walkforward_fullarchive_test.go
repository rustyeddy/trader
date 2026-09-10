//go:build fullarchive

// This file runs a true walk-forward baseline for strategy/smatrend's
// "probation-trend" ExitRule (issue #349, the SMA Long Hold playbook)
// against real SPY history, at TrailingStopPercent 0.10 and 0.20 —
// the same walk-forward window parameters (5-year train, 1-year test,
// 1-year step) EQS-01's own walk-forward sweep
// (eqs01_walkforward_fullarchive_test.go) established, but with no
// parameter grid search or selection: both TrailingStopPercent values
// (and every other playbook reference parameter) are fixed inputs,
// not chosen per fold. Every individual out-of-sample trade is
// recorded to a CSV trade log, intended as a durable baseline to
// compare against once this strategy's rules are tweaked.
//
// Lives under marketdata/ as package marketdata_test for the same
// reason eqs01_walkforward_fullarchive_test.go does (it needs
// marketdata/internal/provider/stooq, only reachable from inside this
// directory tree) and reuses that file's own already-built helpers
// (setupEQS01WalkForwardInstrument, eqs01WFInstrument,
// eqs01WFTrainBars/TestBars/StepBars, eqs01WFStartingCapital,
// eqs01WFPriceSource, cagrFromReturn, calmarRatio, equityCurveAt,
// mustParseFloatWF) directly rather than duplicating them.
//
// Results go to a local, private directory
// (fullArchiveSMATrendWFOutputDir, edited locally, never committed) —
// never anywhere under this repository, per the same decision
// recorded in eqs01_walkforward_fullarchive_test.go's own doc
// comment.
package marketdata_test

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	simbroker "github.com/rustyeddy/trader/adapters/broker/sim"
	"github.com/rustyeddy/trader/backtest"
	"github.com/rustyeddy/trader/clock"
	"github.com/rustyeddy/trader/execution"
	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/journal"
	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
	"github.com/rustyeddy/trader/pipeline"
	"github.com/rustyeddy/trader/risk"
	svcbacktest "github.com/rustyeddy/trader/service/backtest"
	"github.com/rustyeddy/trader/strategy/smatrend"
)

// fullArchiveSMATrendWFOutputDir names the local directory this test
// writes its results into — empty by default; an operator edits this
// locally (for example to
// "/srv/trading/sweeps/smatrend-probation-walkforward").
const fullArchiveSMATrendWFOutputDir = ""

// wfMemoryRecorder is a minimal in-memory journal.Recorder, needed
// here (unlike eqs01_walkforward_fullarchive_test.go's own driver,
// which only ever needed aggregate equity-curve/metrics figures) to
// recover each individual Fill's own Price/Quantity for the trade
// log below: order.Trade itself carries only FillIDs, not price or
// quantity directly.
type wfMemoryRecorder struct {
	records []journal.Record
}

func (r *wfMemoryRecorder) Record(ctx context.Context, rec journal.Record) error {
	r.records = append(r.records, rec)
	return nil
}
func (r *wfMemoryRecorder) Close() error { return nil }

// fillsByID indexes every KindFill record this recorder observed by
// its own FillID, for TradeLogRow's own entry/exit price/quantity
// reconstruction.
func (r *wfMemoryRecorder) fillsByID() map[id.FillID]order.Fill {
	m := make(map[id.FillID]order.Fill, len(r.records))
	for _, rec := range r.records {
		if rec.Kind == journal.KindFill && rec.Fill != nil {
			m[rec.Fill.FillID] = *rec.Fill
		}
	}
	return m
}

// wfJournalEnvironmentFactory is eqs01WFEnvironmentFactory (see that
// type's own doc comment for the composition it builds) plus a wired
// Journal — everything else is identical.
type wfJournalEnvironmentFactory struct {
	prices  map[time.Time]marketdata.Bar
	journal journal.Recorder
}

func (f wfJournalEnvironmentFactory) NewEnvironment(ctx context.Context, req svcbacktest.EnvironmentRequest) (svcbacktest.Environment, error) {
	c := clock.NewSimulated(req.Span.Start())
	ids := id.NewGenerator(c, id.NewDeterministic(1, 2))
	accountID, err := id.GenerateAccountID(ids)
	if err != nil {
		return svcbacktest.Environment{}, err
	}

	prices := &eqs01WFPriceSource{clock: c, bars: f.prices}
	b, err := simbroker.NewBroker("sim", simbroker.Deps{
		Clock:  c,
		IDs:    ids,
		Prices: prices,
	}, simbroker.AccountConfig{AccountID: accountID, StartingCash: req.StartingCapital})
	if err != nil {
		return svcbacktest.Environment{}, err
	}
	acct, err := b.OpenAccount(ctx, accountID)
	if err != nil {
		return svcbacktest.Environment{}, err
	}

	planner, err := execution.NewPlanner(execution.Deps{Clock: c, IDs: ids})
	if err != nil {
		return svcbacktest.Environment{}, err
	}
	engine, err := risk.NewEngine()
	if err != nil {
		return svcbacktest.Environment{}, err
	}
	pl, err := pipeline.NewPipeline(pipeline.Deps{
		Sizer:   risk.NewFixedFractionSizer(),
		Planner: planner,
		Engine:  engine,
		Broker:  b,
		IDs:     ids,
	})
	if err != nil {
		return svcbacktest.Environment{}, err
	}

	fill, err := backtest.NewComponentInfo(prices.Info().Name, prices.Info().Version, nil)
	if err != nil {
		return svcbacktest.Environment{}, err
	}
	none, err := backtest.NewComponentInfo("none", "", nil)
	if err != nil {
		return svcbacktest.Environment{}, err
	}

	return svcbacktest.Environment{
		Clock:           c,
		IDs:             ids,
		Account:         acct,
		Pipeline:        pl,
		Journal:         f.journal,
		FillModel:       fill,
		SlippageModel:   none,
		CommissionModel: none,
	}, nil
}

// runWFBacktestWithJournal is runEQS01WFBacktest plus a wired
// journal, returning the recorder alongside the response so the
// caller can recover individual Fill records.
func runWFBacktestWithJournal(ctx context.Context, mgr *marketdata.Manager, simResolver instrument.Resolver, simID instrument.ID,
	span marketdata.TimeRange, cfg smatrend.Config, startingCapital num.Money, riskFraction num.Rate, adverseDistance num.Price,
	priceByTime map[time.Time]marketdata.Bar) (svcbacktest.RunResponse, *wfMemoryRecorder, error) {

	rec := &wfMemoryRecorder{}
	factory := wfJournalEnvironmentFactory{prices: priceByTime, journal: rec}
	svc, err := svcbacktest.New(mgr, simResolver, factory, nil)
	if err != nil {
		return svcbacktest.RunResponse{}, nil, err
	}

	strat, err := smatrend.New(simID, marketdata.D1, cfg)
	if err != nil {
		return svcbacktest.RunResponse{}, nil, err
	}

	resp, err := svc.Run(ctx, svcbacktest.RunRequest{
		Strategy:        strat,
		Span:            span,
		StartingCapital: startingCapital,
		RiskFraction:    riskFraction,
		AdverseDistance: adverseDistance,
	})
	return resp, rec, err
}

// tradeLogRow is one out-of-sample trade's own baseline record —
// intended to be diffed against a later run of the same walk-forward
// protocol once this strategy's rules are tweaked (issue #349's own
// next phase).
type tradeLogRow struct {
	TrailingStopPercent string  `json:"trailing_stop_percent"`
	Fold                int     `json:"fold"`
	TestStart           string  `json:"test_start"`
	TestEnd             string  `json:"test_end"`
	OpenedAt            string  `json:"opened_at"`
	ClosedAt            string  `json:"closed_at"` // empty if still open at this fold's own run end
	Side                string  `json:"side"`
	Quantity            string  `json:"quantity"`
	EntryPrice          string  `json:"entry_price"`
	ExitPrice           string  `json:"exit_price"` // empty if still open
	RealizedPnL         float64 `json:"realized_pnl"`
	Costs               float64 `json:"costs"`
	NetPnL              float64 `json:"net_pnl"`
	ReturnPercent       float64 `json:"return_percent"` // net PnL / (entry price * quantity), 0 if still open
	HoldingDays         float64 `json:"holding_days"`   // 0 if still open
	StillOpenAtFoldEnd  bool    `json:"still_open_at_fold_end"`
	// OOSEntry is true when OpenedAt itself falls within [testStart,
	// testEnd) — a genuine entry decision made using only
	// out-of-sample data. false means this trade was opened during
	// the fold's own training portion and carried across testStart
	// still open, contributing to this fold's own TestReturn/
	// TestMaxDrawdown (foldSummary) without itself being an
	// OOS-generated signal (PR #357 review) — still recorded here
	// (with its own real, pre-test entry date/price) so every
	// position that contributes to a fold's OOS result appears in the
	// trade log exactly once.
	OOSEntry bool `json:"oos_entry"`
}

// weightedFillPrice returns the quantity-weighted average Price
// across fillIDs (each looked up in fills), and the summed Quantity —
// used to reconstruct a Trade's own entry/exit price and size, since
// order.Trade itself only ever stores FillIDs, not price or quantity
// directly.
func weightedFillPrice(t *testing.T, fillIDs []id.FillID, fills map[id.FillID]order.Fill) (num.Price, num.Quantity, error) {
	t.Helper()
	if len(fillIDs) == 0 {
		return num.Price{}, num.Quantity{}, fmt.Errorf("no fills")
	}
	totalQty := num.Quantity{}
	notional := 0.0
	qtySum := 0.0
	for _, fid := range fillIDs {
		f, ok := fills[fid]
		if !ok {
			return num.Price{}, num.Quantity{}, fmt.Errorf("fill %s not found in journal", fid)
		}
		var err error
		totalQty, err = totalQty.Add(f.Quantity)
		if err != nil {
			return num.Price{}, num.Quantity{}, err
		}
		// num.Quantity has no Float64() conversion method (unlike
		// num.Price, ADR-045) — this is test-only research code
		// reconstructing a display quantity for the CSV trade log,
		// not a value that ever re-enters the exact/order domain, so
		// a String()/ParseFloat round-trip is an acceptable, narrowly
		// scoped exception here.
		qFloat, err := strconv.ParseFloat(f.Quantity.String(), 64)
		if err != nil {
			return num.Price{}, num.Quantity{}, err
		}
		notional += f.Price.Float64() * qFloat
		qtySum += qFloat
	}
	if qtySum == 0 {
		return num.Price{}, num.Quantity{}, fmt.Errorf("zero total quantity")
	}
	avgPrice, err := num.ParsePrice(strconv.FormatFloat(notional/qtySum, 'f', 8, 64))
	if err != nil {
		return num.Price{}, num.Quantity{}, err
	}
	return avgPrice, totalQty, nil
}

// TestSMATrendProbationTrendWalkForwardBaseline runs strategy/
// smatrend's "probation-trend" ExitRule through the exact same
// walk-forward window protocol eqs01_walkforward_fullarchive_test.go
// established (5-year/1260-bar train, 1-year/252-bar test, 1-year/
// 252-bar step — no overlap between test windows) against SPY's full
// Stooq history, once at TrailingStopPercent 0.10 and once at 0.20.
// Unlike that file's own sweep, there is no grid search or selection
// here: SMAPeriod (200), InitialStopBelowSMA (0.01), TrailActivationGain
// (0.05), ReEntryRuleName ("reclaim-exit-price"), and
// TrailingStopPercent are all fixed inputs to every fold.
//
// Sizing is full notional (RiskFraction=1, AdverseDistance=100% of
// price), re-anchored once per fold from that fold's own train-start
// price — the same technique eqs01WFSizingModes's "full-notional"
// mode already established, needed for the same reason recorded
// there and in the simple single-run comparison
// (smatrend_probation_simple_fullarchive_test.go): an anchor frozen
// once for a multi-decade span drifts too far from later prices.
//
// Every trade overlapping that specific fold's own [testStart,
// testEnd) OOS window — OpenedAt before testEnd, and either still
// open or ClosedAt at/after testStart — is written to a CSV trade
// log, so every position contributing to a fold's own TestReturn/
// TestMaxDrawdown is represented exactly once (PR #357 review). The
// tradeLogRow.OOSEntry column distinguishes the two cases this
// predicate admits: true for a genuine entry decision made during
// OOS (OpenedAt itself falls within the window), false for a
// position opened during the fold's own training portion and simply
// carried open across testStart, recorded with its own real,
// pre-test entry date/price rather than pretending it was an
// OOS-generated signal.
//
// Known limitation, the same shape as eqs01WFFoldResult's own
// documented SMA-warmup asymmetry: a trade opened very close to a
// fold's own testEnd may not close before that fold's run ends; it
// is recorded as still-open for that fold and is independently
// re-entered and evaluated as part of the *next* fold's own training
// window, but since its own OpenedAt then falls in that next fold's
// train portion (not test), it is not recorded a second time —
// meaning a small number of trades that span a fold boundary may
// appear in this log as "still open" without their own eventual
// close ever being recorded. This is judged an acceptable limitation
// for a baseline trade log (not a rigorous, gap-free trade census);
// flagged explicitly rather than silently accepted.
func TestSMATrendProbationTrendWalkForwardBaseline(t *testing.T) {
	if fullArchiveSMATrendWFOutputDir == "" {
		t.Skip("fullArchiveSMATrendWFOutputDir is empty; edit the constant in this file to point at a local results directory to run this test")
	}
	if fullArchiveEQS01SPYCSVPath == "" {
		t.Skip("fullArchiveEQS01SPYCSVPath is empty; edit the constant in eqs01_walkforward_fullarchive_test.go to point at a local Stooq SPY export to run this test")
	}
	if err := os.MkdirAll(fullArchiveSMATrendWFOutputDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", fullArchiveSMATrendWFOutputDir, err)
	}

	ctx := context.Background()
	inst := eqs01WFInstrument{Symbol: "SPY", CSVPath: fullArchiveEQS01SPYCSVPath, Exchange: "ARCA"}
	setup := setupEQS01WalkForwardInstrument(t, ctx, inst)

	startingCapital := num.MustParseMoney(eqs01WFStartingCapital, num.MustParseCurrency("USD"))
	riskFraction := num.MustParseRate("1") // full notional

	var allTrades []tradeLogRow
	type foldSummary struct {
		TrailingStopPercent string  `json:"trailing_stop_percent"`
		Fold                int     `json:"fold"`
		TestStart           string  `json:"test_start"`
		TestEnd             string  `json:"test_end"`
		TestReturn          float64 `json:"test_return"`
		TestMaxDrawdown     float64 `json:"test_max_drawdown"`
	}
	var allFolds []foldSummary
	var wfSpanStart, wfSpanEnd time.Time // the overall [first fold's testStart, last fold's testEnd) span, tracked below

	for _, tsp := range []string{"0.10", "0.20"} {
		cfg := smatrend.Config{
			SMAPeriod:           200,
			ExitRuleName:        "probation-trend",
			ReEntryRuleName:     "reclaim-exit-price",
			InitialStopBelowSMA: num.MustParseRate("0.01"),
			TrailActivationGain: num.MustParseRate("0.05"),
			TrailingStopPercent: num.MustParseRate(tsp),
		}

		numFolds := (len(setup.bars) - eqs01WFTrainBars) / eqs01WFStepBars
		for i := 0; i < numFolds; i++ {
			trainStartIdx := i * eqs01WFStepBars
			testStartIdx := trainStartIdx + eqs01WFTrainBars
			testEndIdxExclusive := testStartIdx + eqs01WFTestBars
			if testEndIdxExclusive > len(setup.bars) {
				break
			}

			trainStart := setup.bars[trainStartIdx].Time
			testStart := setup.bars[testStartIdx].Time
			var testEndExclusive time.Time
			if testEndIdxExclusive < len(setup.bars) {
				testEndExclusive = setup.bars[testEndIdxExclusive].Time
			} else {
				testEndExclusive = setup.bars[len(setup.bars)-1].Time.AddDate(0, 0, 1)
			}

			combinedSpan, err := marketdata.NewTimeRange(trainStart, testEndExclusive)
			if err != nil {
				t.Fatalf("tsp=%s fold %d: combined span: %v", tsp, i, err)
			}
			adverseDistance, err := setup.bars[trainStartIdx].Open.MulRate(num.MustParseRate("1"))
			if err != nil {
				t.Fatalf("tsp=%s fold %d: adverse distance: %v", tsp, i, err)
			}

			resp, rec, err := runWFBacktestWithJournal(ctx, setup.mgr, setup.simResolver, setup.simID, combinedSpan, cfg, startingCapital, riskFraction, adverseDistance, setup.priceByTime)
			if err != nil {
				t.Fatalf("tsp=%s fold %d [%s, %s): backtest run: %v", tsp, i, testStart.Format("2006-01-02"), testEndExclusive.Format("2006-01-02"), err)
			}
			fills := rec.fillsByID()

			baselineEquity, ok1 := equityCurveAt(resp.EquityCurve, testStart)
			finalTime := setup.bars[testEndIdxExclusive-1].Time
			finalEquity, ok2 := equityCurveAt(resp.EquityCurve, finalTime)
			if !ok1 || !ok2 {
				t.Fatalf("tsp=%s fold %d: could not locate equity-curve points at fold boundaries", tsp, i)
			}
			testReturn := finalEquity/baselineEquity - 1
			testMaxDD := maxDrawdownSince(resp.EquityCurve, testStart)
			allFolds = append(allFolds, foldSummary{
				TrailingStopPercent: tsp, Fold: i,
				TestStart: testStart.Format("2006-01-02"), TestEnd: testEndExclusive.Format("2006-01-02"),
				TestReturn: testReturn, TestMaxDrawdown: testMaxDD,
			})

			allTrades = append(allTrades, foldTradeRows(t, tsp, i, testStart, testEndExclusive, resp, fills)...)

			if wfSpanStart.IsZero() || testStart.Before(wfSpanStart) {
				wfSpanStart = testStart
			}
			if testEndExclusive.After(wfSpanEnd) {
				wfSpanEnd = testEndExclusive
			}
		}
	}

	writeTradeLogCSV(t, filepath.Join(fullArchiveSMATrendWFOutputDir, "spy-probation-trend-walkforward-trades.csv"), allTrades)

	// Buy-and-hold benchmark over the identical walk-forward-tested
	// span (the union of every fold's own [testStart, testEnd) test
	// window — the strategy is never evaluated outside this range, so
	// comparing against buy-and-hold over any wider span would not be
	// a fair comparison). Unlike the strategy's own per-fold
	// MaxDrawdown (a floor, not continuous — see foldSummary's own
	// TestMaxDrawdown and this test's doc comment), this is a true,
	// single-pass, continuous peak-to-trough figure computed directly
	// from the real daily Close series, since buy-and-hold is not a
	// risk-sized, fold-chained product at all.
	bhStartBar, ok := firstBarAtOrAfter(setup.bars, wfSpanStart)
	if !ok {
		t.Fatalf("no bar found at or after walk-forward span start %s", wfSpanStart.Format("2006-01-02"))
	}
	bhEndBar := lastBarBefore(setup.bars, wfSpanEnd)
	years := bhEndBar.Time.Sub(bhStartBar.Time).Hours() / 24 / 365.25
	bhReturn := bhEndBar.Close.Float64()/bhStartBar.Open.Float64() - 1
	bhCAGR := cagrFromReturn(bhReturn, years)
	bhMaxDD := 0.0
	peak := 0.0
	for _, b := range setup.bars {
		if b.Time.Before(wfSpanStart) || !b.Time.Before(wfSpanEnd) {
			continue
		}
		c := b.Close.Float64()
		if c > peak {
			peak = c
		}
		if peak > 0 {
			if dd := (peak - c) / peak; dd > bhMaxDD {
				bhMaxDD = dd
			}
		}
	}
	type buyAndHoldSummary struct {
		SpanStart   string  `json:"span_start"`
		SpanEnd     string  `json:"span_end"`
		Years       float64 `json:"years"`
		NetReturn   float64 `json:"net_return"`
		CAGR        float64 `json:"cagr"`
		MaxDrawdown float64 `json:"max_drawdown"`
		Calmar      float64 `json:"calmar"`
	}
	bh := buyAndHoldSummary{
		SpanStart: bhStartBar.Time.Format("2006-01-02"), SpanEnd: bhEndBar.Time.Format("2006-01-02"),
		Years: years, NetReturn: bhReturn, CAGR: bhCAGR, MaxDrawdown: bhMaxDD, Calmar: calmarRatio(bhCAGR, bhMaxDD),
	}
	t.Logf("SPY buy-and-hold [%s, %s]: return=%.4f cagr=%.4f maxDD=%.4f calmar=%.4f",
		bh.SpanStart, bh.SpanEnd, bhReturn, bhCAGR, bhMaxDD, bh.Calmar)
	bhOut, err := json.MarshalIndent(bh, "", "  ")
	if err != nil {
		t.Fatalf("marshal buy-and-hold summary: %v", err)
	}
	bhPath := filepath.Join(fullArchiveSMATrendWFOutputDir, "spy-buyhold-benchmark.json")
	if err := os.WriteFile(bhPath, bhOut, 0o644); err != nil {
		t.Fatalf("write %s: %v", bhPath, err)
	}

	foldsOut, err := json.MarshalIndent(allFolds, "", "  ")
	if err != nil {
		t.Fatalf("marshal fold summaries: %v", err)
	}
	foldsPath := filepath.Join(fullArchiveSMATrendWFOutputDir, "spy-probation-trend-walkforward-folds.json")
	if err := os.WriteFile(foldsPath, foldsOut, 0o644); err != nil {
		t.Fatalf("write %s: %v", foldsPath, err)
	}

	t.Logf("wrote %d trades and %d folds to %s", len(allTrades), len(allFolds), fullArchiveSMATrendWFOutputDir)
}

// foldTradeRows extracts resp.Trades (closed) and resp.OpenTrades
// (still open at this fold's own run end) whose OpenedAt falls within
// [testStart, testEnd) — see TestSMATrendProbationTrendWalkForwardBaseline's
// own doc comment for exactly what this does and does not capture.
func foldTradeRows(t *testing.T, tsp string, fold int, testStart, testEnd time.Time, resp svcbacktest.RunResponse, fills map[id.FillID]order.Fill) []tradeLogRow {
	t.Helper()
	var rows []tradeLogRow

	appendRow := func(tr order.Trade, stillOpen bool) {
		// Overlap, not "opened during OOS" (PR #357 review): a trade
		// opened during this fold's own training portion but carried
		// open across testStart contributes real PnL/drawdown to this
		// fold's own TestReturn/TestMaxDrawdown (the strategy runs
		// continuously through train+test) and must be represented
		// here too, or the trade log would silently fail to explain
		// part of the fold's own reported OOS result. A trade that
		// opened AND fully closed entirely within training never
		// touches the test window at all and is correctly excluded.
		if !tr.OpenedAt.Before(testEnd) {
			return
		}
		if !stillOpen && tr.ClosedAt.Before(testStart) {
			return
		}
		entryPrice, qty, err := weightedFillPrice(t, tr.EntryFillIDs, fills)
		if err != nil {
			t.Fatalf("tsp=%s fold %d: entry fill lookup for trade opened %s: %v", tsp, fold, tr.OpenedAt, err)
		}

		row := tradeLogRow{
			TrailingStopPercent: tsp,
			Fold:                fold,
			TestStart:           testStart.Format("2006-01-02"),
			TestEnd:             testEnd.Format("2006-01-02"),
			OpenedAt:            tr.OpenedAt.Format("2006-01-02"),
			Side:                tr.Side.String(),
			Quantity:            qty.String(),
			EntryPrice:          entryPrice.String(),
			RealizedPnL:         mustParseFloatWF(t, fieldsFirst(tr.RealizedPnL.String())),
			Costs:               mustParseFloatWF(t, fieldsFirst(tr.Costs.String())),
			StillOpenAtFoldEnd:  stillOpen,
			OOSEntry:            !tr.OpenedAt.Before(testStart),
		}
		row.NetPnL = row.RealizedPnL - row.Costs

		if !stillOpen {
			exitPrice, _, err := weightedFillPrice(t, tr.ExitFillIDs, fills)
			if err != nil {
				t.Fatalf("tsp=%s fold %d: exit fill lookup for trade opened %s: %v", tsp, fold, tr.OpenedAt, err)
			}
			row.ClosedAt = tr.ClosedAt.Format("2006-01-02")
			row.ExitPrice = exitPrice.String()
			row.HoldingDays = tr.ClosedAt.Sub(tr.OpenedAt).Hours() / 24
			qtyFloat, err := strconv.ParseFloat(qty.String(), 64)
			if err != nil {
				t.Fatalf("tsp=%s fold %d: parsing quantity for return-percent: %v", tsp, fold, err)
			}
			notional := entryPrice.Float64() * qtyFloat
			if notional != 0 {
				row.ReturnPercent = row.NetPnL / notional
			}
		}

		rows = append(rows, row)
	}

	for _, tr := range resp.Trades {
		appendRow(tr, false)
	}
	for _, tr := range resp.OpenTrades {
		appendRow(tr, true)
	}
	return rows
}

// writeTradeLogCSV writes rows to path, checking every failure mode a
// durable research artifact needs to actually surface (PR #357
// review): Write, Flush (via w.Error(), the only way encoding/csv
// reports a flush-time failure), and Close are all checked explicitly
// — none of gofmt's usual io.Writer/io.Closer shortcuts that silently
// swallow a write or close error, which for a file this test's own
// caller treats as the durable baseline could otherwise corrupt or
// truncate it without any test failure ever reporting that.
func writeTradeLogCSV(t *testing.T, path string, rows []tradeLogRow) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create %s: %v", path, err)
	}

	w := csv.NewWriter(f)

	header := []string{
		"trailing_stop_percent", "fold", "test_start", "test_end",
		"opened_at", "closed_at", "side", "quantity",
		"entry_price", "exit_price", "realized_pnl", "costs", "net_pnl",
		"return_percent", "holding_days", "still_open_at_fold_end", "oos_entry",
	}
	if err := w.Write(header); err != nil {
		_ = f.Close()
		t.Fatalf("write csv header: %v", err)
	}
	for _, r := range rows {
		record := []string{
			r.TrailingStopPercent, strconv.Itoa(r.Fold), r.TestStart, r.TestEnd,
			r.OpenedAt, r.ClosedAt, r.Side, r.Quantity,
			r.EntryPrice, r.ExitPrice,
			strconv.FormatFloat(r.RealizedPnL, 'f', 2, 64),
			strconv.FormatFloat(r.Costs, 'f', 2, 64),
			strconv.FormatFloat(r.NetPnL, 'f', 2, 64),
			strconv.FormatFloat(r.ReturnPercent, 'f', 6, 64),
			strconv.FormatFloat(r.HoldingDays, 'f', 1, 64),
			strconv.FormatBool(r.StillOpenAtFoldEnd),
			strconv.FormatBool(r.OOSEntry),
		}
		if err := w.Write(record); err != nil {
			_ = f.Close()
			t.Fatalf("write csv row: %v", err)
		}
	}

	w.Flush()
	if err := w.Error(); err != nil {
		_ = f.Close()
		t.Fatalf("flush csv %s: %v", path, err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close csv %s: %v", path, err)
	}
}
