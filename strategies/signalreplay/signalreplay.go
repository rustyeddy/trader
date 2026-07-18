// Package signalreplay is an analysis harness for scanner-signal evaluation,
// not a live trading strategy. It replays a `trader review` sweep CSV through
// the existing backtest runner: each row is a scanner "signal" (instrument,
// bias, date), consecutive same-bias rows within a configurable gap collapse
// into one episode, and each episode is traded with a deliberately naive
// mechanical entry (next bar after the signal date). The point is to measure
// whether the scanner's "tradeable" classification has edge, not to trade it
// live. Registers as "signalreplay".
package signalreplay

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/rustyeddy/trader/execution"
	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/strategy"
	"github.com/rustyeddy/trader/types"
)

func init() {
	strategy.MustRegisterStrategy(build, "signalreplay")
}

// reasonPrefix is the stable Signal.Reason prefix carrying the episode
// signal date, joined back to the sweep CSV by the outcome report.
const reasonPrefix = "signalreplay:"

// Config controls the signalreplay strategy's behaviour. See the param table
// in docs/signalreplay-spec.org for defaults and meaning.
type Config struct {
	SignalsPath    string // path to a review sweep CSV; required
	Entry          string // entry mode; v1 supports only "next-open"
	EpisodeGapDays int    // max calendar-day gap merging rows into one episode
	MaxHoldDays    int    // 0 = unlimited; else time-stop after N bars in position
	CloseOnFlip    bool   // emit CloseAll when a new episode has opposite bias
	OnePerEpisode  bool   // at most one entry per episode (no re-entry)
}

// episode is a collapsed run of consecutive same-bias sweep rows for one
// instrument: (instrument, bias, first-date, last-date). The first date is
// the signal date; entry is the first bar strictly after it.
type episode struct {
	Instrument string
	Bias       types.Side
	FirstDate  time.Time
	LastDate   time.Time
}

// signalRow is one parsed, filtered row from the sweep CSV.
type signalRow struct {
	Date       time.Time
	Instrument string // normalized (no underscore, uppercase)
	Bias       types.Side
}

// SignalRow is the exported form of signalRow, for callers outside this
// package (e.g. the "trader signalreplay gen" config generator) that need
// the same parsed/filtered sweep rows without duplicating the CSV contract.
type SignalRow = signalRow

// LoadSignalRows parses and filters a review sweep CSV the same way the
// signalreplay Strategy does: only BUCKET=="tradeable" rows with a
// recognized BIAS survive, and PAIR is normalized (EUR_USD -> EURUSD).
func LoadSignalRows(path string) ([]SignalRow, error) {
	return loadSignalRows(path)
}

// Strategy replays scanner signals as a synthetic strategy. It does not know
// its instrument at construction time, only at Update time, so signal rows
// are loaded and filtered lazily on first Update.
type Strategy struct {
	cfg  Config
	name string

	loaded   bool
	loadErr  error
	episodes []episode

	// idx is the cursor into episodes: the earliest episode not yet
	// resolved (entered, when OnePerEpisode). This is position-state and
	// is cleared by Reset(); the parsed episode list is cached.
	idx            int
	barsInPosition int
}

// New constructs a signalreplay Strategy from a fully-resolved Config.
func New(cfg Config) (*Strategy, error) {
	if cfg.SignalsPath == "" {
		return nil, fmt.Errorf("signalreplay: signals path is required")
	}
	if cfg.Entry == "" {
		cfg.Entry = "next-open"
	}
	if cfg.Entry != "next-open" {
		return nil, fmt.Errorf("signalreplay: unsupported entry mode %q (v1 supports only \"next-open\")", cfg.Entry)
	}
	if cfg.EpisodeGapDays < 0 {
		return nil, fmt.Errorf("signalreplay: episode-gap must be >= 0")
	}
	if cfg.MaxHoldDays < 0 {
		return nil, fmt.Errorf("signalreplay: max-hold-days must be >= 0")
	}

	return &Strategy{
		cfg: cfg,
		name: fmt.Sprintf("SIGNALREPLAY(%s,gap=%dd,hold=%d,flip=%v,once=%v)",
			filepath.Base(cfg.SignalsPath), cfg.EpisodeGapDays, cfg.MaxHoldDays,
			cfg.CloseOnFlip, cfg.OnePerEpisode),
	}, nil
}

