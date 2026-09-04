package emacross

import (
	"context"
	"fmt"
	"strconv"

	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/indicator"
	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/journal"
	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/order"
	"github.com/rustyeddy/trader/strategy"
)

// Name identifies this strategy in Descriptor.Name and, conventionally,
// as the Source recorded on every Intent its IntentFactory builds.
const Name = "ema-cross"

// Version distinguishes revisions of this strategy's own logic.
const Version = "v0"

// Strategy is the EMA crossover strategy.Strategy implementation
// (issue #249, EMA-04). It owns two indicator.EMA instances and the
// crossover state machine that interprets them; see the package doc
// comment and docs/research/ema-01-experiment-definition.org for the
// exact semantics.
//
// Strategy is not safe for concurrent use, and not reusable across
// runs: construct a fresh Strategy (via New) for each run, matching
// strategy.Environment's own "strategy state must not be reused"
// requirement documented on service/backtest.RunRequest.Strategy.
type Strategy struct {
	instrumentID instrument.ID
	interval     marketdata.Interval
	config       Config

	fast  *indicator.EMA
	slow  *indicator.EMA
	cross crossState

	intents strategy.IntentFactory
	journal journal.Recorder // nil unless env.Journal was set (issue #253, EMA-08)
	runID   id.RunID
}

// New returns a Strategy trading instrumentID on interval, configured
// by config. It returns config's own Validate error, if any.
func New(instrumentID instrument.ID, interval marketdata.Interval, config Config) (*Strategy, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	fast, err := indicator.NewEMA(config.FastPeriod)
	if err != nil {
		return nil, err
	}
	slow, err := indicator.NewEMA(config.SlowPeriod)
	if err != nil {
		return nil, err
	}
	return &Strategy{
		instrumentID: instrumentID,
		interval:     interval,
		config:       config,
		fast:         fast,
		slow:         slow,
	}, nil
}

// Config returns this strategy's own configuration, for a composition
// root that wants to record it (for example in a run manifest)
// without keeping a second copy of the same values.
func (s *Strategy) Config() Config {
	return s.config
}

// Describe implements strategy.Strategy. WarmupBars equals SlowPeriod
// (Decision 2): both EMAs use SMA seeding, so the slow EMA — the
// later of the two to become ready — becomes ready on exactly the
// SlowPeriod-th bar, and the runtime's own warm-up discard keeps that
// bar's own (necessarily crossover-less, see Decision 3) intents from
// ever reaching execution.
func (s *Strategy) Describe() strategy.Descriptor {
	return strategy.Descriptor{
		Name:    Name,
		Version: Version,
		Requirements: []strategy.DataRequirement{
			{Instrument: s.instrumentID, Interval: s.interval, WarmupBars: s.config.SlowPeriod},
		},
	}
}

// Start implements strategy.Strategy.
func (s *Strategy) Start(ctx context.Context, env strategy.Environment) error {
	if env.Journal != nil && env.RunID.IsZero() {
		return fmt.Errorf("emacross: env.Journal is set but env.RunID is zero: every recorded signal needs a run id")
	}
	s.intents = env.Intents
	s.journal = env.Journal
	s.runID = env.RunID
	return nil
}

// OnBar implements strategy.Strategy.
func (s *Strategy) OnBar(ctx context.Context, event strategy.BarEvent, view strategy.View) ([]order.Intent, error) {
	// event.Bar.Close.Float64() is ADR-045's explicit exact-to-analytical
	// conversion boundary: a direct numeric conversion, never a
	// String()/strconv.ParseFloat() round-trip.
	sample := event.Bar.Close.Float64()

	if err := s.fast.Update(sample); err != nil {
		return nil, fmt.Errorf("emacross: updating fast EMA: %w", err)
	}
	if err := s.slow.Update(sample); err != nil {
		return nil, fmt.Errorf("emacross: updating slow EMA: %w", err)
	}

	if !s.fast.Ready() || !s.slow.Ready() {
		return nil, nil
	}

	// prevRelation/havePrev capture crossState's own "last non-tie
	// relation" *before* this bar's update, purely so recordSignal can
	// report the exact evidence Decision 1's state machine actually
	// compared against — not the more confusing (and sometimes
	// unavailable, pre-Tie) raw previous bar relation.
	prevRelation, havePrev := s.cross.last, s.cross.have
	current := classifyRelation(s.fast.Value(), s.slow.Value())
	bullish, bearish := s.cross.update(current)

	// A signal is recorded only at a genuine decision boundary — a
	// detected crossover — never for every ready bar (PR #264 review):
	// with no crossover, there is no new evidence beyond what the last
	// recorded signal already established, and recording one anyway
	// would journal little more than "still nothing happened" on every
	// one of a run's bars.
	if !bullish && !bearish {
		return nil, nil
	}

	cross, want := "bearish", order.Sell
	if bullish {
		cross, want = "bullish", order.Buy
	}
	side := currentPositionSide(view, s.instrumentID)
	action, intents, err := s.actOnCross(side, want)
	if err != nil {
		return nil, err
	}

	if err := s.recordSignal(ctx, event, prevRelation, havePrev, current, cross, action, intents); err != nil {
		return nil, err
	}
	return intents, nil
}

