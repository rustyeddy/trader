// Command sdk-scanner is a minimal snapshot (OnBars) consumer, the
// cross-sectional counterpart to examples/sdk-minimal (issue #467).
//
// Each completed D1 boundary it receives the whole subscribed universe
// at once, picks the pair with the largest high-to-low range, and
// reports it as a described signal. It emits no intents: a scanner
// observes and ranks, it does not trade. Like every sdk consumer it
// imports only sdk and public value packages.
package main

import (
	"context"
	"log"
	"strconv"

	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/sdk"
)

func main() {
	if err := sdk.Serve(newScanner()); err != nil {
		log.Fatal(err)
	}
}

// scanner implements sdk.BarsConsumer: ConsumerBase plus OnBars, and no
// OnBar.
type scanner struct {
	universe []instrument.ID
	interval marketdata.Interval
}

var _ sdk.BarsConsumer = (*scanner)(nil)

func newScanner() *scanner {
	usd := num.MustParseCurrency("USD")
	var universe []instrument.ID
	for _, base := range []string{"EUR", "GBP", "AUD"} {
		universe = append(universe, instrument.CurrencyPairID(num.MustParseCurrency(base), usd))
	}
	interval, err := marketdata.NewInterval(marketdata.UnitDay, 1)
	if err != nil {
		// A fixed, valid literal; panic makes an impossible failure explicit.
		panic(err)
	}
	return &scanner{universe: universe, interval: interval}
}

func (s *scanner) Describe() sdk.Descriptor {
	reqs := make([]sdk.DataRequirement, len(s.universe))
	for i, id := range s.universe {
		reqs[i] = sdk.DataRequirement{Instrument: id, Interval: s.interval}
	}
	return sdk.Descriptor{Name: "sdk-scanner", Version: "0.1.0", Requirements: reqs}
}

func (s *scanner) Start(context.Context, sdk.Environment) error { return nil }

func (s *scanner) OnBars(_ context.Context, ev sdk.BarsEvent, _ sdk.View) ([]sdk.DescribedIntent, []sdk.DescribedSignal, error) {
	var best instrument.ID
	var bestRange float64
	found := false
	for _, b := range ev.Bars {
		// The analytical float64 view of exact prices is for ranking
		// only (ADR-045); nothing exact is derived from it.
		r := b.Bar.High.Float64() - b.Bar.Low.Float64()
		if !found || r > bestRange {
			best, bestRange, found = b.Instrument, r, true
		}
	}
	if !found {
		return nil, nil, nil
	}
	return nil, []sdk.DescribedSignal{{
		Strategy: "sdk-scanner",
		Values: map[string]string{
			"boundary": ev.Boundary.Format("2006-01-02"),
			"widest":   best.String(),
			"range":    strconv.FormatFloat(bestRange, 'f', 5, 64),
			"missing":  strconv.Itoa(len(ev.Missing)),
		},
	}}, nil
}
