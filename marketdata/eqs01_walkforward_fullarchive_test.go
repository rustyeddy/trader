//go:build fullarchive

// This file is excluded from normal `go test ./...` / `make check` by
// the fullarchive build tag, mirroring eqr01c_devrun_test.go and
// eqr01d_validationrun_test.go exactly: it imports real Stooq exports
// and runs a true walk-forward parameter-optimization sweep for
// strategy/smatrend (issue #335, EQS-01's own follow-up experiment
// phase) — an operator action producing durable research artifacts,
// not a CI assertion.
//
// This lives in the marketdata_test external test package, not
// marketdata's own internal test package, for the identical reason
// eqr01c_devrun_test.go does: this file needs strategy/smatrend,
// backtest, service/backtest, execution, risk, pipeline, and
// adapters/broker/sim, none of which marketdata itself may import
// (they would all become import cycles from inside package
// marketdata), while remaining inside the marketdata/ directory tree
// so it can still reach marketdata/internal/provider/stooq directly —
// the one internal import genuinely needed to turn a raw Stooq CSV
// export into the raw-partition layout Manager.Plan/Build expect.
//
// # Why a Go test, not a research-runs/ program
//
// stooq.Import lives in marketdata/internal/ and is deliberately not a
// public Manager operation (ADR-020's "only Manager may invoke
// acquisition/build" boundary) — a research-runs/ program outside this
// module's own internal-package visibility cannot call it. Living
// under marketdata/ as an external test package is how EQR-01's own
// research (eqr01c/eqr01d) already solved the identical problem; this
// file follows that established precedent rather than reinventing a
// workaround.
//
// # Where results go
//
// Per an explicit decision (not a technical constraint): the strategy
// and sweep-driver *code* here lives in this public, open-source
// repository, but the *results* — real historical trading performance
// data — are written to a local, private directory
// (fullArchiveEQS01OutputDir, edited locally, never committed), not
// anywhere under this repository. See docs/research/
// eqs-01-baseline-sma-trend.org for the reasoning already recorded
// when this same question came up for EQS-01's own initial backtest.
package marketdata_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"testing"
	"time"

	simbroker "github.com/rustyeddy/trader/adapters/broker/sim"
	"github.com/rustyeddy/trader/backtest"
	"github.com/rustyeddy/trader/clock"
	"github.com/rustyeddy/trader/execution"
	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/marketdata/internal/provider/stooq"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
	"github.com/rustyeddy/trader/pipeline"
	"github.com/rustyeddy/trader/risk"
	svcbacktest "github.com/rustyeddy/trader/service/backtest"
	svcmarketdata "github.com/rustyeddy/trader/service/marketdata"
	"github.com/rustyeddy/trader/strategy/smatrend"
)

// fullArchiveEQS01SPYCSVPath/QQQCSVPath name the real Stooq native
// daily-history CSV exports this test imports — empty by default,
// matching every other fullarchive constant in this codebase (an
// operator edits these locally, for example to
// "/srv/trading/data/raw/stooq/spy_us_d.csv", and never commits the
// edit).
//
//	go test -tags fullarchive ./marketdata/... -run TestEQS01WalkForward -v
const fullArchiveEQS01SPYCSVPath = ""
const fullArchiveEQS01QQQCSVPath = ""

// fullArchiveEQS01OutputDir names the local directory this test writes
// its results (per-instrument JSON, a combined write-up) into — empty
// by default; an operator edits this locally too (for example to
// "/srv/trading/sweeps/eqs01-walkforward"). Results are never written
// anywhere under this repository — see this file's own doc comment.
const fullArchiveEQS01OutputDir = ""

// eqs01WFUSEquityCalendarYears spans every year any Stooq export this
// test might read could plausibly cover, matching eqr01CUSEquityCalendarYears'
// own "configure for the full possible range, not just what one run
// happens to query" reasoning.
func eqs01WFUSEquityCalendarYears() []int {
	years := make([]int, 0, 40)
	for y := 1995; y <= 2026; y++ {
		years = append(years, y)
	}
	return years
}

