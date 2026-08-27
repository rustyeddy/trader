package execution

import (
	"fmt"
	"strings"

	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
)

// intentFlags holds evaluate/submit's own shared intent-construction
// flags: an order.IntentEnter's Side, plus the risk/sizing assumptions
// pipeline.Input needs (RiskFraction, AdverseDistance, and the
// optional ReferencePrice a value-based risk.Rule might consult).
//
// Only order.IntentEnter is supported for v0: it is the one intent
// kind that exercises risk.Sizer, making it the most representative
// path through the full sizing -> planning -> risk -> request
// sequence this issue asks the CLI to demonstrate (#187's own "a
// representative intent" scope). IntentExit/IntentTargetExposure are
// additive future work once a real consumer needs them from this CLI,
// not speculated into v0.
type intentFlags struct {
	side            string
	riskFraction    string
	adverseDistance string
	referencePrice  string
}

// parseIntentSide parses --side into order.Side, case-insensitively —
// the same convention cmd/trader/broker's own parseOrderSide
// establishes, duplicated here rather than shared across
// command-family packages (issue #201).
func parseIntentSide(s string) (order.Side, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "buy":
		return order.Buy, nil
	case "sell":
		return order.Sell, nil
	default:
		return 0, fmt.Errorf("invalid --side %q: expected buy or sell", s)
	}
}

// buildEnterIntent constructs an order.IntentEnter for instID/side,
// with fresh IntentID/EventID/CorrelationID from gen — a leaf-level
// generator distinct from buildSimBroker's own internal one, the same
// separation cmd/trader/broker's own submit command establishes
// between the two identifier domains.
func buildEnterIntent(gen *id.Generator, instID instrument.ID, side order.Side) (order.Intent, error) {
	intentID, err := id.GenerateIntentID(gen)
	if err != nil {
		return order.Intent{}, err
	}
	eventID, err := id.GenerateEventID(gen)
	if err != nil {
		return order.Intent{}, err
	}
	corrID, err := id.GenerateCorrelationID(gen)
	if err != nil {
		return order.Intent{}, err
	}
	return order.NewIntent(order.Intent{
		IntentID:   intentID,
		Kind:       order.IntentEnter,
		Instrument: instID,
		Side:       side,
		Metadata:   id.Metadata{EventID: eventID, CorrelationID: corrID},
	})
}

// buildSizingParams parses flags' RiskFraction/AdverseDistance/
// ReferencePrice into pipeline.Input's own typed fields.
// ReferencePrice is optional — a zero-rule risk.Engine (see service.go's
// buildService) never needs one, but a caller supplying --reference-price
// still has it threaded through for parity with the full pipeline.Input
// contract.
func buildSizingParams(flags intentFlags) (riskFraction num.Rate, adverseDistance *num.Price, referencePrice *num.Price, err error) {
	riskFraction, err = num.ParseRate(flags.riskFraction)
	if err != nil {
		return num.Rate{}, nil, nil, fmt.Errorf("invalid --risk-fraction: %w", err)
	}
	adverse, err := num.ParsePrice(flags.adverseDistance)
	if err != nil {
		return num.Rate{}, nil, nil, fmt.Errorf("invalid --adverse-distance: %w", err)
	}
	adverseDistance = &adverse

	if flags.referencePrice != "" {
		ref, err := num.ParsePrice(flags.referencePrice)
		if err != nil {
			return num.Rate{}, nil, nil, fmt.Errorf("invalid --reference-price: %w", err)
		}
		referencePrice = &ref
	}

	return riskFraction, adverseDistance, referencePrice, nil
}
