package strategies

import (
	"context"
	"fmt"

	"github.com/rustyeddy/trader/broker"
	"github.com/rustyeddy/trader/market"
)

// OpenOnceStrategy opens a single market order the first time it sees a tick
// for the configured instrument. It's meant as a wiring test.
type OpenOnceStrategy struct {
	Instrument string
	Units      float64

	opened bool
}

func (s *OpenOnceStrategy) OnTick(ctx context.Context, b broker.Broker, tick market.Tick) error {
	if s.opened {
		return nil
	}
	if tick.Instrument != s.Instrument {
		return nil
	}
	if s.Units == 0 {
		return fmt.Errorf("open-once: units must be non-zero")
	}

	_, err := b.CreateMarketOrder(ctx, broker.MarketOrderRequest{
		Instrument: s.Instrument,
		Units:      s.Units,
	})
	if err != nil {
		return err
	}
	s.opened = true
	return nil
}
