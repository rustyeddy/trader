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
//
// Bumped to "v2" by issue #368 (PR #369 review, blocker 4): a
// probation-trend entry/re-entry now submits a bracket
// order.IntentEnterWithStop instead of a plain order.IntentEnter,
// changing both entry-bar stop behavior and persisted signal
// semantics (a new "enter-long-with-stop" KindSignal action, see
// recordSignal). Per ADR-044, Descriptor.Version is the discriminator
// for exactly this kind of logic change: a "v1" run and a "v2" run of
// an otherwise-identical config are not directly comparable results.
const Version = "v2"

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

	exitRule         ExitRule
	reEntryRule      ReEntryRule
	initialEntryRule InitialEntryRule

	// everExited is false until the first exit (however triggered) has
	// occurred. The very first entry ever is governed by
	// initialEntryRule (not necessarily a fresh SMA cross — see
	// InitialEntryRule's own doc comment), never reEntryRule — see
	// onFlat.
	everExited bool
	// sideLastBar records this instrument's own position side as
	// observed on the *previous* OnBar call, so a transition (Flat->Long
	// or Long->Flat) can be detected exactly once, on the bar it
	// actually happens, rather than re-triggering OnEntry/OnExit on
	// every subsequent bar spent in the same side. The zero value,
	// order.Flat, is correct before the first OnBar call.
	sideLastBar order.PositionSide
	// lastStop is exitRule's own most recently returned NewStop, kept
	// here so onExit can hand it to reEntryRule.OnExit as "the level
	// this strategy was protecting at" the moment a broker-triggered
	// stop exit is observed — see ReEntryRule.OnExit's own doc comment
	// for why this is deliberately the rule's last-known intended
	// stop, not necessarily the real broker fill price. Always nil for
	// an ExitRule (like sma-cross) that never places a resting stop;
	// see directExitReference for that case instead.
	lastStop *num.Price
	// directExitReference is the bar Close observed at the exact
	// moment onLong emitted a direct order.IntentExit (ExitDecision.
	// ExitNow), captured here because the bar onExit later observes
	// Flat on (the fill bar) is not the bar that made the exit
	// decision and its Close is not a meaningful reference level (PR
	// #348 review). nil whenever the most recent exit was a
	// broker-triggered stop instead, in which case lastStop is used.
	directExitReference *num.Price
	// pendingInitialStop is the stop price used by the most recent
	// EnterWithStop intent onFlat emitted, carried forward exactly one
	// bar (issue #368) — nil whenever the most recent entry was a
	// plain Enter (no InitialStopProvider, or none configured). It is
	// consumed on the very next bar OnBar observes, one of three ways:
	//
	//   - Flat->Long transition: the bracket entry filled and survived
	//     the bar. exitRule.OnEntry runs its own default
	//     initialization, then — if exitRule also implements
	//     InitialStopSeeder — SeedInitialStop overrides the one field
	//     that default reset with this exact value (PR #369 review;
	//     see InitialStopSeeder's own doc comment for why this is a
	//     second call rather than a parameter on OnEntry itself).
	//   - Flat->Flat, with the account's own RealizedPnL unchanged
	//     since the last bar observed: the entry never filled at all
	//     (rejected outright) — simply discarded, see
	//     discardUnfilledBracket.
	//   - Flat->Flat, with RealizedPnL changed: the entry filled *and*
	//     its own attached stop triggered within the same bar,
	//     invisible to a side-only Flat/Long comparison (PR #369
	//     review, blocker 1) — see lastRealizedPnL's own doc comment.
	pendingInitialStop *num.Price
	// lastRealizedPnL is this Strategy's own last-observed
	// view.Account().RealizedPnL(), captured at the end of every OnBar
	// call regardless of return path (issue #368, PR #369 review). It
	// exists solely to detect the one lifecycle transition a bare
	// Flat/Long position-side comparison cannot see at all: a bracket
	// entry (order.IntentEnterWithStop) whose own attached stop
	// triggers within the same bar the entry itself fills.
	// sideLastBar stays order.Flat across both the bar before and the
	// bar of such an episode — OnBar never observes order.Long in
	// between — so without this signal, onExit would never run:
	// everExited would stay false, ReEntryRule.OnExit would never be
	// called, and the next entry would be incorrectly governed by
	// InitialEntryRule instead of the configured ReEntryRule.
	// RealizedPnL only changes when a position actually closes
	// (opening one does not move it), so a change observed while
	// pendingInitialStop is non-nil and side is Flat is conclusive
	// proof a full bracket round trip occurred.
	//
	// A change is sufficient but not necessary (PR #369 re-review): a
	// long bracket entry filling at this bar's own Open, already at or
	// below its own protective stop, realizes exactly zero PnL —
	// ADR-026's gap rule fills the stop at that identical Open price,
	// so entry and exit share one price. OnBar's own Flat branch
	// covers that case with a second, independent check
	// (event.Bar.Open against the bracket's own stop) that needs no
	// account state at all. Together the two checks are exhaustive for
	// every fill outcome this codebase's own fill models produce
	// today: a non-gap intrabar touch always realizes a strictly
	// negative PnL (the stop necessarily fills below the entry's own
	// Open), and a gap-through-Open fill always realizes exactly zero.
	// What remains unprovable from account/bar state alone is the
	// reverse case — a bracket entry rejected outright (no fill at
	// all) on a bar whose Open independently happens to sit at or
	// below whatever stop price would have been used. That coincidence
	// is unrelated to genuine price/risk causality and is not resolved
	// here; closing it fully would need a real fill-event signal (for
	// example a wired FillHandler capability) rather than inference
	// over account/bar snapshots.
	lastRealizedPnL num.Money

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
	initialEntryRule, err := initialEntryRuleRegistry[config.initialEntryModeName()](config)
	if err != nil {
		return nil, fmt.Errorf("smatrend: constructing initial entry rule %q: %w", config.initialEntryModeName(), err)
	}
	return &Strategy{
		instrumentID:     instrumentID,
		interval:         interval,
		config:           config,
		sma:              sma,
		exitRule:         exitRule,
		reEntryRule:      reEntryRule,
		initialEntryRule: initialEntryRule,
	}, nil
}

