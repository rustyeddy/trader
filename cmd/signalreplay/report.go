package signalreplay

import (
	"encoding/csv"
	"fmt"
	"io"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rustyeddy/trader/backtest"
	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/service"
)

// signalReasonPrefix must match strategies/signalreplay's reasonPrefix: the
// stable Signal.Reason marker carrying the episode signal date.
const signalReasonPrefix = "signalreplay:"

// outcomeFeatureColumns is the fixed set of sweep CSV columns joined onto
// each outcome row, in output order. See docs/signalreplay-spec.org
// Component 3.
var outcomeFeatureColumns = []string{
	"ADX", "CI", "EMA SEP", "EMA DIST", "H4 ADX", "H4 CI", "H4 EMA DIST",
	"Squeeze", "W1 Bias", "WEEK%", "H1 Align", "H1 EMA DIST",
}

var outcomeCoreColumns = []string{
	"signal_date", "instrument", "bias", "entry_time", "entry_price",
	"initial_stop", "exit_time", "exit_price", "close_cause", "pnl",
	"r_multiple", "hold_bars",
}

// OutcomeRow is one closed signalreplay trade joined back to the sweep CSV
// row that generated its entry signal.
type OutcomeRow struct {
	SignalDate  string
	Instrument  string
	Bias        string
	EntryTime   string
	EntryPrice  float64
	InitialStop float64
	ExitTime    string
	ExitPrice   float64
	CloseCause  string
	PNL         float64
	RMultiple   float64
	HoldBars    int
	Features    map[string]string
}

// BuildOutcomeRows reads every backtest JSON report in reportsDir, keeps only
// trades opened by the signalreplay strategy (identified by the
// "signalreplay:<date>" Reason marker), and joins each one back to its
// sweep CSV row's feature columns on (instrument, signal date).
func BuildOutcomeRows(reportsDir, signalsPath string) ([]OutcomeRow, error) {
	features, err := loadSweepFeatures(signalsPath)
	if err != nil {
		return nil, err
	}

	summaries, err := service.ListBacktestSummaries(reportsDir)
	if err != nil {
		return nil, fmt.Errorf("signalreplay report: list reports in %q: %w", reportsDir, err)
	}
	if len(summaries) == 0 {
		return nil, fmt.Errorf("signalreplay report: no backtest reports found in %q", reportsDir)
	}

	var rows []OutcomeRow
	for _, s := range summaries {
		barSeconds := timeframeSeconds(s.Timeframe)
		for _, tr := range s.TradeDetails {
			row, ok := buildOutcomeRow(tr, features, barSeconds)
			if !ok {
				continue
			}
			rows = append(rows, row)
		}
	}

	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Instrument != rows[j].Instrument {
			return rows[i].Instrument < rows[j].Instrument
		}
		return rows[i].EntryTime < rows[j].EntryTime
	})
	return rows, nil
}

// buildOutcomeRow converts one BacktestReportTrade into an OutcomeRow. It
// returns ok=false for trades that were not opened by the signalreplay
// strategy (no recognizable Reason marker) so callers can filter mixed
// report directories.
func buildOutcomeRow(tr backtest.BacktestReportTrade, features map[string]map[string]string, barSeconds float64) (OutcomeRow, bool) {
	if !strings.HasPrefix(tr.Reason, signalReasonPrefix) {
		return OutcomeRow{}, false
	}
	dateStr := strings.TrimPrefix(tr.Reason, signalReasonPrefix)
	signalDate, err := time.Parse(time.RFC3339, dateStr)
	if err != nil {
		return OutcomeRow{}, false
	}

	inst := market.NormalizeInstrument(tr.Instrument)
	key := featureKey(inst, signalDate)

	return OutcomeRow{
		SignalDate:  signalDate.UTC().Format(time.RFC3339),
		Instrument:  inst,
		Bias:        tr.Side,
		EntryTime:   tr.OpenTime,
		EntryPrice:  tr.OpenPrice,
		InitialStop: tr.InitialStopPrice,
		ExitTime:    tr.CloseTime,
		ExitPrice:   tr.ClosePrice,
		CloseCause:  tr.CloseCause,
		PNL:         tr.PNL,
		RMultiple:   rMultiple(tr.Side, tr.OpenPrice, tr.ClosePrice, tr.InitialStopPrice),
		HoldBars:    holdBars(tr.OpenTime, tr.CloseTime, barSeconds),
		Features:    features[key],
	}, true
}

