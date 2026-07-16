// Package signalreplay provides the "trader signalreplay" CLI: generating a
// backtest YAML from a review sweep CSV (gen) and executing it (run). See
// docs/signalreplay-spec.org and strategies/signalreplay for the analysis
// this tooling drives.
package signalreplay

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rustyeddy/trader/backtest"
	"github.com/rustyeddy/trader/strategies/signalreplay"
	"github.com/rustyeddy/trader/strategy"
)

// dateLayout matches backtest.DataConfig.From/To's expected "YYYY-MM-DD" form.
const dateLayout = "2006-01-02"

// GenOptions controls "trader signalreplay gen" config generation.
type GenOptions struct {
	SignalsPath string
	ExitKind    string
	ExitParams  map[string]any

	Timeframe string
	Source    string

	RiskPct         float64
	StartingBalance float64
	AccountCCY      string
	Scale           int64
	MaxSpreadPips   float64

	WarmupDays int
	RunoutDays int

	// signalreplay strategy params, embedded verbatim into every run.
	Entry          string
	EpisodeGapDays int
	MaxHoldDays    int
	CloseOnFlip    bool
	OnePerEpisode  bool
}

// DefaultGenOptions returns the flag defaults documented in
// docs/signalreplay-spec.org Component 2.
func DefaultGenOptions() GenOptions {
	return GenOptions{
		Entry:           "next-open",
		Timeframe:       "D1",
		Source:          "oanda",
		RiskPct:         0.5,
		StartingBalance: 10000,
		AccountCCY:      "USD",
		Scale:           100000,
		WarmupDays:      90,
		RunoutDays:      120,
		EpisodeGapDays:  5,
		CloseOnFlip:     true,
		OnePerEpisode:   true,
	}
}

// GenerateConfig reads the sweep CSV and emits one RunConfig per distinct
// instrument, each with a date range spanning that instrument's earliest
// signal minus a warmup buffer to its latest signal plus a runout buffer.
// Runs are ordered by instrument name so the same inputs always produce the
// same output (no map-order leakage).
func GenerateConfig(opts GenOptions) (*backtest.Config, error) {
	if strings.TrimSpace(opts.ExitKind) == "" {
		return nil, fmt.Errorf("signalreplay gen: --exit is required (refuse empty exit config)")
	}
	if strings.TrimSpace(opts.SignalsPath) == "" {
		return nil, fmt.Errorf("signalreplay gen: --signals is required")
	}

	rows, err := signalreplay.LoadSignalRows(opts.SignalsPath)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("signalreplay gen: no tradeable rows found in %q", opts.SignalsPath)
	}

	type dateRange struct {
		first, last time.Time
	}
	ranges := map[string]dateRange{}
	for _, r := range rows {
		dr, ok := ranges[r.Instrument]
		if !ok {
			ranges[r.Instrument] = dateRange{first: r.Date, last: r.Date}
			continue
		}
		if r.Date.Before(dr.first) {
			dr.first = r.Date
		}
		if r.Date.After(dr.last) {
			dr.last = r.Date
		}
		ranges[r.Instrument] = dr
	}

	instruments := make([]string, 0, len(ranges))
	for inst := range ranges {
		instruments = append(instruments, inst)
	}
	sort.Strings(instruments)

	warmup := time.Duration(opts.WarmupDays) * 24 * time.Hour
	runout := time.Duration(opts.RunoutDays) * 24 * time.Hour

	strategyParams := map[string]any{
		"signals":         opts.SignalsPath,
		"entry":           opts.Entry,
		"episode-gap":     int64(opts.EpisodeGapDays),
		"max-hold-days":   int64(opts.MaxHoldDays),
		"close-on-flip":   opts.CloseOnFlip,
		"one-per-episode": opts.OnePerEpisode,
	}

	runs := make([]backtest.RunConfig, 0, len(instruments))
	for _, inst := range instruments {
		dr := ranges[inst]
		runs = append(runs, backtest.RunConfig{
			Name: fmt.Sprintf("%s-signalreplay", strings.ToLower(inst)),
			Data: backtest.DataConfig{
				Instrument: inst,
				Timeframe:  opts.Timeframe,
				From:       dr.first.Add(-warmup).Format(dateLayout),
				To:         dr.last.Add(runout).Format(dateLayout),
			},
			Strategy: strategy.StrategyConfig{
				Kind:   "signalreplay",
				Params: strategyParams,
			},
			Exit: strategy.ExitConfig{
				Kind:   opts.ExitKind,
				Params: opts.ExitParams,
			},
		})
	}

	return &backtest.Config{
		Version: 1,
		Defaults: backtest.RunDefaults{
			StartingBalance: opts.StartingBalance,
			AccountCCY:      opts.AccountCCY,
			Scale:           opts.Scale,
			RiskPct:         opts.RiskPct,
			Source:          opts.Source,
			MaxSpreadPips:   opts.MaxSpreadPips,
		},
		Runs: runs,
	}, nil
}

// ParseExitParams parses a comma-separated key=value flag value (e.g.
// "atr_period=14,multiplier=2.0") into a params map, inferring int64,
// float64, or bool for values that parse as such and falling back to string.
// An empty input yields a nil (not empty) map so the generated YAML omits
// the params key entirely rather than emitting "params: {}".
func ParseExitParams(raw string) (map[string]any, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	params := map[string]any{}
	for pair := range strings.SplitSeq(raw, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		kv := strings.SplitN(pair, "=", 2)
		if len(kv) != 2 {
			return nil, fmt.Errorf("signalreplay gen: invalid --exit-params entry %q (want key=value)", pair)
		}
		key := strings.TrimSpace(kv[0])
		val := strings.TrimSpace(kv[1])
		if key == "" {
			return nil, fmt.Errorf("signalreplay gen: invalid --exit-params entry %q (empty key)", pair)
		}
		params[key] = parseParamValue(val)
	}
	return params, nil
}

func parseParamValue(v string) any {
	if i, err := strconv.ParseInt(v, 10, 64); err == nil {
		return i
	}
	if f, err := strconv.ParseFloat(v, 64); err == nil {
		return f
	}
	if b, err := strconv.ParseBool(v); err == nil {
		return b
	}
	return v
}
