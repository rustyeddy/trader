package trader

import (
	"bufio"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/rustyeddy/trader/num"
)

// Run backtests every scenario and writes one summary each to w. It is the
// entry point cmd/trader calls; see run for the output contract.
func Run(w io.Writer) error { return run(w) }

// scenarios are the reviewed datasets in emax-validation.org, in its order.
var scenarios = []string{
	"a-reversal-exit",
	"b1-stop-wick",
	"b2-stop-cross",
	"b3-stop-gap",
	"c-reversal",
	"d-short-stop",
}

// run backtests every scenario on fresh instances and writes one summary each
// to w. Diagnostics go to slog, on stderr, and carry wall-clock times; w gets
// only the summaries, so its output is identical from run to run.
func run(w io.Writer) error {
	for _, scenario := range scenarios {
		r, err := runScenario(scenario)
		if err != nil {
			return fmt.Errorf("scenario %s: %w", scenario, err)
		}
		if err := r.write(w); err != nil {
			return fmt.Errorf("scenario %s: write summary: %w", scenario, err)
		}
	}
	return nil
}

func runScenario(scenario string) (*result, error) {
	tr, err := getTrader()
	if err != nil {
		return nil, err
	}
	prices, err := loadBars(scenarioPath(scenario))
	if err != nil {
		return nil, err
	}
	if err := tr.broker.markets.normalize(prices); err != nil {
		return nil, fmt.Errorf("%s: %w", scenarioPath(scenario), err)
	}

	slog.Info("backtest starting",
		"scenario", scenario,
		"strategy", tr.strategy.name(),
		"instrument", prices.instrument,
		"interval", prices.interval,
		"bars", len(prices.data))

	ctx := context.Background()
	if err := (&backtest{}).run(ctx, prices.bars(ctx), tr); err != nil {
		return nil, err
	}

	slog.Info("backtest complete", "scenario", scenario, "strategy", tr.strategy.name())
	return newResult(scenario, len(prices.data), tr)
}

// scenarioPath is where a scenario's bars are, relative to examples/design.
func scenarioPath(scenario string) string {
	return filepath.Join("testdata", "emax", scenario,
		"data", "synthetic", "canonical", "EURUSD", "2010", "01", "EURUSD-2010-01-D1.csv")
}

// getTrader builds the configuration the scenarios were worked out for.
func getTrader() (*trader, error) {
	markets, err := defaultCatalog()
	if err != nil {
		return nil, err
	}
	m, ok := markets.lookup("EURUSD")
	if !ok {
		return nil, fmt.Errorf("the scenarios trade EURUSD: %w", errUnknownInstrument)
	}
	strat := &emax{
		id:      "emax",
		short:   2,
		long:    4,
		stopPct: rateOnePercent,
		tick:    m.tick,
	}
	if err := strat.init(); err != nil {
		return nil, err
	}
	br := newBroker("sim", markets)
	return &trader{
		broker:   br,
		account:  br.addAccount("sim123", num.MustParseMoney("10000", usdCurrency)),
		strategy: strat,
	}, nil
}

// --- The prototype scenario file reader (design-bars-v1) ---

const (
	barSchema     = "design-bars-v1"
	barTimeLayout = "2006-01-02T15:04:05Z" // always UTC
)

var barColumns = []string{"time", "open", "high", "low", "close", "volume"}

// barDurations gives how long a bar of each supported interval lasts. Weekly and
// session-based bars are absent on purpose: their end is not open plus a fixed
// duration.
var barDurations = map[string]time.Duration{
	"h1": time.Hour,
	"d1": 24 * time.Hour,
}

// loadBars reads the bars in the file at path; see readBars.
func loadBars(path string) (*marketData, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }() // read only: nothing to lose

	md, err := readBars(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return md, nil
}

