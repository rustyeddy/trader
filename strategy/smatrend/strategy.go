package smatrend

import (
	"context"
	"fmt"
	"strconv"

	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/indicator"
	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/journal"
	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
	"github.com/rustyeddy/trader/strategy"
)

// Name identifies this strategy in Descriptor.Name and, conventionally,
// as the Source recorded on every Intent its IntentFactory builds.
const Name = "sma-trend"

// Version distinguishes revisions of this strategy's own logic.
const Version = "v0"

// Strategy is the SMA-trend baseline strategy.Strategy implementation
// (issue #335, EQS-01). It owns one indicator.SMA instance, the
// cross-above state machine that interprets it, and the trailing-stop
// bookkeeping (high-water mark, current stop level) that governs the
// only exit this strategy ever takes. See the package doc comment for
// the exact semantics, and docs/research/eqs-01-baseline-sma-trend.org
// for the reference SPY configuration and backtest results.
//
// Strategy is not safe for concurrent use, and not reusable across
// runs: construct a fresh Strategy (via New) for each run, matching
// strategy.Environment's own "strategy state must not be reused"
// requirement documented on service/backtest.RunRequest.Strategy.
type Strategy struct {
	instrumentID instrument.ID
	interval     marketdata.Interval
	config       Config

	// retainFraction is 1 - config.TrailingStopPercent, computed once
	// at construction (Config.Validate already guarantees it cannot
	// error) rather than recomputed every bar.
	retainFraction num.Rate

	sma   *indicator.SMA
	cross crossState

	// highWaterMark and stopPrice are nil until the first bar a Long
	// position is observed (see resetTrailingState); both are cleared
	// the moment the position returns to Flat, so a later re-entry
	// starts a fresh trailing episode rather than resuming stale state
	// from a previous one.
	highWaterMark *num.Price
	stopPrice     *num.Price

	intents strategy.IntentFactory
	journal journal.Recorder // nil unless env.Journal was set
	runID   id.RunID
}

// New returns a Strategy trading instrumentID on interval, configured
// by config. It returns config's own Validate error, if any.
func New(instrumentID instrument.ID, interval marketdata.Interval, config Config) (*Strategy, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	sma, err := indicator.NewSMA(config.SMAPeriod)
	if err != nil {
		return nil, err
	}
	retain, err := config.StopFraction()
	if err != nil {
		return nil, err
	}
	return &Strategy{
		instrumentID:   instrumentID,
		interval:       interval,
		config:         config,
		retainFraction: retain,
		sma:            sma,
	}, nil
}

// Config returns this strategy's own configuration, for a composition
// root that wants to record it (for example in a run manifest)
// without keeping a second copy of the same values.
func (s *Strategy) Config() Config {
	return s.config
}

// Describe implements strategy.Strategy. WarmupBars equals SMAPeriod:
// no cross-above signal is meaningful (and none is possible — SMA.
// Value() is 0 and Ready() is false) before the SMA has seen a full
// window of samples.
func (s *Strategy) Describe() strategy.Descriptor {
	return strategy.Descriptor{
		Name:    Name,
		Version: Version,
		Requirements: []strategy.DataRequirement{
			{Instrument: s.instrumentID, Interval: s.interval, WarmupBars: s.config.SMAPeriod},
		},
	}
}

// Start implements strategy.Strategy.
func (s *Strategy) Start(ctx context.Context, env strategy.Environment) error {
	if env.Journal != nil && env.RunID.IsZero() {
		return fmt.Errorf("smatrend: env.Journal is set but env.RunID is zero: every recorded signal needs a run id")
	}
	s.intents = env.Intents
	s.journal = env.Journal
	s.runID = env.RunID
	return nil
}

// OnBar implements strategy.Strategy.
//
// smatrend never detects its own trailing-stop trigger: by the time
// OnBar observes this bar, backtest.Scheduler has already advanced the
// broker's own resting-order machinery (ADR-026, issue #338) against
// this exact bar, using whatever stop level a prior bar's own OnBar
// call established. If that stop was breached, view already reports
// this instrument Flat — "the position survived the bar" (EQS-01's own
// phrase) is exactly the condition side == order.Long below. This is
// what keeps stop evaluation genuinely prior-information-only: OnBar
// never compares this bar's own price action against a stop level
// computed from this same bar.
func (s *Strategy) OnBar(ctx context.Context, event strategy.BarEvent, view strategy.View) ([]order.Intent, error) {
	// event.Bar.Close.Float64() is ADR-045's explicit exact-to-analytical
	// conversion boundary: a direct numeric conversion, never a
	// String()/strconv.ParseFloat() round-trip.
	close := event.Bar.Close.Float64()
	if err := s.sma.Update(close); err != nil {
		return nil, fmt.Errorf("smatrend: updating sma: %w", err)
	}
	if !s.sma.Ready() {
		return nil, nil
	}

	smaValue := s.sma.Value()
	aboveSMA := close > smaValue
	crossedAbove := s.cross.update(aboveSMA)

	side := currentPositionSide(view, s.instrumentID)

	switch side {
	case order.Flat:
		return s.onFlat(ctx, event, crossedAbove, close, smaValue)
	case order.Long:
		return s.onLong(ctx, event, close, smaValue)
	case order.Short:
		// smatrend never emits a Sell Enter/TargetExposure intent, so a
		// Short position should be unreachable here. Reported as an
		// error rather than silently ignored: it would mean some other
		// path (a different strategy sharing this account, a manual
		// order) put this account into a state smatrend's own
		// long-only design never anticipated.
		return nil, fmt.Errorf("smatrend: unexpected short position for long-only strategy")
	default:
		return nil, fmt.Errorf("smatrend: unrecognized position side %v", side)
	}
}

