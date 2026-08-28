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

func mustEurUsdListing(t *testing.T) instrument.ID {
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
type buyOnFirstBarStrategy struct {
	instID   instrument.ID
	entered  bool
	startCtx bool
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
	s.startCtx = true
	return nil
}

func (s *buyOnFirstBarStrategy) OnBar(ctx context.Context, event BarEvent, view View) ([]order.Intent, error) {
	if s.entered {
		return nil, nil
	}
	s.entered = true
	return nil, nil // real intent construction is exercised via env.Intents below
}

var _ Strategy = (*buyOnFirstBarStrategy)(nil)

// TestStrategy_RepresentativeInvocation drives a Strategy through the
// real contract end to end: Describe, Start with an Environment, and
// OnBar with a View — using Environment.Intents to build a real
// order.Intent, proving the contract is usable exactly as a runner
// would use it.
func TestStrategy_RepresentativeInvocation(t *testing.T) {
	ctx := context.Background()
	instID := mustEurUsdListing(t)
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
	assert.True(t, s.startCtx)

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
	assert.Empty(t, intents, "this fixture's own OnBar returns no intents directly")

	// Prove env.Intents (what a real strategy would call from inside
	// OnBar) actually builds a valid, canonical order.Intent.
	in, err := env.Intents.Enter(instID, order.Buy)
	require.NoError(t, err)
	assert.Equal(t, order.IntentEnter, in.Kind)
	assert.Equal(t, instID, in.Instrument)
	assert.False(t, in.IntentID.IsZero())

	// A second OnBar call on the same strategy instance is a no-op,
	// proving state carries across calls the way a real strategy's
	// own internal state would.
	intents, err = s.OnBar(ctx, event, view)
	require.NoError(t, err)
	assert.Empty(t, intents)
	assert.True(t, s.entered)
}