// actOnCross builds the intents for one detected crossover, per
// Decision 4's flat/long/short transition table, and returns the
// action label recordSignal journals alongside them. want is the side
// this crossover favors (Buy for a bullish cross, Sell for a bearish
// one).
//
// Config.AllowedSide (issue #273) restricts entering the disallowed
// side: a flat->disallowed-side crossover is a no-op, and a would-be
// reversal into the disallowed side instead exits to flat only
// ("exit"), never opening the new, disallowed position. The
// unrestricted SideBoth case (the zero value) is textually unchanged
// from before this option existed.
func (s *Strategy) actOnCross(side order.PositionSide, want order.Side) (string, []order.Intent, error) {
	wantsLong := want == order.Buy
	allowed := wantsLong && s.config.AllowedSide.allowsLong() || !wantsLong && s.config.AllowedSide.allowsShort()

	switch side {
	case order.Flat:
		if !allowed {
			return "none", nil, nil
		}
		in, err := s.intents.Enter(s.instrumentID, want)
		if err != nil {
			return "none", nil, err
		}
		if wantsLong {
			return "enter-long", []order.Intent{in}, nil
		}
		return "enter-short", []order.Intent{in}, nil

	case order.Long:
		if wantsLong {
			// A bullish cross while already long cannot occur given a
			// correctly maintained crossState (the prior bullish cross
			// that opened this long already consumed the Below->Above
			// transition), but returning no intent here is still the
			// correct, safe response rather than assuming that
			// invariant holds.
			return "none", nil, nil
		}
		if !allowed {
			return s.exitOnly()
		}
		intents, err := s.reverse(want)
		return "reverse", intents, err

	case order.Short:
		if !wantsLong {
			return "none", nil, nil
		}
		if !allowed {
			return s.exitOnly()
		}
		intents, err := s.reverse(want)
		return "reverse", intents, err

	default:
		return "none", nil, fmt.Errorf("emacross: unrecognized position side %v", side)
	}
}

// exitOnly builds a single Exit intent — the AllowedSide-restricted
// counterpart to reverse (issue #273): the current position closes to
// flat, but no new position on the now-disallowed side opens.
func (s *Strategy) exitOnly() (string, []order.Intent, error) {
	in, err := s.intents.Exit(s.instrumentID)
	if err != nil {
		return "none", nil, err
	}
	return "exit", []order.Intent{in}, nil
}

// recordSignal journals one KindSignal decision-evidence record for
// this bar (issue #253, EMA-08), if a Journal was configured (an
// Environment built for a test that doesn't need decision evidence may
// leave it nil). CorrelationID is the emitted intents' own — zero when
// no action was taken, since there is then no execution-side record to
// correlate with.
func (s *Strategy) recordSignal(ctx context.Context, event strategy.BarEvent, prevRelation relation, havePrev bool, current relation, cross, action string, intents []order.Intent) error {
	if s.journal == nil {
		return nil
	}

	prevStr := "none"
	if havePrev {
		prevStr = prevRelation.String()
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
			Values: map[string]string{
				"fast_ema":      strconv.FormatFloat(s.fast.Value(), 'f', -1, 64),
				"slow_ema":      strconv.FormatFloat(s.slow.Value(), 'f', -1, 64),
				"prev_relation": prevStr,
				"curr_relation": current.String(),
				"cross":         cross,
				"action":        action,
			},
		},
	})
	if err != nil {
		return fmt.Errorf("emacross: building signal record: %w", err)
	}
	return s.journal.Record(ctx, rec)
}

// reverse builds the Exit+Enter intent pair for a long<->short
// reversal, both under one shared correlation ID (Decision 4): they
// are causally related for journaling/analysis, not a guarantee of
// atomic execution — the existing pipeline/risk/broker semantics
// remain authoritative for each leg independently.
func (s *Strategy) reverse(want order.Side) ([]order.Intent, error) {
	corr, err := s.intents.NewCorrelationID()
	if err != nil {
		return nil, err
	}
	f := s.intents.WithCorrelation(corr)

	exit, err := f.Exit(s.instrumentID)
	if err != nil {
		return nil, err
	}
	enter, err := f.Enter(s.instrumentID, want)
	if err != nil {
		return nil, err
	}
	return []order.Intent{exit, enter}, nil
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