// Walk-forward protocol constants, frozen before this test was ever
// run against real data (see the message thread that authorized this
// experiment): 5-year (1260 trading-day) train windows, 1-year
// (252 trading-day) test windows, rolled forward in 252-trading-day
// steps (i.e. test windows never overlap — every trading day is
// evaluated out-of-sample exactly once). Bar-count windows, not
// calendar-date windows, so every fold boundary lands exactly on a
// real trading day regardless of weekends/holidays.
const (
	eqs01WFTrainBars = 1260
	eqs01WFTestBars  = 252
	eqs01WFStepBars  = 252

	eqs01WFStartingCapital  = "100000"
	eqs01WFMinDrawdownFloor = 0.01 // Calmar-ratio denominator floor, avoiding a near-zero-drawdown blowup.
)

// eqs01WFSizingMode is one (RiskFraction, AdversePercent) sizing
// policy this sweep runs the full walk-forward protocol under.
// risk.FixedFractionSizer computes quantity as (equity x
// RiskFraction) / (AdverseDistance x multiplier), so notional exposure
// when a position is open works out to equity x (RiskFraction /
// AdversePercent) — a Sizer never sees the current market price
// directly (risk.SizeInput carries no price field at all, by design:
// sizing is meant to be a pure function of equity/risk-fraction/stop-
// distance, not of price), so "full notional" exposure is reached
// indirectly, by choosing RiskFraction and AdversePercent whose ratio
// is 1.0, rather than by writing a second Sizer implementation.
//
// riskManaged is EQS-01's own original, non-optimized reference
// sizing (1% risk, 5% assumed adverse distance) — deliberately
// conservative, and deliberately not comparable to a 100%-invested
// buy-and-hold baseline on its own (a real user asked exactly this
// question after the first version of this sweep's results: "are
// these using the same size lots?" — they were not). fullNotional
// targets the same ~100% equity notional deployment buy-and-hold
// itself uses whenever a position is open, making that comparison
// fair; it is not a realistic trading configuration (a 100%-of-equity
// single-instrument position with no assumed stop distance for sizing
// purposes is not something a real account should run), only a
// deliberately constructed baseline for this one comparison.
type eqs01WFSizingMode struct {
	Name           string
	RiskFraction   num.Rate
	AdversePercent float64
}

var eqs01WFSizingModes = []eqs01WFSizingMode{
	{Name: "risk-managed", RiskFraction: num.MustParseRate("0.01"), AdversePercent: 0.05},
	{Name: "full-notional", RiskFraction: num.MustParseRate("1"), AdversePercent: 1.0},
}

// eqs01WFGridSMAPeriods/TrailingStopPercents are the frozen 6x5 = 30
// combination parameter grid, exactly as authorized: SMAPeriod and
// TrailingStopPercent, the two parameters EQS-01 (issue #335) itself
// explicitly left untuned.
var eqs01WFGridSMAPeriods = []int{50, 100, 150, 200, 250, 300}
var eqs01WFGridTrailingStopPercents = []string{"0.05", "0.075", "0.10", "0.15", "0.20"}

// eqs01WFInstrument is one instrument this sweep runs against.
type eqs01WFInstrument struct {
	Symbol   string
	CSVPath  string
	Exchange string // real listing exchange (ADR-047's own identity requirement)
}

var eqs01WFInstruments = []eqs01WFInstrument{
	{Symbol: "SPY", CSVPath: fullArchiveEQS01SPYCSVPath, Exchange: "ARCA"},
	{Symbol: "QQQ", CSVPath: fullArchiveEQS01QQQCSVPath, Exchange: "NASDAQ"},
}

