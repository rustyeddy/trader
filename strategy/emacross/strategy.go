package emacross

import (
	"context"
	"fmt"
	"strconv"

	"github.com/rustyeddy/trader/indicator"
	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
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
	s.intents = env.Intents
	return nil
}

// OnBar implements strategy.Strategy.
func (s *Strategy) OnBar(ctx context.Context, event strategy.BarEvent, view strategy.View) ([]order.Intent, error) {
	sample, err := priceToFloat64(event.Bar.Close)
	if err != nil {
		return nil, fmt.Errorf("emacross: converting bar close to float64: %w", err)
	}

	if err := s.fast.Update(sample); err != nil {
		return nil, fmt.Errorf("emacross: updating fast EMA: %w", err)
	}
	if err := s.slow.Update(sample); err != nil {
		return nil, fmt.Errorf("emacross: updating slow EMA: %w", err)
	}

	if !s.fast.Ready() || !s.slow.Ready() {
		return nil, nil
	}

	current := classifyRelation(s.fast.Value(), s.slow.Value())
	bullish, bearish := s.cross.update(current)
	if !bullish && !bearish {
		return nil, nil
	}

	side := currentPositionSide(view, s.instrumentID)
	if bullish {
		return s.actOnCross(side, order.Buy)
	}
	return s.actOnCross(side, order.Sell)
}

// actOnCross builds the intents for one detected crossover, per
// Decision 4's flat/long/short transition table. want is the side this
// crossover favors (Buy for a bullish cross, Sell for a bearish one).
func (s *Strategy) actOnCross(side order.PositionSide, want order.Side) ([]order.Intent, error) {
	wantsLong := want == order.Buy

	switch side {
	case order.Flat:
		in, err := s.intents.Enter(s.instrumentID, want)
		if err != nil {
			return nil, err
		}
		return []order.Intent{in}, nil

	case order.Long:
		if wantsLong {
			// A bullish cross while already long cannot occur given a
			// correctly maintained crossState (the prior bullish cross
			// that opened this long already consumed the Below->Above
			// transition), but returning no intent here is still the
			// correct, safe response rather than assuming that
			// invariant holds.
			return nil, nil
		}
		return s.reverse(want)

	case order.Short:
		if !wantsLong {
			return nil, nil
		}
		return s.reverse(want)

	default:
		return nil, fmt.Errorf("emacross: unrecognized position side %v", side)
	}
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

// priceToFloat64 converts a canonical, exact num.Price into the
// float64 sample indicator.EMA consumes — an analytical conversion,
// not an accounting one (package doc comment). num.Price.String
// always produces plain decimal text, never scientific notation, so
// strconv.ParseFloat cannot fail for any value num.Price itself can
// represent; the error return exists only because ParseFloat's own
// signature has one, not because a real failure is expected here.
func priceToFloat64(p num.Price) (float64, error) {
	return strconv.ParseFloat(p.String(), 64)
}