func (s *Strategy) Name() string { return s.name }

// StopDescription is delegated entirely to the configured exit strategy;
// signalreplay never sets Signal.Stop.
func (s *Strategy) StopDescription() string { return "delegated to exit strategy" }

// Ready reports true once the signal CSV has been loaded and episodes
// compiled for the current instrument.
func (s *Strategy) Ready() bool { return s.loaded && s.loadErr == nil }

// Reset clears position-state (the episode cursor and hold-bar counter) but
// keeps the parsed episode list cached across runs.
func (s *Strategy) Reset() {
	s.idx = 0
	s.barsInPosition = 0
}

func (s *Strategy) ensureLoaded(instrument string) {
	if s.loaded {
		return
	}
	s.loaded = true

	rows, err := loadSignalRows(s.cfg.SignalsPath)
	if err != nil {
		s.loadErr = err
		return
	}

	norm := market.NormalizeInstrument(instrument)
	filtered := rows[:0:0]
	for _, r := range rows {
		if r.Instrument == norm {
			filtered = append(filtered, r)
		}
	}
	s.episodes = compileEpisodes(filtered, s.cfg.EpisodeGapDays)
}

func (s *Strategy) Update(_ context.Context, ct *market.Candle, sc strategy.StrategyContext) strategy.Signal {
	if ct == nil {
		return strategy.Hold("no candle")
	}

	s.ensureLoaded(sc.Instrument())
	if s.loadErr != nil {
		return strategy.Hold(fmt.Sprintf("signalreplay: %v", s.loadErr))
	}

	barTime := ct.Timestamp
	lotOpen, openSide := openLotInfo(sc)
	if !lotOpen {
		s.barsInPosition = 0
	}

	var pending *episode
	if s.idx < len(s.episodes) {
		pending = &s.episodes[s.idx]
	}
	active := pending != nil && types.Timestamp(pending.FirstDate.Unix()) < barTime

	if lotOpen {
		s.barsInPosition++

		if active && s.cfg.CloseOnFlip && pending.Bias != openSide {
			reason := episodeReason(pending.FirstDate)
			if s.cfg.OnePerEpisode {
				s.idx++
			}
			s.barsInPosition = 0
			return strategy.Signal{Side: pending.Bias, CloseAll: true, Reason: reason}
		}

		if s.cfg.MaxHoldDays > 0 && s.barsInPosition >= s.cfg.MaxHoldDays {
			s.barsInPosition = 0
			return strategy.Signal{Side: types.Flat, CloseAll: true, Reason: reasonPrefix + "max-hold"}
		}

		return strategy.Hold("position open")
	}

	if active {
		reason := episodeReason(pending.FirstDate)
		if s.cfg.OnePerEpisode {
			s.idx++
		}
		s.barsInPosition = 0
		return strategy.Signal{Side: pending.Bias, Reason: reason}
	}

	return strategy.Hold("no active episode")
}

func episodeReason(t time.Time) string {
	return reasonPrefix + t.UTC().Format(time.RFC3339)
}

// openLotInfo reports whether a lot is currently open and, if so, its side.
// A netting account holds at most one net position, so the first open lot
// found is authoritative.
func openLotInfo(sc strategy.StrategyContext) (bool, types.Side) {
	var side types.Side
	found := false
	_ = sc.OpenLots().Range(func(lot *execution.Lot) error {
		if lot != nil && lot.State == execution.LotOpen {
			side = lot.Side
			found = true
		}
		return nil
	})
	return found, side
}