// eqs01WFFoldResult is one instrument's one walk-forward fold: the
// in-sample (train) parameter selection and the resulting genuinely
// out-of-sample (test) result.
type eqs01WFFoldResult struct {
	Instrument string `json:"instrument"`
	Fold       int    `json:"fold"`

	TrainStart time.Time `json:"train_start"`
	TrainEnd   time.Time `json:"train_end"` // exclusive; equals TestStart
	TestStart  time.Time `json:"test_start"`
	TestEnd    time.Time `json:"test_end"` // exclusive

	SelectedSMAPeriod           int     `json:"selected_sma_period"`
	SelectedTrailingStopPercent string  `json:"selected_trailing_stop_percent"`
	TrainCAGR                   float64 `json:"train_cagr"`
	TrainMaxDrawdown            float64 `json:"train_max_drawdown"`
	TrainCalmar                 float64 `json:"train_calmar"`

	TestReturn      float64 `json:"test_return"`
	TestCAGR        float64 `json:"test_cagr"`
	TestMaxDrawdown float64 `json:"test_max_drawdown"`
	TestTradeCount  int     `json:"test_trade_count"`
}

// eqs01WFInstrumentSummary aggregates one instrument's own chained,
// genuinely out-of-sample equity curve across every fold, plus the
// buy-and-hold comparison over the identical walk-forward-tradeable
// span (from the first fold's own TestStart onward — the walk-forward
// process cannot trade before it has a first trained parameter
// choice, so comparing against buy-and-hold over the *full* history
// including the training-only prefix would not be a fair comparison).
type eqs01WFInstrumentSummary struct {
	Instrument       string              `json:"instrument"`
	SizingMode       string              `json:"sizing_mode"`
	Folds            []eqs01WFFoldResult `json:"folds"`
	WalkForwardStart time.Time           `json:"walk_forward_start"`
	WalkForwardEnd   time.Time           `json:"walk_forward_end"`
	ChainedOOSReturn float64             `json:"chained_oos_return"`
	ChainedOOSCAGR   float64             `json:"chained_oos_cagr"`
	BuyAndHoldReturn float64             `json:"buy_and_hold_return"`
	BuyAndHoldCAGR   float64             `json:"buy_and_hold_cagr"`
}

func cagrFromReturn(netReturn, years float64) float64 {
	if years <= 0 {
		return 0
	}
	base := 1 + netReturn
	if base <= 0 {
		return -1
	}
	return math.Pow(base, 1/years) - 1
}

func calmarRatio(cagr, maxDrawdown float64) float64 {
	dd := maxDrawdown
	if dd < eqs01WFMinDrawdownFloor {
		dd = eqs01WFMinDrawdownFloor
	}
	return cagr / dd
}

func mustParseFloatWF(t *testing.T, s string) float64 {
	t.Helper()
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		t.Fatalf("parse float %q: %v", s, err)
	}
	return f
}

// eqs01WFPriceSource is a real (not fixed-value) simbroker.
// FillPriceSource, keyed by a clock.Simulated: for whatever instant
// clock currently reports, it returns that instant's own canonical bar
// Open, unrounded. Real split-adjusted historical equity data can
// legitimately carry sub-cent precision (ADR-052), but this source
// deliberately does not pre-round it: sim.Broker's own buildFill
// already rounds every fill price to the listing's tick size,
// conservatively and side-dependent (ADR-057) — Buy rounds up, Sell
// rounds down. Pre-rounding down here unconditionally, regardless of
// side, would make a Buy fill non-conservative (PR #346 review): the
// broker's own RoundUp would then be a no-op against an
// already-floored value, silently filling every Buy at a price at or
// below the true one instead of at or above it.
type eqs01WFPriceSource struct {
	clock *clock.Simulated
	bars  map[time.Time]marketdata.Bar
}

func (s *eqs01WFPriceSource) Info() simbroker.ModelInfo {
	return simbroker.ModelInfo{Name: "next-bar-open-lookup", Version: "eqs01-walkforward"}
}

func (s *eqs01WFPriceSource) Price(listing instrument.Listing, side order.Side) (num.Price, error) {
	now := s.clock.Now()
	bar, ok := s.bars[now]
	if !ok {
		return num.Price{}, fmt.Errorf("no canonical bar for %s at %s", listing.Symbol(), now)
	}
	return bar.Open, nil
}

