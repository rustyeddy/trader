package strategy

import (
	"github.com/rustyeddy/trader/clock"
	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
)

// IntentFactory is the narrow, runtime-injected capability a Strategy
// uses to build canonical order.Intent values (issue #210's own
// review): it owns deterministic ID generation
// (IntentID/EventID/CorrelationID) and calls order.NewIntent on the
// strategy's behalf, so every Intent it returns is valid from the
// instant it exists (order.NewIntent's own contract) without any
// strategy needing direct access to an *id.Generator.
//
// The strategy still owns trading semantics: whether several intents
// it emits from one OnBar call belong to one correlation group is the
// strategy's own decision, expressed via NewCorrelationID/
// WithCorrelation, not something IntentFactory decides on its behalf.
// Every call not explicitly grouped via WithCorrelation mints its own
// fresh CorrelationID — an independent intent, not implicitly
// correlated with any other.
type IntentFactory interface {
	// Enter builds an order.IntentEnter for instID/side.
	Enter(instID instrument.ID, side order.Side) (order.Intent, error)
	// Exit builds an order.IntentExit for instID.
	Exit(instID instrument.ID) (order.Intent, error)
	// AdjustStop builds an order.IntentAdjustStop moving instID's
	// protective stop to stopPrice.
	AdjustStop(instID instrument.ID, stopPrice num.Price) (order.Intent, error)
	// TargetExposure builds an order.IntentTargetExposure for instID,
	// side, and quantity.
	TargetExposure(instID instrument.ID, side order.Side, quantity num.Quantity) (order.Intent, error)

	// NewCorrelationID mints a fresh, deterministic id.CorrelationID a
	// caller can pass to WithCorrelation to group multiple intents —
	// for example a reversal expressed as an exit intent and an enter
	// intent that should be recognized as one causally-related move.
	NewCorrelationID() (id.CorrelationID, error)
	// WithCorrelation returns an IntentFactory that builds every
	// intent under corr instead of minting a fresh CorrelationID per
	// call. The returned factory shares this one's clock, ID
	// generator, and Source; only its correlation behavior differs.
	WithCorrelation(corr id.CorrelationID) IntentFactory
}

// NewIntentFactory returns an IntentFactory that generates identifiers
// from ids and timestamps from c, attributing every built Intent's
// Metadata.Source to source (conventionally the owning strategy's own
// Descriptor.Name — see the package doc comment).
func NewIntentFactory(c clock.Clock, ids *id.Generator, source id.Source) IntentFactory {
	return &intentFactory{clock: c, ids: ids, source: source}
}

type intentFactory struct {
	clock  clock.Clock
	ids    *id.Generator
	source id.Source
	corr   *id.CorrelationID // nil: mint a fresh CorrelationID per call
}

func (f *intentFactory) NewCorrelationID() (id.CorrelationID, error) {
	return id.GenerateCorrelationID(f.ids)
}

func (f *intentFactory) WithCorrelation(corr id.CorrelationID) IntentFactory {
	return &intentFactory{clock: f.clock, ids: f.ids, source: f.source, corr: &corr}
}

func (f *intentFactory) Enter(instID instrument.ID, side order.Side) (order.Intent, error) {
	return f.build(order.IntentEnter, instID, side, nil, nil)
}

func (f *intentFactory) Exit(instID instrument.ID) (order.Intent, error) {
	return f.build(order.IntentExit, instID, 0, nil, nil)
}

func (f *intentFactory) AdjustStop(instID instrument.ID, stopPrice num.Price) (order.Intent, error) {
	return f.build(order.IntentAdjustStop, instID, 0, nil, &stopPrice)
}

func (f *intentFactory) TargetExposure(instID instrument.ID, side order.Side, quantity num.Quantity) (order.Intent, error) {
	return f.build(order.IntentTargetExposure, instID, side, &quantity, nil)
}

// build assembles and validates one Intent, resolving this factory's
// correlation policy (a shared corr if WithCorrelation was used,
// otherwise a fresh one per call) before delegating to order.NewIntent
// for full field validation.
func (f *intentFactory) build(kind order.IntentKind, instID instrument.ID, side order.Side, quantity *num.Quantity, stopPrice *num.Price) (order.Intent, error) {
	intentID, err := id.GenerateIntentID(f.ids)
	if err != nil {
		return order.Intent{}, err
	}
	eventID, err := id.GenerateEventID(f.ids)
	if err != nil {
		return order.Intent{}, err
	}

	corrID, err := f.correlationID()
	if err != nil {
		return order.Intent{}, err
	}

	return order.NewIntent(order.Intent{
		IntentID:   intentID,
		Kind:       kind,
		Instrument: instID,
		Side:       side,
		Quantity:   quantity,
		StopPrice:  stopPrice,
		Metadata: id.Metadata{
			EventID:       eventID,
			CorrelationID: corrID,
			Timestamp:     f.clock.Now(),
			Source:        f.source,
		},
	})
}

func (f *intentFactory) correlationID() (id.CorrelationID, error) {
	if f.corr != nil {
		return *f.corr, nil
	}
	return id.GenerateCorrelationID(f.ids)
}