// readBars parses a design-bars-v1 file: a "# schema=..." header naming the
// instrument and interval, the column names, then one row per bar. It refuses
// anything malformed rather than skipping it, including bars whose times do not
// strictly increase, since the broker fills an order only on a later bar.
func readBars(r io.Reader) (*marketData, error) {
	br := bufio.NewReader(r)
	header, err := br.ReadString('\n')
	if err != nil {
		return nil, fmt.Errorf("bars header: %w", err)
	}
	fields, ok := strings.CutPrefix(header, "# ")
	if !ok {
		return nil, fmt.Errorf("bars header %q: want a comment", header)
	}
	meta := make(map[string]string)
	for _, f := range strings.Fields(fields) {
		k, v, _ := strings.Cut(f, "=")
		meta[k] = v
	}
	if meta["schema"] != barSchema {
		return nil, fmt.Errorf("bars schema %q: want %s", meta["schema"], barSchema)
	}
	if meta["instrument"] == "" || meta["tf"] == "" {
		return nil, errors.New("bars header needs an instrument and a tf")
	}
	lasts, ok := barDurations[meta["tf"]]
	if !ok {
		return nil, fmt.Errorf("bars header: unsupported tf %q", meta["tf"])
	}

	cr := csv.NewReader(br)
	cols, err := cr.Read()
	if err != nil {
		return nil, fmt.Errorf("bars columns: %w", err)
	}
	// Compare fields, not joined text: a quoted comma would hide a missing column
	// and let shorter rows through to parseBar.
	if !slices.Equal(cols, barColumns) {
		return nil, fmt.Errorf("bars columns %q: want %q", cols, barColumns)
	}

	md := &marketData{instrument: meta["instrument"], interval: meta["tf"]}
	for {
		rec, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("bars: %w", err)
		}
		line, _ := cr.FieldPos(0)
		b, err := parseBar(md.instrument, rec, lasts)
		if err != nil {
			return nil, fmt.Errorf("bars line %d: %w", line, err)
		}
		if n := len(md.data); n > 0 && !b.After(md.data[n-1].Time) {
			return nil, fmt.Errorf("bars line %d: time %s is not after %s", line, rec[0], md.data[n-1].Format(barTimeLayout))
		}
		md.data = append(md.data, b)
	}
	if len(md.data) == 0 {
		return nil, errors.New("bars: no rows")
	}
	return md, nil
}

// parseBar parses one row, checked against barColumns by the caller. A bar that
// lasts the given duration is available when its interval ends. The file does
// not store that time: this is where the prototype builds a canonical bar, the
// counterpart of production's raw-to-canonical conversion, which owns the rule.
func parseBar(instrument string, rec []string, lasts time.Duration) (bar, error) {
	t, err := time.Parse(barTimeLayout, rec[0])
	if err != nil {
		return bar{}, err
	}
	var px [4]num.Price
	for i := range px {
		if px[i], err = parsePrice(rec[i+1]); err != nil {
			return bar{}, err
		}
	}
	v, err := strconv.Atoi(rec[5])
	if err != nil {
		return bar{}, fmt.Errorf("volume: %w", err)
	}
	if v < 0 {
		return bar{}, fmt.Errorf("volume %d is negative", v)
	}
	o, h, l, c := px[0], px[1], px[2], px[3]
	if h.Cmp(num.Max(o, c)) < 0 || l.Cmp(num.Min(o, c)) > 0 {
		return bar{}, fmt.Errorf("high %s and low %s do not contain open %s and close %s", h, l, o, c)
	}
	return bar{instrument: instrument, Time: t, ohlc: ohlc{o: o, h: h, l: l, c: c, v: v}, availableAt: t.Add(lasts)}, nil
}

// parsePrice reads a positive price written with exactly five decimals, the
// resolution of the bars schema. The text rule (digits only, exactly five
// decimals, parsed exactly, nothing through a float) is num's; positivity and the
// largest price an engine accepts are checked here. That each price is a whole
// number of its instrument's ticks is checked when the bars are normalized.
func parsePrice(s string) (num.Price, error) {
	p, err := num.ParsePriceFixed(s, barPriceDecimals)
	if err != nil {
		return num.Price{}, fmt.Errorf("price %q: %w", s, err)
	}
	if p.IsZero() {
		return num.Price{}, fmt.Errorf("price %q: must be positive", s)
	}
	if p.Cmp(maxPrice) > 0 {
		return num.Price{}, fmt.Errorf("price %q: above the maximum %s", s, maxPrice)
	}
	return p, nil
}