// Config returns this strategy's own configuration, for a composition
// root that wants to record it (for example in a run manifest)
// without keeping a second copy of the same values.
func (s *Strategy) Config() Config {
	return s.config
}

// phaseReporter is implemented by an ExitRule that tracks its own
// explicit Probation/Trending lifecycle (currently only
// probationTrendExitRule); every other ExitRule has no such
// distinction.
type phaseReporter interface{ Phase() Phase }

// Phase reports this Strategy's own current lifecycle phase (issue
// #349): always PhaseFlat while no position is open, and otherwise
// whatever the configured ExitRule itself reports through the
// optional phaseReporter capability — PhaseFlat for any ExitRule with
// no Probation/Trending distinction of its own (trailing-stop,
// sma-cross), and the real state for "probation-trend". Exposed so
// Probation/Trending is a first-class, queryable concept for
// journaling, debugging, and tests, rather than private state hidden
// inside one ExitRule implementation (PR #348/#349 review).
func (s *Strategy) Phase() Phase {
	if s.sideLastBar != order.Long {
		return PhaseFlat
	}
	if pr, ok := s.exitRule.(phaseReporter); ok {
		return pr.Phase()
	}
	return PhaseFlat
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
	// Captured once, up front: used both by the Flat-branch same-bar-
	// round-trip check below and to refresh lastRealizedPnL, via
	// defer, on every return path (issue #368, PR #369 review) —
	// including the warm-up early return, so lastRealizedPnL is never
	// stale relative to what this bar actually observed.
	currentRealizedPnL := view.Account().RealizedPnL()
	defer func() { s.lastRealizedPnL = currentRealizedPnL }()

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
		switch {
		case s.sideLastBar == order.Long:
			s.onExit(event.Bar)
		case s.pendingInitialStop != nil:
			roundTrip, err := realizedPnLChanged(currentRealizedPnL, s.lastRealizedPnL)
			if err != nil {
				return nil, fmt.Errorf("smatrend: comparing realized pnl: %w", err)
			}
			// A RealizedPnL change alone misses one real case (PR #369
			// re-review): a long bracket entry filling at this bar's
			// own Open, already at or below its own protective stop
			// (ADR-026's gap rule then fills that stop at the
			// identical Open price) realizes exactly zero PnL — entry
			// and exit at the same price. event.Bar.Open is knowable
			// directly from this bar's own data, independent of
			// account state, and conclusively proves that outcome
			// whenever it holds.
			gappedThroughStop := event.Bar.Open.Cmp(*s.pendingInitialStop) <= 0
			if roundTrip || gappedThroughStop {
				// The bracket entry filled and its own attached stop
				// triggered within this same bar (PR #369 review,
				// blocker 1) — invisible to sideLastBar, which never
				// observed order.Long in between.
				s.onExit(event.Bar)
			} else {
				s.discardUnfilledBracket()
			}
		}
		s.sideLastBar = order.Flat
		return s.onFlat(ctx, event, crossedAbove, aboveSMA, close, smaValue)
	case order.Long:
		if s.sideLastBar != order.Long {
			entryPrice, err := currentPositionAvgPrice(view, s.instrumentID)
			if err != nil {
				return nil, fmt.Errorf("smatrend: reading entry price: %w", err)
			}
			s.exitRule.OnEntry(event.Bar, entryPrice)
			if s.pendingInitialStop != nil {
				if seeder, ok := s.exitRule.(InitialStopSeeder); ok {
					seeder.SeedInitialStop(*s.pendingInitialStop)
				}
				s.pendingInitialStop = nil
			}
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

// realizedPnLChanged reports whether cur differs from prev, both
// account.Snapshot.RealizedPnL() values from the same account and
// therefore always the same currency (issue #368) — a mismatch would
// indicate the account itself changed underneath this Strategy, which
// is reported as an error rather than silently guessed at.
func realizedPnLChanged(cur, prev num.Money) (bool, error) {
	cmp, err := cur.Cmp(prev)
	if err != nil {
		return false, err
	}
	return cmp != 0, nil
}

// discardUnfilledBracket rolls back the speculative state
// buildEntryIntent's bracket path set for an entry attempt that never
// actually filled (issue #368): pendingInitialStop and lastStop, both
// set optimistically before the outcome was known. Called only from
// the Flat branch's own "no realized-pnl change" case.
func (s *Strategy) discardUnfilledBracket() {
	s.pendingInitialStop = nil
	s.lastStop = nil
}

// onExit notifies reEntryRule that a position just closed — whether
// via a broker-triggered stop (ADR-026) or a direct
// ExitRule.ExitNow — using the best available reference level for
// the exit price (see ReEntryRule.OnExit's own doc comment for why
// this is a reference level, not necessarily a real fill price):
// directExitReference when the most recent exit was a direct
// IntentExit (captured at the exact bar that decided it, not the
// later bar this exit is observed on), else lastStop when it was a
// broker-triggered stop, else — only possible for a custom ExitRule
// that manages neither — exitBar's own Close as a last resort. Called
// exactly once per exit, from OnBar's own Flat-transition detection —
// either the classic Long->Flat transition, or (issue #368) a bracket
// entry whose own attached stop triggered within the same bar the
// entry itself filled, detected via a RealizedPnL change while
// sideLastBar never observed order.Long at all. lastStop is already
// the correct reference for that second case too: buildEntryIntent's
// bracket path sets it to the bracket's own stop price at the moment
// the entry intent was built, and nothing overwrites it before onExit
// runs, since OnEntry/onLong were never reached.
func (s *Strategy) onExit(exitBar marketdata.Bar) {
	exitPrice := exitBar.Close
	switch {
	case s.directExitReference != nil:
		exitPrice = *s.directExitReference
	case s.lastStop != nil:
		exitPrice = *s.lastStop
	}
	s.reEntryRule.OnExit(exitPrice, exitBar)
	s.everExited = true
	s.lastStop = nil
	s.directExitReference = nil
	s.pendingInitialStop = nil
}

// onFlat handles a bar observed with no open position. The very first
// entry ever (before onExit has ever run) is governed by
// initialEntryRule (issue #349 review) — not necessarily a fresh
// cross, see InitialEntryRule's own doc comment. Every entry after
// that is gated by aboveSMA (the SMA Long Hold playbook's own
// re-entry invariant, PR #348 review): while price remains below the
// SMA, only a fresh cross re-enters, regardless of the configured
// ReEntryRule — there is nothing to "reclaim" or "break out of" in a
// regime this strategy does not consider bullish yet. Only once price
// is back above the SMA does reEntryRule get to decide how, within
// that regime, to resume (issue #347).
//
// reEntryRule.ObserveFlatBar is called for every flat bar once
// everExited is true, regardless of aboveSMA (PR #362 review) — after
// this bar's own enter/no-enter decision has already been computed,
// so a rule with continuous bar-by-bar state (for example
// nBarBreakoutReEntryRule's own rolling window) stays chronologically
// correct across a below-SMA stretch without that state ever
// influencing this same bar's own already-decided outcome.
func (s *Strategy) onFlat(ctx context.Context, event strategy.BarEvent, crossedAbove, aboveSMA bool, close, smaValue float64) ([]order.Intent, error) {
	var enter bool
	switch {
	case !s.everExited:
		enter = s.initialEntryRule.ShouldEnter(InitialEntryContext{CrossedAboveSMA: crossedAbove, AboveSMA: aboveSMA})
	case aboveSMA:
		enter = s.reEntryRule.ShouldEnter(ReEntryContext{Bar: event.Bar, CrossedAboveSMA: crossedAbove})
	default:
		enter = crossedAbove
	}
	if s.everExited {
		s.reEntryRule.ObserveFlatBar(event.Bar)
	}
	if !enter {
		return nil, nil
	}

	in, initialStop, err := s.buildEntryIntent(smaValue)
	if err != nil {
		return nil, err
	}
	action := "enter-long"
	if initialStop != nil {
		action = "enter-long-with-stop"
	}
	if err := s.recordSignal(ctx, event, close, smaValue, PhaseFlat, action, initialStop, []order.Intent{in}); err != nil {
		return nil, err
	}
	return []order.Intent{in}, nil
}

// buildEntryIntent builds this entry's own order.Intent: a bracket
// order.IntentEnterWithStop when exitRule implements
// InitialStopProvider (issue #368, ADR-059) — closing the entry-fill-
// bar protection gap for whichever ExitRule can compute its first
// stop before the fill — or a plain order.IntentEnter otherwise,
// unchanged from before this issue. The returned *num.Price, when
// non-nil, is both the stop actually placed and the value
// recordSignal journals as decision evidence.
//
// The bracket path sets both s.pendingInitialStop (for OnBar's own
// Flat->Long/Flat->Flat consumption, see that field's own doc
// comment) and s.lastStop (PR #369 review, blocker 2): without this,
// a bracket entry that survives its own fill bar with no ratcheting
// AdjustStop on the very next OnLongBar call (its computed stop no
// higher than the seeded floor) would leave lastStop nil despite a
// real resting stop existing, so onExit would later fall back to the
// exit bar's own Close instead of the real intended stop level when
// that resting stop eventually triggers — corrupting exit-price-based
// re-entry rules such as "reclaim-exit-price". Both are speculative
// until the outcome is known: OnBar's own Flat branch rolls them back
// via discardUnfilledBracket if the entry turns out to have never
// filled at all.
//
// smaValue is the entry-decision bar's own current SMA value — the
// only signal available before the fill — so InitialStop is
// necessarily computed from information no later than this bar; it
// never uses the eventual fill bar's own Close/High/Low, which is not
// yet known (issue #368's own explicit no-lookahead requirement).
func (s *Strategy) buildEntryIntent(smaValue float64) (order.Intent, *num.Price, error) {
	provider, ok := s.exitRule.(InitialStopProvider)
	if !ok {
		in, err := s.intents.Enter(s.instrumentID, order.Buy)
		if err != nil {
			return order.Intent{}, nil, err
		}
		s.pendingInitialStop = nil
		return in, nil, nil
	}

	stop, err := provider.InitialStop(smaValue)
	if err != nil {
		return order.Intent{}, nil, fmt.Errorf("smatrend: computing initial stop for bracket entry: %w", err)
	}
	in, err := s.intents.EnterWithStop(s.instrumentID, order.Buy, stop)
	if err != nil {
		return order.Intent{}, nil, err
	}
	s.pendingInitialStop = &stop
	s.lastStop = &stop
	return in, &stop, nil
}

// onLong handles a bar observed with an open long position that
// survived the bar (see OnBar's own doc comment for why any stop
// trigger against this bar's own price action has already resolved by
// this point): delegate to exitRule for whatever protective action
// (if any) it decides, then translate that decision into the
// corresponding intent.
func (s *Strategy) onLong(ctx context.Context, event strategy.BarEvent, close, smaValue float64) ([]order.Intent, error) {
	// Captured before OnLongBar runs: a same-bar Probation->Trending
	// activation must not make this bar's own journaled phase look
	// like the decision was made under the new phase (issue #349).
	phase := s.Phase()

	decision, err := s.exitRule.OnLongBar(event.Bar, smaValue)
	if err != nil {
		return nil, fmt.Errorf("smatrend: exit rule: %w", err)
	}
	if decision.ExitNow && decision.NewStop != nil {
		return nil, fmt.Errorf("smatrend: exit rule %q returned an ambiguous decision: ExitNow and NewStop are mutually exclusive", s.config.exitRuleName())
	}

	if decision.ExitNow {
		in, err := s.intents.Exit(s.instrumentID)
		if err != nil {
			return nil, err
		}
		// Captured now, at the exact bar that decided the exit — not
		// the later bar onExit observes Flat on, whose own Close bears
		// no relationship to why this position closed (PR #348 review).
		exitClose := event.Bar.Close
		s.directExitReference = &exitClose
		if err := s.recordSignal(ctx, event, close, smaValue, phase, "exit-now", nil, []order.Intent{in}); err != nil {
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

	if err := s.recordSignal(ctx, event, close, smaValue, phase, "adjust-stop", decision.NewStop, []order.Intent{in}); err != nil {
		return nil, err
	}
	return []order.Intent{in}, nil
}

// recordSignal journals one KindSignal decision-evidence record for
// this bar, if a Journal was configured (an Environment built for a
// test that doesn't need decision evidence may leave it nil). phase
// is the caller's own already-captured Strategy.Phase() (issue #349):
// callers capture it before invoking any exitRule method that might
// itself transition phase for the *next* bar, so a same-bar
// Probation->Trending activation is never journaled as though the
// decision were already made under the new phase. CorrelationID is
// the emitted intents' own.
func (s *Strategy) recordSignal(ctx context.Context, event strategy.BarEvent, close, smaValue float64, phase Phase, action string, stopPrice *num.Price, intents []order.Intent) error {
	if s.journal == nil {
		return nil
	}

	values := map[string]string{
		"close":        strconv.FormatFloat(close, 'f', -1, 64),
		"sma":          strconv.FormatFloat(smaValue, 'f', -1, 64),
		"action":       action,
		"exit_rule":    s.config.exitRuleName(),
		"reentry_rule": s.config.reEntryRuleName(),
		"phase":        phase.String(),
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

// currentPositionAvgPrice returns the real average fill price of
// instID's open position (issue #349 review's explicit preference
// for the actual entry/fill price over a signal-bar approximation),
// read directly from the account snapshot's own Position.AvgPrice.
// Only ever called on the bar OnBar first observes order.Long, when
// order.NewPosition's own invariant guarantees AvgPrice is non-nil.
func currentPositionAvgPrice(view strategy.View, instID instrument.ID) (num.Price, error) {
	for _, p := range view.Account().Positions() {
		if p.Listing.InstrumentID().Equal(instID) {
			if p.AvgPrice == nil {
				return num.Price{}, fmt.Errorf("smatrend: long position for %s has no AvgPrice", instID)
			}
			return *p.AvgPrice, nil
		}
	}
	return num.Price{}, fmt.Errorf("smatrend: no open position found for %s", instID)
}
