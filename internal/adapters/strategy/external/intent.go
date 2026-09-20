package external

import (
	"fmt"

	"github.com/rustyeddy/trader/internal/id"
	runtimeorder "github.com/rustyeddy/trader/internal/order"
	"github.com/rustyeddy/trader/internal/strategy"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
	v1 "github.com/rustyeddy/trader/protocol/strategy/v1"
)

// IntentsFromWire converts a guest's described intents (one
// OnBarResponse.intents list) into canonical order.Intent values,
// built exclusively through factory — the run's own retained
// strategy.IntentFactory (ADR-005, ADR-062's own "Intent construction
// ownership" section). No order.Intent field is ever set by hand
// here: every IntentID/EventID/CorrelationID is minted by factory,
// the identical identity-generation path an in-process strategy
// already goes through.
//
// # Correlation-token grouping
//
// A DescribedIntent's correlation_token is an opaque, guest-chosen
// string with no identity meaning of its own beyond grouping (see
// v1.DescribedIntent's own doc comment). described is walked in
// order; the first time a non-empty token is seen, this mints one
// fresh id.CorrelationID via factory.NewCorrelationID() and records
// it in the returned map; every later intent (in this same call)
// sharing that token reuses the identical CorrelationID via
// factory.WithCorrelation. An empty token gets no map entry and
// builds through factory unmodified — matching IntentFactory's own
// documented default, "every call not explicitly grouped mints its
// own fresh CorrelationID."
//
// The returned map lets a caller (ExternalStrategyAdapter, #379)
// resolve a DescribedSignal.correlation_token to the same real
// CorrelationID via SignalFromWire, reproducing strategy/smatrend's
// own recordSignal behavior across the process boundary exactly
// (strategy.proto's own DescribedSignal doc comment).
func IntentsFromWire(described []*v1.DescribedIntent, factory strategy.IntentFactory) ([]runtimeorder.Intent, map[string]id.CorrelationID, error) {
	if factory == nil {
		return nil, nil, fmt.Errorf("external: intents from wire: factory must not be nil")
	}

	intents := make([]runtimeorder.Intent, 0, len(described))
	tokens := make(map[string]id.CorrelationID)

	for i, di := range described {
		if di == nil {
			return nil, nil, fmt.Errorf("%w: intent %d must be set", ErrInvalidWireValue, i)
		}

		f := factory
		if token := di.GetCorrelationToken(); token != "" {
			corr, ok := tokens[token]
			if !ok {
				var err error
				corr, err = factory.NewCorrelationID()
				if err != nil {
					return nil, nil, fmt.Errorf("external: intent %d: correlation token %q: %w", i, token, err)
				}
				tokens[token] = corr
			}
			f = factory.WithCorrelation(corr)
		}

		in, err := describedIntentToOrderIntent(f, di)
		if err != nil {
			return nil, nil, fmt.Errorf("external: intent %d: %w", i, err)
		}
		intents = append(intents, in)
	}

	return intents, tokens, nil
}

// describedIntentToOrderIntent builds one order.Intent from di via f,
// reading only the fields di's own Kind requires — mirroring
// order.NewIntent's own per-Kind field contract exactly (strategy.proto's
// own DescribedIntent doc comment: "issue #378's own job to enforce,
// not this schema's").
func describedIntentToOrderIntent(f strategy.IntentFactory, di *v1.DescribedIntent) (runtimeorder.Intent, error) {
	kind, err := fromWireIntentKind(di.GetKind())
	if err != nil {
		return runtimeorder.Intent{}, err
	}
	instID, err := parseInstrumentID(di.GetInstrumentId())
	if err != nil {
		return runtimeorder.Intent{}, err
	}

	// Every field this kind does not use must be at its own absent/
	// zero wire value (SIDE_UNSPECIFIED, an empty quantity/stop_price
	// string) — review finding: reading only the fields a kind
	// requires silently accepted and discarded a forbidden field
	// instead of rejecting it, unlike order.NewIntent's own per-Kind
	// contract (order/intent.go's own "Field requirements are per
	// Kind, not uniform" table), which this boundary must mirror
	// exactly rather than loosen.
	requireSide := kind == order.IntentEnter || kind == order.IntentTargetExposure || kind == order.IntentEnterWithStop
	requireQuantity := kind == order.IntentTargetExposure
	requireStopPrice := kind == order.IntentAdjustStop || kind == order.IntentEnterWithStop

	if !requireSide && di.GetSide() != v1.Side_SIDE_UNSPECIFIED {
		return runtimeorder.Intent{}, fmt.Errorf("%w: side must not be set for intent kind %v", ErrInvalidWireValue, kind)
	}
	if !requireQuantity && di.GetQuantity() != "" {
		return runtimeorder.Intent{}, fmt.Errorf("%w: quantity must not be set for intent kind %v", ErrInvalidWireValue, kind)
	}
	if !requireStopPrice && di.GetStopPrice() != "" {
		return runtimeorder.Intent{}, fmt.Errorf("%w: stop_price must not be set for intent kind %v", ErrInvalidWireValue, kind)
	}

	var (
		side      order.Side
		quantity  num.Quantity
		stopPrice num.Price
	)
	if requireSide {
		side, err = fromWireSide(di.GetSide())
		if err != nil {
			return runtimeorder.Intent{}, err
		}
	}
	if requireQuantity {
		quantity, err = parseQuantity("quantity", di.GetQuantity())
		if err != nil {
			return runtimeorder.Intent{}, err
		}
	}
	if requireStopPrice {
		stopPrice, err = parsePrice("stop_price", di.GetStopPrice())
		if err != nil {
			return runtimeorder.Intent{}, err
		}
	}

	switch kind {
	case order.IntentEnter:
		return f.Enter(instID, side)
	case order.IntentExit:
		return f.Exit(instID)
	case order.IntentAdjustStop:
		return f.AdjustStop(instID, stopPrice)
	case order.IntentTargetExposure:
		return f.TargetExposure(instID, side, quantity)
	case order.IntentEnterWithStop:
		return f.EnterWithStop(instID, side, stopPrice)
	default:
		// fromWireIntentKind only ever returns one of the five cases
		// above; this is unreachable but keeps the switch exhaustive
		// and explicit rather than silently falling through.
		return runtimeorder.Intent{}, fmt.Errorf("%w: intent kind %v", ErrInvalidWireValue, kind)
	}
}
