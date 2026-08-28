package strategy

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/account"
	"github.com/rustyeddy/trader/clock"
	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/logging"
	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
)

// fakeView is a minimal View test double: an account.Snapshot fixed at
// construction, standing in for whatever a real runner would compute
// from already-visible state.
type fakeView struct {
	snap account.Snapshot
}

func (v fakeView) Account() account.Snapshot { return v.snap }

func mustSnapshot(t *testing.T) account.Snapshot {
	t.Helper()
	c := clock.NewSimulated(testStart)
	gen := id.NewGenerator(c, id.NewDeterministic(3, 4))
	accountID, err := id.GenerateAccountID(gen)
	require.NoError(t, err)
	usd := num.MustParseCurrency("USD")
	snap, err := account.NewSnapshot(account.SnapshotParams{
		AccountID:       accountID,
		Broker:          "sim",
		Currency:        usd,
		AsOf:            testStart,
		CashBalances:    []num.Money{num.MustParseMoney("10000", usd)},
		Equity:          num.MustParseMoney("10000", usd),
		BuyingPower:     num.MustParseMoney("10000", usd),
		MarginUsed:      num.MustParseMoney("0", usd),
		MarginAvailable: num.MustParseMoney("10000", usd),
		RealizedPnL:     num.MustParseMoney("0", usd),
		UnrealizedPnL:   num.MustParseMoney("0", usd),
		Fees:            num.MustParseMoney("0", usd),
		Financing:       num.MustParseMoney("0", usd),
	})
	require.NoError(t, err)
	return snap
}

func mustEurUsdInstrumentID(t *testing.T) instrument.ID {
	t.Helper()
	inst, err := instrument.NewCurrencyPair(num.MustParseCurrency("EUR"), num.MustParseCurrency("USD"))
	require.NoError(t, err)
	return inst.ID()
}

// buyOnFirstBarStrategy is a minimal, representative Strategy
// implementation (issue #210's own "tests demonstrate representative
// strategy invocation and intent emission" acceptance criterion, the
// same "test double, not a real trading strategy" scope risk's own
// fakeRule established): it enters long on the first bar it ever
// sees, and does nothing on every bar after — enough to exercise
// Describe/Start/OnBar and IntentFactory together without inventing
// real trading logic this issue does not need.
//
// It retains the IntentFactory Start receives via env and calls it
// from OnBar — the same lifecycle contract a real strategy follows:
// the runtime injects capabilities once, in Start, and strategy logic
// uses those retained capabilities across every later OnBar call,
// since OnBar itself never receives an Environment (review feedback
// on PR #228).
type buyOnFirstBarStrategy struct {
	instID  instrument.ID
	intents IntentFactory
	entered bool
}

func (s *buyOnFirstBarStrategy) Describe() Descriptor {
	return Descriptor{
		Name:    "buy_on_first_bar",
		Version: "v0",
		Requirements: []DataRequirement{
			{Instrument: s.instID, Interval: marketdata.H1, WarmupBars: 0},
		},
	}
}

func (s *buyOnFirstBarStrategy) Start(ctx context.Context, env Environment) error {
	s.intents = env.Intents
	return nil
}

func (s *buyOnFirstBarStrategy) OnBar(ctx context.Context, event BarEvent, view View) ([]order.Intent, error) {
	if s.entered {
		return nil, nil
	}
	s.entered = true
	in, err := s.intents.Enter(s.instID, order.Buy)
	if err != nil {
		return nil, err
	}
	return []order.Intent{in}, nil
}

var _ Strategy = (*buyOnFirstBarStrategy)(nil)

// TestStrategy_RepresentativeInvocation drives a Strategy through the
// real contract end to end: Describe, Start with an Environment, and
// OnBar with a View — proving a strategy can retain Start's injected
// IntentFactory and use it from OnBar to actually emit an
// order.Intent, the way a runner would exercise it.
func TestStrategy_RepresentativeInvocation(t *testing.T) {
	ctx := context.Background()
	instID := mustEurUsdInstrumentID(t)
	s := &buyOnFirstBarStrategy{instID: instID}

	desc := s.Describe()
	assert.Equal(t, "buy_on_first_bar", desc.Name)
	require.Len(t, desc.Requirements, 1)
	assert.Equal(t, instID, desc.Requirements[0].Instrument)

	c := clock.NewSimulated(testStart)
	gen := id.NewGenerator(c, id.NewDeterministic(1, 2))
	env := Environment{
		Clock:   c,
		Intents: NewIntentFactory(c, gen, id.Source(desc.Name)),
		Logger:  logging.Discard(),
	}
	require.NoError(t, s.Start(ctx, env))

	bar := marketdata.Bar{
		Time:  testStart,
		Open:  num.MustParsePrice("1.10000"),
		High:  num.MustParsePrice("1.10500"),
		Low:   num.MustParsePrice("1.09500"),
		Close: num.MustParsePrice("1.10200"),
	}
	event := BarEvent{Instrument: instID, Interval: marketdata.H1, Bar: bar}
	view := fakeView{snap: mustSnapshot(t)}

	intents, err := s.OnBar(ctx, event, view)
	require.NoError(t, err)
	require.Len(t, intents, 1, "the first bar must emit exactly the one Enter intent this fixture describes")
	assert.Equal(t, order.IntentEnter, intents[0].Kind)
	assert.Equal(t, instID, intents[0].Instrument)
	assert.Equal(t, order.Buy, intents[0].Side)
	assert.False(t, intents[0].IntentID.IsZero())

	// A second OnBar call on the same strategy instance is a no-op,
	// proving state carries across calls the way a real strategy's
	// own internal state would.
	intents, err = s.OnBar(ctx, event, view)
	require.NoError(t, err)
	assert.Empty(t, intents)
	assert.True(t, s.entered)
}
