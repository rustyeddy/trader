package backtest

import (
	"context"
	"fmt"

	"github.com/rustyeddy/trader/account"
	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
	"github.com/rustyeddy/trader/pipeline"
	"github.com/rustyeddy/trader/strategy"
)

// resolverInputBuilder is the standard InputBuilder implementation
// (issue #216, M5-08 review): the mechanical translation from an
// emitted order.Intent to pipeline.Input is the same for every
// backtest run — resolve the intent's own instrument to a broker-side
// Listing, and fill in the run's own fixed RiskFraction/
// AdverseDistance policy — so every composition root would otherwise
// have to reimplement it. Build's ReferencePrice is event.Bar.Open —
// per #214's next-bar-open fill-eligibility rule, event is the bar
// that made intent eligible, and Open is the honest reference price
// at that instant. This is a reference/sizing input to the M4
// pipeline only: it does not guarantee the simulator's eventual fill
// price, which the broker's own slippage/fill model still owns.
type resolverInputBuilder struct {
	resolver        instrument.Resolver
	riskFraction    num.Rate
	adverseDistance num.Price
}

// NewResolverInputBuilder returns the standard InputBuilder: it
// resolves each intent's own instrument to a Listing via resolver, and
// applies one fixed riskFraction/adverseDistance policy to every
// sized intent, matching RunnerParams' own "one fixed policy per run"
// scope.
func NewResolverInputBuilder(resolver instrument.Resolver, riskFraction num.Rate, adverseDistance num.Price) InputBuilder {
	return resolverInputBuilder{resolver: resolver, riskFraction: riskFraction, adverseDistance: adverseDistance}
}

func (b resolverInputBuilder) Build(ctx context.Context, intent order.Intent, event strategy.BarEvent, snapshot account.Snapshot) (pipeline.Input, error) {
	listing, err := b.resolver.ResolveInstrument(intent.Instrument, snapshot.Broker(), "")
	if err != nil {
		return pipeline.Input{}, fmt.Errorf("backtest: resolver input builder: resolving %s on %s: %w", intent.Instrument, snapshot.Broker(), err)
	}

	adverse := b.adverseDistance
	ref := event.Bar.Open
	return pipeline.Input{
		Intent:          intent,
		Listing:         listing,
		Account:         snapshot,
		RiskFraction:    b.riskFraction,
		AdverseDistance: &adverse,
		ReferencePrice:  &ref,
	}, nil
}

var _ InputBuilder = resolverInputBuilder{}