// eqs01WFEnvironmentFactory builds the real M4 pipeline (fixed-
// fraction sizing, execution.Planner, risk.Engine, simulated broker)
// against prices — no strategy-specific execution wiring — mirroring
// strategy/emacross/execution_test.go's and research-runs/eqs-01's own
// identical composition.
type eqs01WFEnvironmentFactory struct {
	prices map[time.Time]marketdata.Bar
}

func (f eqs01WFEnvironmentFactory) NewEnvironment(ctx context.Context, req svcbacktest.EnvironmentRequest) (svcbacktest.Environment, error) {
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
		FillModel:       fill,
		SlippageModel:   none,
		CommissionModel: none,
	}, nil
}

// TestEQS01WalkForward runs the frozen walk-forward parameter-
// optimization protocol for strategy/smatrend against real Stooq SPY
// and QQQ D1 history: for each instrument, roll a 5-year train / 1-
// year test window forward in 1-year steps; on each train window, grid
// search all 30 (SMAPeriod, TrailingStopPercent) combinations and
// select the one with the highest Calmar ratio (CAGR / max drawdown,
// floored to avoid a near-zero-drawdown blowup); apply that one
// selected combination, untouched, to the immediately following test
// window (genuinely out-of-sample); chain every fold's own test-window
// return into one aggregate walk-forward equity curve; compare against
// buy-and-hold over the identical walk-forward-tradeable span.
func TestEQS01WalkForward(t *testing.T) {
	if fullArchiveEQS01OutputDir == "" {
		t.Skip("fullArchiveEQS01OutputDir is empty; edit the constant in this file to point at a local results directory to run this test")
	}
	if err := os.MkdirAll(fullArchiveEQS01OutputDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", fullArchiveEQS01OutputDir, err)
	}

	ctx := context.Background()
	var summaries []eqs01WFInstrumentSummary

	for _, inst := range eqs01WFInstruments {
		if inst.CSVPath == "" {
			t.Logf("skipping %s: CSV path constant is empty", inst.Symbol)
			continue
		}
		if info, err := os.Stat(inst.CSVPath); err != nil || info.IsDir() {
			t.Logf("skipping %s: %q is not a readable file: %v", inst.Symbol, inst.CSVPath, err)
			continue
		}

		setup := setupEQS01WalkForwardInstrument(t, ctx, inst)
		for _, mode := range eqs01WFSizingModes {
			summary := runEQS01WalkForwardFolds(t, ctx, inst, setup, mode)
			summaries = append(summaries, summary)

			out, err := json.MarshalIndent(summary, "", "  ")
			if err != nil {
				t.Fatalf("marshal %s/%s summary: %v", inst.Symbol, mode.Name, err)
			}
			outPath := filepath.Join(fullArchiveEQS01OutputDir, inst.Symbol+"-"+mode.Name+"-walkforward.json")
			if err := os.WriteFile(outPath, out, 0o644); err != nil {
				t.Fatalf("write %s: %v", outPath, err)
			}
			t.Logf("%s/%s: wrote %d folds to %s", inst.Symbol, mode.Name, len(summary.Folds), outPath)
			t.Logf("%s/%s: chained OOS return=%.2f%% CAGR=%.2f%% vs buy-and-hold return=%.2f%% CAGR=%.2f%%",
				inst.Symbol, mode.Name, summary.ChainedOOSReturn*100, summary.ChainedOOSCAGR*100,
				summary.BuyAndHoldReturn*100, summary.BuyAndHoldCAGR*100)
		}
	}

	if len(summaries) == 0 {
		t.Skip("no instrument had both a CSV path and output directory configured")
	}
}

// eqs01WFSetup holds one instrument's own canonical data and broker-
// side registration, built once and reused across every sizing
// mode's own full walk-forward run — none of this setup is sizing-
// mode-dependent, so repeating it per mode would just re-import and
// re-build the identical canonical data.
type eqs01WFSetup struct {
	mgr         *marketdata.Manager
	simResolver instrument.Resolver
	simID       instrument.ID
	bars        []marketdata.Bar
	priceByTime map[time.Time]marketdata.Bar
}

