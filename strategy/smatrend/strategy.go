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
const Version = "v1"

// Strategy is the SMA-trend baseline strategy.Strategy implementation
// (issue #335, EQS-01), extended with pluggable exit/re-entry rules
// (issue #347). It owns one indicator.SMA instance, the cross-above
// state machine that interprets it, and delegates the protective exit
// while long (ExitRule) and the re-entry decision after a stop-out
// (ReEntryRule) to whichever implementations Config names. See the
// package doc comment for the exact semantics, and
// docs/research/eqs-01-baseline-sma-trend.org for the reference SPY
// configuration and backtest results (from before issue #347's rule
// pluggability existed — those results used the DefaultExitRuleName/
// DefaultReEntryRuleName rules, unchanged by this revision).
//
// Strategy is not safe for concurrent use, and not reusable across
// runs: construct a fresh Strategy (via New) for each run, matching
// strategy.Environment's own "strategy state must not be reused"
// requirement documented on service/backtest.RunRequest.Strategy.
type Strategy struct {
	instrumentID instrument.ID
	interval     marketdata.Interval
	config       Config

	sma   *indicator.SMA
	cross crossState

	exitRule    ExitRule
	reEntryRule ReEntryRule

	// everExited is false until the first exit (however triggered) has
	// occurred. The very first entry ever always uses the cross-
	// above-SMA trigger directly, never reEntryRule — see onFlat.
	everExited bool
	// sideLastBar records this instrument's own position side as
	// observed on the *previous* OnBar call, so a transition (Flat->Long
	// or Long->Flat) can be detected exactly once, on the bar it
	// actually happens, rather than re-triggering OnEntry/OnExit on
	// every subsequent bar spent in the same side. The zero value,
	// order.Flat, is correct before the first OnBar call.
	sideLastBar order.PositionSide
	// lastStop is exitRule's own most recently returned NewStop, kept
	// here so onFlat can hand it to reEntryRule.OnExit as "the level
	// this strategy was protecting at" the moment a position exits —
	// see ReEntryRule.OnExit's own doc comment for why this is
	// deliberately the rule's last-known intended stop, not
	// necessarily the real broker fill price.
	lastStop *num.Price

	intents strategy.IntentFactory
	journal journal.Recorder // nil unless env.Journal was set
	runID   id.RunID
}

// New returns a Strategy trading instrumentID on interval, configured
// by config. It returns config's own Validate error, if any, or an
// error from constructing config's own named ExitRule/ReEntryRule.
func New(instrumentID instrument.ID, interval marketdata.Interval, config Config) (*Strategy, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	sma, err := indicator.NewSMA(config.SMAPeriod)
	if err != nil {
		return nil, err
	}
	exitRule, err := exitRuleRegistry[config.exitRuleName()](config)
	if err != nil {
		return nil, fmt.Errorf("smatrend: constructing exit rule %q: %w", config.exitRuleName(), err)
	}
	reEntryRule, err := reEntryRuleRegistry[config.reEntryRuleName()](config)
	if err != nil {
		return nil, fmt.Errorf("smatrend: constructing reentry rule %q: %w", config.reEntryRuleName(), err)
	}
	return &Strategy{
		instrumentID: instrumentID,
		interval:     interval,
		config:       config,
		sma:          sma,
		exitRule:     exitRule,
		reEntryRule:  reEntryRule,
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
		if s.sideLastBar == order.Long {
			s.onExit(event.Bar)
		}
		s.sideLastBar = order.Flat
		return s.onFlat(ctx, event, crossedAbove, close, smaValue)
	case order.Long:
		if s.sideLastBar != order.Long {
			s.exitRule.OnEntry(event.Bar)
		}
		s.sideLastBar = order.Long
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

// onExit notifies reEntryRule that a position just closed — whether
// via a broker-triggered stop (ADR-026) or a direct
// ExitRule.ExitNow — using exitRule's own last-known intended stop
// level as the exit price (see ReEntryRule.OnExit's own doc comment
// for why). Called exactly once per exit, from OnBar's own
// Flat-transition detection.
func (s *Strategy) onExit(exitBar marketdata.Bar) {
	exitPrice := exitBar.Close
	if s.lastStop != nil {
		exitPrice = *s.lastStop
	}
	s.reEntryRule.OnExit(exitPrice, exitBar)
	s.everExited = true
	s.lastStop = nil
}

// onFlat handles a bar observed with no open position. The very first
// entry ever (before onExit has ever run) always requires a fresh
// cross above the SMA; every entry after that delegates to
// reEntryRule instead (issue #347) — see ReEntryRule's own doc
// comment for why the split happens exactly here.
func (s *Strategy) onFlat(ctx context.Context, event strategy.BarEvent, crossedAbove bool, close, smaValue float64) ([]order.Intent, error) {
	var enter bool
	if !s.everExited {
		enter = crossedAbove
	} else {
		enter = s.reEntryRule.ShouldEnter(ReEntryContext{Bar: event.Bar, CrossedAboveSMA: crossedAbove})
	}
	if !enter {
		return nil, nil
	}

	in, err := s.intents.Enter(s.instrumentID, order.Buy)
	if err != nil {
		return nil, err
	}
	if err := s.recordSignal(ctx, event, close, smaValue, "enter-long", nil, []order.Intent{in}); err != nil {
		return nil, err
	}
	return []order.Intent{in}, nil
}

// onLong handles a bar observed with an open long position that
// survived the bar (see OnBar's own doc comment for why any stop
// trigger against this bar's own price action has already resolved by
// this point): delegate to exitRule for whatever protective action
// (if any) it decides, then translate that decision into the
// corresponding intent.
func (s *Strategy) onLong(ctx context.Context, event strategy.BarEvent, close, smaValue float64) ([]order.Intent, error) {
	decision, err := s.exitRule.OnLongBar(event.Bar, smaValue)
	if err != nil {
		return nil, fmt.Errorf("smatrend: exit rule: %w", err)
	}

	if decision.ExitNow {
		in, err := s.intents.Exit(s.instrumentID)
		if err != nil {
			return nil, err
		}
		if err := s.recordSignal(ctx, event, close, smaValue, "exit-now", nil, []order.Intent{in}); err != nil {
			return nil, err
		}
		return []order.Intent{in}, nil
	}

	if decision.NewStop == nil {
		return nil, nil
	}

	in, err := s.intents.AdjustStop(s.instrumentID, *decision.NewStop)
	if err != nil {
		return nil, err
	}
	s.lastStop = decision.NewStop

	if err := s.recordSignal(ctx, event, close, smaValue, "adjust-stop", decision.NewStop, []order.Intent{in}); err != nil {
		return nil, err
	}
	return []order.Intent{in}, nil
}

// recordSignal journals one KindSignal decision-evidence record for
// this bar, if a Journal was configured (an Environment built for a
// test that doesn't need decision evidence may leave it nil).
// CorrelationID is the emitted intents' own.
func (s *Strategy) recordSignal(ctx context.Context, event strategy.BarEvent, close, smaValue float64, action string, stopPrice *num.Price, intents []order.Intent) error {
	if s.journal == nil {
		return nil
	}

	values := map[string]string{
		"close":        strconv.FormatFloat(close, 'f', -1, 64),
		"sma":          strconv.FormatFloat(smaValue, 'f', -1, 64),
		"action":       action,
		"exit_rule":    s.config.exitRuleName(),
		"reentry_rule": s.config.reEntryRuleName(),
	}
	if stopPrice != nil {
		values["stop_price"] = stopPrice.String()
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