// rMultiple computes the signed, side-adjusted R-multiple:
// (exit-entry)/(entry-initial_stop) for longs, mirrored for shorts. Returns
// 0 when the stop distance is zero (guards divide-by-zero for a stop set
// at entry).
func rMultiple(side string, entry, exit, initialStop float64) float64 {
	risk := entry - initialStop
	if risk < 0 {
		risk = -risk
	}
	if risk == 0 {
		return 0
	}
	reward := exit - entry
	if strings.EqualFold(side, "short") {
		reward = entry - exit
	}
	return reward / risk
}

// holdBars converts the entry/exit RFC3339 timestamps into a bar count using
// barSeconds (the run's candle timeframe). Returns 0 if either timestamp or
// the timeframe is unrecognized.
func holdBars(openTime, closeTime string, barSeconds float64) int {
	if barSeconds <= 0 {
		return 0
	}
	entryT, err1 := time.Parse(time.RFC3339, openTime)
	exitT, err2 := time.Parse(time.RFC3339, closeTime)
	if err1 != nil || err2 != nil {
		return 0
	}
	return int(math.Round(exitT.Sub(entryT).Seconds() / barSeconds))
}

func timeframeSeconds(tf string) float64 {
	switch strings.ToLower(strings.TrimSpace(tf)) {
	case "m1":
		return 60
	case "h1":
		return 3600
	case "h4":
		return 14400
	case "d1":
		return 86400
	default:
		return 0
	}
}

func featureKey(instrument string, date time.Time) string {
	return instrument + "|" + date.UTC().Format(time.RFC3339)
}

// loadSweepFeatures indexes a review sweep CSV by (instrument, DATE),
// keeping only the columns the outcome report joins onto each trade.
func loadSweepFeatures(path string) (map[string]map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("signalreplay report: open signals %q: %w", path, err)
	}
	defer f.Close()

	r := csv.NewReader(f)
	r.FieldsPerRecord = -1

	header, err := r.Read()
	if err != nil {
		return nil, fmt.Errorf("signalreplay report: read signals header: %w", err)
	}
	col := map[string]int{}
	for i, h := range header {
		col[strings.TrimSpace(h)] = i
	}
	required := append([]string{"DATE", "PAIR"}, outcomeFeatureColumns...)
	for _, want := range required {
		if _, ok := col[want]; !ok {
			return nil, fmt.Errorf("signalreplay report: signals CSV missing required column %q", want)
		}
	}

	idx := map[string]map[string]string{}
	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("signalreplay report: parse signals row: %w", err)
		}

		date, err := time.Parse(time.RFC3339, rec[col["DATE"]])
		if err != nil {
			continue
		}
		inst := market.NormalizeInstrument(rec[col["PAIR"]])

		feat := make(map[string]string, len(outcomeFeatureColumns))
		for _, c := range outcomeFeatureColumns {
			feat[c] = rec[col[c]]
		}
		idx[featureKey(inst, date)] = feat
	}
	return idx, nil
}

// WriteOutcomeCSV writes one row per trade in the documented column order.
// Row order is the caller's responsibility (BuildOutcomeRows already sorts
// deterministically), so the same inputs always produce byte-identical
// output.
func WriteOutcomeCSV(w io.Writer, rows []OutcomeRow) error {
	cw := csv.NewWriter(w)
	defer cw.Flush()

	header := make([]string, 0, len(outcomeCoreColumns)+len(outcomeFeatureColumns))
	header = append(header, outcomeCoreColumns...)
	header = append(header, outcomeFeatureColumns...)
	if err := cw.Write(header); err != nil {
		return err
	}

	for _, r := range rows {
		rec := []string{
			r.SignalDate,
			r.Instrument,
			r.Bias,
			r.EntryTime,
			formatFloat(r.EntryPrice),
			formatFloat(r.InitialStop),
			r.ExitTime,
			formatFloat(r.ExitPrice),
			r.CloseCause,
			formatFloat(r.PNL),
			strconv.FormatFloat(r.RMultiple, 'f', 4, 64),
			strconv.Itoa(r.HoldBars),
		}
		for _, c := range outcomeFeatureColumns {
			rec = append(rec, r.Features[c])
		}
		if err := cw.Write(rec); err != nil {
			return err
		}
	}
	return cw.Error()
}