func setupEQS01WalkForwardInstrument(t *testing.T, ctx context.Context, inst eqs01WFInstrument) eqs01WFSetup {
	t.Helper()

	rawRoot := t.TempDir()
	importResult, err := stooq.Import(ctx, inst.CSVPath, rawRoot, inst.Symbol)
	if err != nil {
		t.Fatalf("%s: Import: %v", inst.Symbol, err)
	}
	t.Logf("%s: imported %d rows, %s -> %s", inst.Symbol, importResult.RowsImported,
		importResult.FirstDate.Format("2006-01-02"), importResult.LastDate.Format("2006-01-02"))

	dataResolver := instrument.NewMemoryResolver()
	instID, err := svcmarketdata.RegisterETFInstrument(dataResolver, svcmarketdata.EquityRegistration{
		Provider: "stooq",
		Exchange: inst.Exchange,
		Ticker:   inst.Symbol,
		Currency: num.MustParseCurrency("USD"),
	})
	if err != nil {
		t.Fatalf("%s: RegisterETFInstrument: %v", inst.Symbol, err)
	}

	cal := marketdata.NewUSEquityCalendar(marketdata.StandardUSEquityHolidays(eqs01WFUSEquityCalendarYears()...))
	mgr, err := marketdata.New(marketdata.Config{
		Clock:        clock.Real{},
		StoreRoot:    t.TempDir(),
		RawRoot:      rawRoot,
		Resolver:     dataResolver,
		ProviderName: "stooq",
		Calendar:     cal,
	})
	if err != nil {
		t.Fatalf("%s: New manager: %v", inst.Symbol, err)
	}

	span, err := marketdata.NewTimeRange(importResult.FirstDate, importResult.LastDate.AddDate(0, 0, 1))
	if err != nil {
		t.Fatalf("%s: NewTimeRange: %v", inst.Symbol, err)
	}
	query := marketdata.BarQuery{Instrument: instID, Interval: marketdata.D1, Range: span}
	plan, err := mgr.Plan(ctx, query)
	if err != nil {
		t.Fatalf("%s: Plan: %v", inst.Symbol, err)
	}
	if len(plan.Actions) > 0 {
		if _, err := mgr.Build(ctx, plan); err != nil {
			t.Fatalf("%s: Build: %v", inst.Symbol, err)
		}
	}

	reader, err := mgr.Bars(ctx, query)
	if err != nil {
		t.Fatalf("%s: Bars: %v", inst.Symbol, err)
	}
	defer func() { _ = reader.Close() }()
	var bars []marketdata.Bar
	for {
		b, err := reader.Next(ctx)
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			t.Fatalf("%s: Bars.Next: %v", inst.Symbol, err)
		}
		bars = append(bars, b)
	}
	if len(bars) == 0 {
		t.Fatalf("%s: no bars returned", inst.Symbol)
	}
	sort.Slice(bars, func(i, j int) bool { return bars[i].Time.Before(bars[j].Time) })
	t.Logf("%s: %d canonical bars, %s -> %s", inst.Symbol, len(bars),
		bars[0].Time.Format("2006-01-02"), bars[len(bars)-1].Time.Format("2006-01-02"))

	priceByTime := make(map[time.Time]marketdata.Bar, len(bars))
	for _, b := range bars {
		priceByTime[b.Time] = b
	}

	simResolver := instrument.NewMemoryResolver()
	simID, err := svcmarketdata.RegisterETFInstrument(simResolver, svcmarketdata.EquityRegistration{
		Provider: "sim",
		Exchange: inst.Exchange,
		Ticker:   inst.Symbol,
		Currency: num.MustParseCurrency("USD"),
	})
	if err != nil {
		t.Fatalf("%s: RegisterETFInstrument (sim): %v", inst.Symbol, err)
	}
	if !simID.Equal(instID) {
		t.Fatalf("%s: sim-provider instrument ID %s does not match data-provider instrument ID %s (both registrations must resolve to the identical economic instrument)", inst.Symbol, simID, instID)
	}

	return eqs01WFSetup{mgr: mgr, simResolver: simResolver, simID: simID, bars: bars, priceByTime: priceByTime}
}

