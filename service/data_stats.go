package service

import (
	"context"
	"fmt"
	"time"

	"github.com/rustyeddy/trader/datamanager"
	"github.com/rustyeddy/trader/market"
)

// DataStatsRequest parameterises DataStats.
type DataStatsRequest struct {
	Instrument string // e.g. "EURUSD"
	Timeframe  string // "M1", "H1", "D1"; defaults to "H1" when empty
	From       string // YYYY-MM-DD inclusive; from == to is a single-day range
	To         string // YYYY-MM-DD inclusive; must not be before From
	Source     string // optional; defaults to "oanda"
	Units      int64  // optional; >0 adds USD column (100000 = standard lot)
}

// DataStatsResult holds the complete stats output.
type DataStatsResult struct {
	Instrument string           `json:"instrument"`
	Timeframe  string           `json:"timeframe"`
	From       string           `json:"from"`
	To         string           `json:"to"`
	Analyzers  []AnalyzerResult `json:"analyzers"`
}

// AnalyzerResult is a JSON-serialisable group of stats from one analyzer.
type AnalyzerResult struct {
	Name string    `json:"name"`
	Rows []StatRow `json:"rows"`
}

// StatRow is one labeled measurement.
type StatRow struct {
	Name  string  `json:"name"`
	Value string  `json:"value"`
	Pips  float64 `json:"pips,omitempty"`
	USD   float64 `json:"usd,omitempty"`
}

// dataStatsRates are fallback mid-market rates for USD-base pairs.
var dataStatsRates = map[string]float64{
	"USDJPY": 150.00,
	"USDCHF": 0.90,
	"USDCAD": 1.36,
}

func (s *Service) DataStats(ctx context.Context, req DataStatsRequest) (*DataStatsResult, error) {
	inst := market.NormalizeInstrument(req.Instrument)
	if inst == "" {
		return nil, fmt.Errorf("blank instrument")
	}
	instMeta := market.GetInstrument(inst)
	if instMeta == nil {
		return nil, fmt.Errorf("unknown instrument: %s", inst)
	}

	from, err := time.Parse("2006-01-02", req.From)
	if err != nil {
		return nil, fmt.Errorf("bad from %q: %w", req.From, err)
	}
	to, err := time.Parse("2006-01-02", req.To)
	if err != nil {
		return nil, fmt.Errorf("bad to %q: %w", req.To, err)
	}
	if to.Before(from) {
		return nil, fmt.Errorf("from must not be after to")
	}
	toExcl := to.AddDate(0, 0, 1)

	tf := req.Timeframe
	if tf == "" {
		tf = "H1"
	}

	tr, err := market.ParseTimeRange(from.Format("2006-01-02"), toExcl.Format("2006-01-02"), tf)
	if err != nil {
		return nil, fmt.Errorf("bad range: %w", err)
	}

	analyzers := []datamanager.Analyzer{
		datamanager.NewSwingAnalyzer(instMeta),
		datamanager.NewSpreadAnalyzer(instMeta),
		datamanager.NewTrendAnalyzer(),
		datamanager.NewSessionAnalyzer(instMeta),
	}

	dm := datamanager.NewDataManager([]string{inst}, from, toExcl)
	itr, err := dm.Candles(ctx, datamanager.CandleRequest{
		Source:     req.Source,
		Instrument: inst,
		Range:      tr,
	})
	if err != nil {
		return nil, fmt.Errorf("open candles: %w", err)
	}
	if err := datamanager.RunAnalysis(ctx, itr, analyzers); err != nil {
		return nil, fmt.Errorf("analysis: %w", err)
	}

	rate := dataStatsRates[inst]

	result := &DataStatsResult{
		Instrument: inst,
		Timeframe:  tf,
		From:       from.Format("2006-01-02"),
		To:         to.Format("2006-01-02"),
		Analyzers:  make([]AnalyzerResult, 0, len(analyzers)),
	}

	for _, a := range analyzers {
		ar := AnalyzerResult{Name: a.Name()}
		for _, st := range a.Stats() {
			row := StatRow{Name: st.Name, Value: st.Value, Pips: st.Pips}
			if req.Units > 0 && st.Pips > 0 {
				row.USD = instMeta.PipValueUSD(rate, req.Units, st.Pips)
			}
			ar.Rows = append(ar.Rows, row)
		}
		result.Analyzers = append(result.Analyzers, ar)
	}
	return result, nil
}