func formatFloat(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// bucketAcc accumulates R-multiple/win-rate stats for one group (pair or
// close cause) while scanning outcome rows.
type bucketAcc struct {
	count int
	sumR  float64
	wins  int
}

// BucketStats is the finalized, read-only form of bucketAcc for reporting.
type BucketStats struct {
	Count   int
	AvgR    float64
	WinRate float64 // percent
}

// OutcomeSummary aggregates outcome rows for the stdout/org summary block.
type OutcomeSummary struct {
	TradeCount   int
	WinRate      float64 // percent
	AvgR         float64
	Expectancy   float64 // average PNL per trade
	ByPair       map[string]BucketStats
	ByCloseCause map[string]BucketStats
}

// Summarize computes trade count, win rate, avg R, expectancy, and R
// distribution by pair and by close cause.
func Summarize(rows []OutcomeRow) OutcomeSummary {
	var sumR, sumPNL float64
	wins := 0
	byPair := map[string]*bucketAcc{}
	byCause := map[string]*bucketAcc{}

	for _, r := range rows {
		sumR += r.RMultiple
		sumPNL += r.PNL
		if r.PNL > 0 {
			wins++
		}
		addBucket(byPair, r.Instrument, r)
		addBucket(byCause, r.CloseCause, r)
	}

	n := len(rows)
	s := OutcomeSummary{
		TradeCount:   n,
		ByPair:       finalizeBuckets(byPair),
		ByCloseCause: finalizeBuckets(byCause),
	}
	if n > 0 {
		s.WinRate = 100 * float64(wins) / float64(n)
		s.AvgR = sumR / float64(n)
		s.Expectancy = sumPNL / float64(n)
	}
	return s
}

func addBucket(m map[string]*bucketAcc, key string, r OutcomeRow) {
	b, ok := m[key]
	if !ok {
		b = &bucketAcc{}
		m[key] = b
	}
	b.count++
	b.sumR += r.RMultiple
	if r.PNL > 0 {
		b.wins++
	}
}

func finalizeBuckets(m map[string]*bucketAcc) map[string]BucketStats {
	out := make(map[string]BucketStats, len(m))
	for k, b := range m {
		stats := BucketStats{Count: b.count}
		if b.count > 0 {
			stats.AvgR = b.sumR / float64(b.count)
			stats.WinRate = 100 * float64(b.wins) / float64(b.count)
		}
		out[k] = stats
	}
	return out
}

// PrintOutcomeSummary writes a human-readable summary: overall stats, then
// R distribution by pair and by close cause, in alphabetical key order for
// deterministic output.
func PrintOutcomeSummary(w io.Writer, s OutcomeSummary) {
	fmt.Fprintf(w, "Trades: %d   Win rate: %.1f%%   Avg R: %.2f   Expectancy: $%.2f\n",
		s.TradeCount, s.WinRate, s.AvgR, s.Expectancy)

	fmt.Fprintln(w, "\nBy pair:")
	for _, k := range sortedKeys(s.ByPair) {
		b := s.ByPair[k]
		fmt.Fprintf(w, "  %-10s trades=%-4d avg_r=%6.2f win_rate=%5.1f%%\n", k, b.Count, b.AvgR, b.WinRate)
	}

	fmt.Fprintln(w, "\nBy close cause:")
	for _, k := range sortedKeys(s.ByCloseCause) {
		b := s.ByCloseCause[k]
		fmt.Fprintf(w, "  %-14s trades=%-4d avg_r=%6.2f win_rate=%5.1f%%\n", k, b.Count, b.AvgR, b.WinRate)
	}
}

func sortedKeys(m map[string]BucketStats) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