// runEQS01WalkForwardFolds runs the full walk-forward protocol for
// inst under one sizing mode, reusing setup's already-built canonical
// data. See eqs01WFSizingMode's own doc comment for why mode alone —
// not a second Sizer implementation — is what varies between a
// realistic risk-managed run and the fully-comparable-to-buy-and-hold
// one.
func runEQS01WalkForwardFolds(t *testing.T, ctx context.Context, inst eqs01WFInstrument, setup eqs01WFSetup, mode eqs01WFSizingMode) eqs01WFInstrumentSummary {
	t.Helper()
	mgr, simResolver, simID, bars, priceByTime := setup.mgr, setup.simResolver, setup.simID, setup.bars, setup.priceByTime
	label := inst.Symbol + "/" + mode.Name

	startingCapital := num.MustParseMoney(eqs01WFStartingCapital, num.MustParseCurrency("USD"))
	riskFraction := mode.RiskFraction

	var folds []eqs01WFFoldResult
	numFolds := (len(bars) - eqs01WFTrainBars) / eqs01WFStepBars

	for i := 0; i < numFolds; i++ {
		trainStartIdx := i * eqs01WFStepBars
		testStartIdx := trainStartIdx + eqs01WFTrainBars
		testEndIdxExclusive := testStartIdx + eqs01WFTestBars
		if testEndIdxExclusive > len(bars) {
			break
		}

		trainStart := bars[trainStartIdx].Time
		trainEndExclusive := bars[testStartIdx].Time // == testStart
		testStart := trainEndExclusive
		var testEndExclusive time.Time
		if testEndIdxExclusive < len(bars) {
			testEndExclusive = bars[testEndIdxExclusive].Time
		} else {
			testEndExclusive = bars[len(bars)-1].Time.AddDate(0, 0, 1)
		}

		trainSpan, err := marketdata.NewTimeRange(trainStart, trainEndExclusive)
		if err != nil {
			t.Fatalf("%s: fold %d: train NewTimeRange: %v", label, i, err)
		}
		combinedSpan, err := marketdata.NewTimeRange(trainStart, testEndExclusive)
		if err != nil {
			t.Fatalf("%s: fold %d: combined NewTimeRange: %v", label, i, err)
		}

		adverseDistance, err := bars[trainStartIdx].Open.MulRate(num.MustParseRate(fmt.Sprintf("%.8f", mode.AdversePercent)))
		if err != nil {
			t.Fatalf("%s: fold %d: computing adverse distance: %v", label, i, err)
		}

		trainYears := trainEndExclusive.Sub(trainStart).Hours() / 24 / 365.25

		bestScore := math.Inf(-1)
		var bestSMAPeriod int
		var bestTrailingStop string
		var bestTrainCAGR, bestTrainMaxDD float64

		for _, smaPeriod := range eqs01WFGridSMAPeriods {
			for _, tsp := range eqs01WFGridTrailingStopPercents {
				cfg := smatrend.Config{SMAPeriod: smaPeriod, TrailingStopPercent: num.MustParseRate(tsp)}
				resp, err := runEQS01WFBacktest(ctx, mgr, simResolver, simID, trainSpan, cfg, startingCapital, riskFraction, adverseDistance, priceByTime)
				if err != nil {
					t.Logf("%s: fold %d: train combo sma=%d tsp=%s: %v (skipped)", label, i, smaPeriod, tsp, err)
					continue
				}
				cagr := cagrFromReturn(mustParseFloatWF(t, resp.Metrics.NetReturn().String()), trainYears)
				maxDD := mustParseFloatWF(t, resp.Metrics.MaxDrawdown().String())
				score := calmarRatio(cagr, maxDD)
				if score > bestScore {
					bestScore = score
					bestSMAPeriod = smaPeriod
					bestTrailingStop = tsp
					bestTrainCAGR = cagr
					bestTrainMaxDD = maxDD
				}
			}
		}
		if bestTrailingStop == "" {
			t.Logf("%s: fold %d: every combo failed on the train window; skipping fold", label, i)
			continue
		}

		winningCfg := smatrend.Config{SMAPeriod: bestSMAPeriod, TrailingStopPercent: num.MustParseRate(bestTrailingStop)}
		resp, err := runEQS01WFBacktest(ctx, mgr, simResolver, simID, combinedSpan, winningCfg, startingCapital, riskFraction, adverseDistance, priceByTime)
		if err != nil {
			t.Logf("%s: fold %d: OOS run with winning combo sma=%d tsp=%s failed: %v (skipping fold)", label, i, bestSMAPeriod, bestTrailingStop, err)
			continue
		}

		baselineTime := bars[testStartIdx-1].Time
		finalTime := bars[testEndIdxExclusive-1].Time
		baselineEquity, ok1 := equityCurveAt(resp.EquityCurve, baselineTime)
		finalEquity, ok2 := equityCurveAt(resp.EquityCurve, finalTime)
		if !ok1 || !ok2 {
			t.Fatalf("%s: fold %d: could not locate baseline/final equity-curve points at %s/%s", label, i, baselineTime, finalTime)
		}

		testReturn := finalEquity/baselineEquity - 1
		testYears := testEndExclusive.Sub(testStart).Hours() / 24 / 365.25
		testCAGR := cagrFromReturn(testReturn, testYears)
		testMaxDD := maxDrawdownSince(resp.EquityCurve, baselineTime)

		testTrades := 0
		for _, tr := range resp.Trades {
			if !tr.OpenedAt.Before(testStart) {
				testTrades++
			}
		}

		folds = append(folds, eqs01WFFoldResult{
			Instrument:                  inst.Symbol,
			Fold:                        i,
			TrainStart:                  trainStart,
			TrainEnd:                    trainEndExclusive,
			TestStart:                   testStart,
			TestEnd:                     testEndExclusive,
			SelectedSMAPeriod:           bestSMAPeriod,
			SelectedTrailingStopPercent: bestTrailingStop,
			TrainCAGR:                   bestTrainCAGR,
			TrainMaxDrawdown:            bestTrainMaxDD,
			TrainCalmar:                 bestScore,
			TestReturn:                  testReturn,
			TestCAGR:                    testCAGR,
			TestMaxDrawdown:             testMaxDD,
			TestTradeCount:              testTrades,
		})
		t.Logf("%s: fold %d [%s,%s)->[%s,%s): selected sma=%d tsp=%s (train CAGR=%.2f%% maxDD=%.2f%% calmar=%.3f) -> OOS return=%.2f%% CAGR=%.2f%% maxDD=%.2f%% trades=%d",
			label, i,
			trainStart.Format("2006-01-02"), trainEndExclusive.Format("2006-01-02"),
			testStart.Format("2006-01-02"), testEndExclusive.Format("2006-01-02"),
			bestSMAPeriod, bestTrailingStop, bestTrainCAGR*100, bestTrainMaxDD*100, bestScore,
			testReturn*100, testCAGR*100, testMaxDD*100, testTrades)
	}

	if len(folds) == 0 {
		t.Fatalf("%s: no folds completed", label)
	}

	chainedReturn := 1.0
	for _, f := range folds {
		chainedReturn *= 1 + f.TestReturn
	}
	chainedReturn -= 1
	wfStart := folds[0].TestStart
	wfEnd := folds[len(folds)-1].TestEnd
	wfYears := wfEnd.Sub(wfStart).Hours() / 24 / 365.25
	chainedCAGR := cagrFromReturn(chainedReturn, wfYears)

	// The buy-and-hold comparison must cover exactly the same span the
	// chained walk-forward curve does — the last bar *strictly before*
	// wfEnd (the final fold's own TestEnd, half-open), not the
	// dataset's own absolute last bar, which can extend past wfEnd
	// whenever leftover bars remain after the last complete fold (PR
	// #346 review).
	bhStartPrice, ok1 := priceByTime[wfStart]
	if !ok1 {
		t.Fatalf("%s: no canonical bar at walk-forward start %s", label, wfStart)
	}
	lastIdx := sort.Search(len(bars), func(i int) bool { return !bars[i].Time.Before(wfEnd) }) - 1
	if lastIdx < 0 {
		t.Fatalf("%s: no canonical bar before walk-forward end %s", label, wfEnd)
	}
	bhEndBar := bars[lastIdx]
	bhReturn := mustParseFloatWF(t, bhEndBar.Close.String())/mustParseFloatWF(t, bhStartPrice.Close.String()) - 1
	bhYears := wfEnd.Sub(wfStart).Hours() / 24 / 365.25
	bhCAGR := cagrFromReturn(bhReturn, bhYears)

	return eqs01WFInstrumentSummary{
		Instrument:       inst.Symbol,
		SizingMode:       mode.Name,
		Folds:            folds,
		WalkForwardStart: wfStart,
		WalkForwardEnd:   wfEnd,
		ChainedOOSReturn: chainedReturn,
		ChainedOOSCAGR:   chainedCAGR,
		BuyAndHoldReturn: bhReturn,
		BuyAndHoldCAGR:   bhCAGR,
	}
}