// compileEpisodes sorts rows by date and collapses consecutive same-bias
// rows whose gap is <= gapDays calendar days into one episode.
func compileEpisodes(rows []signalRow, gapDays int) []episode {
	if len(rows) == 0 {
		return nil
	}
	sorted := make([]signalRow, len(rows))
	copy(sorted, rows)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Date.Before(sorted[j].Date) })

	gap := time.Duration(gapDays) * 24 * time.Hour
	var eps []episode
	for _, row := range sorted {
		if len(eps) > 0 {
			last := &eps[len(eps)-1]
			if row.Bias == last.Bias && !row.Date.After(last.LastDate.Add(gap)) {
				if row.Date.After(last.LastDate) {
					last.LastDate = row.Date
				}
				continue
			}
		}
		eps = append(eps, episode{
			Instrument: row.Instrument,
			Bias:       row.Bias,
			FirstDate:  row.Date,
			LastDate:   row.Date,
		})
	}
	return eps
}

// loadSignalRows parses a review sweep CSV, keeping only tradeable rows with
// a recognized DATE/BIAS. Columns are located by header name, so extra
// columns (sweep features preserved for the report join) are ignored here.
func loadSignalRows(path string) ([]signalRow, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("signalreplay: open signals %q: %w", path, err)
	}
	defer f.Close()

	r := csv.NewReader(f)
	r.FieldsPerRecord = -1

	header, err := r.Read()
	if err != nil {
		return nil, fmt.Errorf("signalreplay: read signals header: %w", err)
	}
	col := map[string]int{}
	for i, h := range header {
		col[strings.TrimSpace(h)] = i
	}
	for _, want := range []string{"DATE", "PAIR", "BUCKET", "BIAS"} {
		if _, ok := col[want]; !ok {
			return nil, fmt.Errorf("signalreplay: signals CSV missing required column %q", want)
		}
	}

	var rows []signalRow
	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("signalreplay: parse signals row: %w", err)
		}

		if rec[col["BUCKET"]] != "tradeable" {
			continue
		}

		dateStr := rec[col["DATE"]]
		date, err := time.Parse(time.RFC3339, dateStr)
		if err != nil {
			return nil, fmt.Errorf("signalreplay: parse date %q: %w", dateStr, err)
		}

		var bias types.Side
		switch strings.ToLower(strings.TrimSpace(rec[col["BIAS"]])) {
		case "long":
			bias = types.Long
		case "short":
			bias = types.Short
		default:
			continue // unrecognized bias; skip row
		}

		rows = append(rows, signalRow{
			Date:       date,
			Instrument: market.NormalizeInstrument(rec[col["PAIR"]]),
			Bias:       bias,
		})
	}
	return rows, nil
}

func build(params map[string]any) (strategy.Strategy, error) {
	signalsPath, ok, err := strategy.GetStringParam(params, "signals")
	if err != nil {
		return nil, err
	}
	if !ok || signalsPath == "" {
		return nil, fmt.Errorf("signalreplay: param \"signals\" is required")
	}
	if _, err := os.Stat(signalsPath); err != nil {
		return nil, fmt.Errorf("signalreplay: signals file: %w", err)
	}

	entry, ok, err := strategy.GetStringParam(params, "entry")
	if err != nil {
		return nil, err
	}
	if !ok || entry == "" {
		entry = "next-open"
	}

	episodeGap, ok, err := strategy.GetIntParam(params, "episode-gap")
	if err != nil {
		return nil, err
	}
	if !ok {
		episodeGap = 5
	}

	maxHoldDays, ok, err := strategy.GetIntParam(params, "max-hold-days")
	if err != nil {
		return nil, err
	}
	if !ok {
		maxHoldDays = 0
	}

	closeOnFlip, ok, err := strategy.GetBoolParam(params, "close-on-flip")
	if err != nil {
		return nil, err
	}
	if !ok {
		closeOnFlip = true
	}

	onePerEpisode, ok, err := strategy.GetBoolParam(params, "one-per-episode")
	if err != nil {
		return nil, err
	}
	if !ok {
		onePerEpisode = true
	}

	return New(Config{
		SignalsPath:    signalsPath,
		Entry:          entry,
		EpisodeGapDays: episodeGap,
		MaxHoldDays:    maxHoldDays,
		CloseOnFlip:    closeOnFlip,
		OnePerEpisode:  onePerEpisode,
	})
}
