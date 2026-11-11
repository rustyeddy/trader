// Command studyscanner is a test fixture: a BarsConsumer over EURUSD and
// GBPUSD at H1 that emits one signal per snapshot and never an intent, so
// it exercises a model (study) run end to end.
package main

import (
	"context"
	"log"

	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/sdk"
)

type scanner struct {
	universe []instrument.ID
	interval marketdata.Interval
}

var _ sdk.BarsConsumer = (*scanner)(nil)

func main() {
	usd := num.MustParseCurrency("USD")
	interval, err := marketdata.NewInterval(marketdata.UnitHour, 1)
	if err != nil {
		log.Fatal(err)
	}
	s := &scanner{interval: interval}
	for _, base := range []string{"EUR", "GBP"} {
		s.universe = append(s.universe, instrument.CurrencyPairID(num.MustParseCurrency(base), usd))
	}
	if err := sdk.Serve(s); err != nil {
		log.Fatal(err)
	}
}

func (s *scanner) Describe() sdk.Descriptor {
	reqs := make([]sdk.DataRequirement, len(s.universe))
	for i, id := range s.universe {
		reqs[i] = sdk.DataRequirement{Instrument: id, Interval: s.interval}
	}
	return sdk.Descriptor{Name: "study-scanner", Version: "0.1.0", Requirements: reqs}
}

func (s *scanner) Start(context.Context, sdk.Environment) error { return nil }

func (s *scanner) OnBars(_ context.Context, ev sdk.BarsEvent, _ sdk.View) ([]sdk.DescribedIntent, []sdk.DescribedSignal, error) {
	return nil, []sdk.DescribedSignal{{
		Strategy: "study-scanner",
		Values:   map[string]string{"boundary": ev.Boundary.Format("2006-01-02T15:04:05Z")},
	}}, nil
}