// runEQS01WFBacktest runs one smatrend backtest over span with cfg,
// via the real service/backtest.Service composition path.
func runEQS01WFBacktest(ctx context.Context, mgr *marketdata.Manager, simResolver instrument.Resolver, simID instrument.ID,
	span marketdata.TimeRange, cfg smatrend.Config, startingCapital num.Money, riskFraction num.Rate, adverseDistance num.Price,
	priceByTime map[time.Time]marketdata.Bar) (svcbacktest.RunResponse, error) {

	factory := eqs01WFEnvironmentFactory{prices: priceByTime}
	svc, err := svcbacktest.New(mgr, simResolver, factory, nil)
	if err != nil {
		return svcbacktest.RunResponse{}, err
	}

	strat, err := smatrend.New(simID, marketdata.D1, cfg)
	if err != nil {
		return svcbacktest.RunResponse{}, err
	}

	return svc.Run(ctx, svcbacktest.RunRequest{
		Strategy:        strat,
		Span:            span,
		StartingCapital: startingCapital,
		RiskFraction:    riskFraction,
		AdverseDistance: adverseDistance,
	})
}

// equityCurveAt returns the EquityCurve point exactly at t, if any.
func equityCurveAt(curve []backtest.EquityPoint, t time.Time) (float64, bool) {
	for _, p := range curve {
		if p.Timestamp.Equal(t) {
			f, err := strconv.ParseFloat(fieldsFirst(p.Equity.String()), 64)
			if err != nil {
				return 0, false
			}
			return f, true
		}
	}
	return 0, false
}

// maxDrawdownSince computes the maximum peak-to-trough drawdown over
// curve restricted to points at or after since — the test-window-only
// drawdown a continuous [train+test] run's own resp.Metrics.
// MaxDrawdown() cannot isolate on its own (that computes drawdown over
// the *entire* supplied curve, train portion included).
func maxDrawdownSince(curve []backtest.EquityPoint, since time.Time) float64 {
	var peak float64
	var maxDD float64
	started := false
	for _, p := range curve {
		if p.Timestamp.Before(since) {
			continue
		}
		v, err := strconv.ParseFloat(fieldsFirst(p.Equity.String()), 64)
		if err != nil {
			continue
		}
		if !started || v > peak {
			peak = v
			started = true
		}
		if peak > 0 {
			dd := (peak - v) / peak
			if dd > maxDD {
				maxDD = dd
			}
		}
	}
	return maxDD
}

// fieldsFirst returns s's first whitespace-separated field — num.
// Money.String()'s "<amount> <currency>" form carries a trailing
// currency code strconv.ParseFloat cannot parse directly.
func fieldsFirst(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] == ' ' {
			return s[:i]
		}
	}
	return s
}