// onFlat handles a bar observed with no open position: a fresh
// cross-above enters long; anything else, including continuously
// remaining above the SMA after a stop exit, does nothing (EQS-01's
// own "require a fresh cross" rule, enforced by crossState itself —
// see its own doc comment).
func (s *Strategy) onFlat(ctx context.Context, event strategy.BarEvent, crossedAbove bool, close, smaValue float64) ([]order.Intent, error) {
	s.resetTrailingState()
	if !crossedAbove {
		return nil, nil
	}

	in, err := s.intents.Enter(s.instrumentID, order.Buy)
	if err != nil {
		return nil, err
	}
	if err := s.recordSignal(ctx, event, close, smaValue, "enter-long", []order.Intent{in}); err != nil {
		return nil, err
	}
	return []order.Intent{in}, nil
}

// onLong handles a bar observed with an open long position that
// survived the bar (see OnBar's own doc comment for why any stop
// trigger against this bar's own price action has already resolved by
// this point): ratchet the high-water mark from this bar's own High,
// and — only if the resulting stop level is strictly higher than the
// last one this Strategy itself placed — emit an AdjustStop intent.
// The monotonic-upward-only guarantee is enforced right here: a lower
// or equal computed stop simply emits nothing, never a downward
// adjustment.
func (s *Strategy) onLong(ctx context.Context, event strategy.BarEvent, close, smaValue float64) ([]order.Intent, error) {
	high := event.Bar.High
	if s.highWaterMark == nil || high.Cmp(*s.highWaterMark) > 0 {
		s.highWaterMark = &high
	}

	newStop, err := s.highWaterMark.MulRate(s.retainFraction)
	if err != nil {
		return nil, fmt.Errorf("smatrend: computing trailing stop from high-water mark: %w", err)
	}

	if s.stopPrice != nil && newStop.Cmp(*s.stopPrice) <= 0 {
		return nil, nil
	}

	in, err := s.intents.AdjustStop(s.instrumentID, newStop)
	if err != nil {
		return nil, err
	}
	s.stopPrice = &newStop

	if err := s.recordSignal(ctx, event, close, smaValue, "adjust-stop", []order.Intent{in}); err != nil {
		return nil, err
	}
	return []order.Intent{in}, nil
}

// resetTrailingState clears the high-water mark and current stop
// level, so a later re-entry starts a fresh trailing episode rather
// than resuming stale state from a previous one. Safe to call when
// already clear (the common case, before any position has ever been
// held).
func (s *Strategy) resetTrailingState() {
	s.highWaterMark = nil
	s.stopPrice = nil
}

// recordSignal journals one KindSignal decision-evidence record for
// this bar, if a Journal was configured (an Environment built for a
// test that doesn't need decision evidence may leave it nil).
// CorrelationID is the emitted intents' own.
func (s *Strategy) recordSignal(ctx context.Context, event strategy.BarEvent, close, smaValue float64, action string, intents []order.Intent) error {
	if s.journal == nil {
		return nil
	}

	values := map[string]string{
		"close":  strconv.FormatFloat(close, 'f', -1, 64),
		"sma":    strconv.FormatFloat(smaValue, 'f', -1, 64),
		"action": action,
	}
	if s.highWaterMark != nil {
		values["high_water_mark"] = s.highWaterMark.String()
	}
	if s.stopPrice != nil {
		values["stop_price"] = s.stopPrice.String()
	}

	var corr id.CorrelationID
	if len(intents) > 0 {
		corr = intents[0].Metadata.CorrelationID
	}

	rec, err := journal.NewRecord(journal.Record{
		RunID: s.runID,
		Metadata: id.Metadata{
			CorrelationID: corr,
			Timestamp:     event.Bar.Time,
		},
		Kind: journal.KindSignal,
		Signal: &journal.Signal{
			Strategy: Name,
			Values:   values,
		},
	})
	if err != nil {
		return fmt.Errorf("smatrend: building signal record: %w", err)
	}
	return s.journal.Record(ctx, rec)
}

// currentPositionSide returns view's own current side for instID, or
// order.Flat if no open position names it — an account's Positions
// only ever lists non-flat positions (account.Snapshot's own
// contract), so flat is correctly the default for an instrument this
// account has no entry for at all.
func currentPositionSide(view strategy.View, instID instrument.ID) order.PositionSide {
	for _, p := range view.Account().Positions() {
		if p.Listing.InstrumentID().Equal(instID) {
			return p.Side
		}
	}
	return order.Flat
}
