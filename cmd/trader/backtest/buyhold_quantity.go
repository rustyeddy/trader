package backtest

import (
	"context"
	"time"

	"github.com/rustyeddy/trader/instrument"
	runtimeorder "github.com/rustyeddy/trader/internal/order"
	"github.com/rustyeddy/trader/internal/strategy"
	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
)

// buyHoldQuantityVersion distinguishes buy-and-hold's quantity mode from
// the fixed-fraction demo mode (Version "v0") in the manifest.
const buyHoldQuantityVersion = "quantity-v1"

// buyHoldQuantity is buy-and-hold's quantity mode (issue #417): the
// settled Buy & Hold baseline contract.
//
//	instrument  what to buy (the run's single --symbol / backtest.symbol)
//	quantity    exactly how much, never sized or resized
//	buy_date    when: the first bar at or after it (default: the first bar)
//	sell_date   optional: exit on the first bar at or after it
//
// The strategy says what to buy and when; Trader decides whether that
// quantity is admissible (initial margin, ADR-066) and the simulator
// decides the fill. The entry is a TargetExposure intent carrying the
// exact quantity, so no sizer is involved. It is attempted once: a
// quantity the account can't finance is rejected, never resized, and
// not retried.
//
// Intents are decided on a bar's close and fill at the next bar's open
// (the Scheduler's next-bar-open rule), so "the first bar at or after
// buy_date" is when the decision is made.
//
// The run's end date is not a sell. Without sell_date the position
// stays open in the final account state, valued at the final mark; no
// closing trade is synthesized. The exit is emitted only when the
// account actually holds the position (read from View), so a refused
// entry never produces an exit.
type buyHoldQuantity struct {
	instrumentID instrument.ID
	interval     marketdata.Interval
	params       buyHoldQuantityParams
	buyDate      time.Time
	sellDate     *time.Time

	intents strategy.IntentFactory
	entered bool
	exited  bool
}

// buyHoldQuantityParams is quantity mode's strategy parameters, as
// recorded in the run manifest (and so in config_digest).
type buyHoldQuantityParams struct {
	Name     string       `json:"name"`
	Mode     string       `json:"mode"`
	Quantity num.Quantity `json:"quantity"`
	BuyDate  string       `json:"buy_date,omitempty"`
	SellDate string       `json:"sell_date,omitempty"`
}

func newBuyHoldQuantity(instrumentID instrument.ID, interval marketdata.Interval, quantity num.Quantity, buyDate time.Time, sellDate *time.Time, buyDateText, sellDateText string) *buyHoldQuantity {
	return &buyHoldQuantity{
		instrumentID: instrumentID,
		interval:     interval,
		buyDate:      buyDate,
		sellDate:     sellDate,
		params: buyHoldQuantityParams{
			Name:     demoStrategyName,
			Mode:     "quantity",
			Quantity: quantity,
			BuyDate:  buyDateText,
			SellDate: sellDateText,
		},
	}
}

func (s *buyHoldQuantity) Describe() strategy.Descriptor {
	return strategy.Descriptor{
		Name:         demoStrategyName,
		Version:      buyHoldQuantityVersion,
		Requirements: []strategy.DataRequirement{{Instrument: s.instrumentID, Interval: s.interval}},
	}
}

func (s *buyHoldQuantity) Start(ctx context.Context, env strategy.Environment) error {
	s.intents = env.Intents
	return nil
}

func (s *buyHoldQuantity) OnBar(ctx context.Context, event strategy.BarEvent, view strategy.View) ([]runtimeorder.Intent, error) {
	if !event.Instrument.Equal(s.instrumentID) {
		return nil, nil
	}
	at := event.Bar.Time

	if s.sellDate != nil && !at.Before(*s.sellDate) {
		if s.exited || !s.holding(view) {
			return nil, nil
		}
		s.exited = true
		in, err := s.intents.Exit(s.instrumentID)
		if err != nil {
			return nil, err
		}
		return []runtimeorder.Intent{in}, nil
	}

	if s.entered || at.Before(s.buyDate) {
		return nil, nil
	}
	s.entered = true
	in, err := s.intents.TargetExposure(s.instrumentID, order.Buy, s.params.Quantity)
	if err != nil {
		return nil, err
	}
	return []runtimeorder.Intent{in}, nil
}

// holding reports whether the account holds a position in the
// strategy's instrument.
func (s *buyHoldQuantity) holding(view strategy.View) bool {
	for _, p := range view.Account().Positions() {
		if p.Listing.InstrumentID().Equal(s.instrumentID) && p.Side != order.Flat {
			return true
		}
	}
	return false
}
