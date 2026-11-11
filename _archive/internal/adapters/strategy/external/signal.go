package external

import (
	"fmt"

	"github.com/rustyeddy/trader/internal/id"
	"github.com/rustyeddy/trader/internal/journal"
	v1 "github.com/rustyeddy/trader/protocol/strategy/v1"
)

// SignalFromWire converts a received *v1.DescribedSignal into a
// journal.Signal, plus the real id.CorrelationID it should be
// journaled under. journal.Signal mirrors DescribedSignal's own
// Strategy/Values fields verbatim (issue #384's own deterministic-
// equivalence requirement) — no translation is needed for the
// evidence bag itself.
//
// The returned CorrelationID reproduces strategy/smatrend's own
// recordSignal behavior across the process boundary
// (strategy.proto's own DescribedSignal doc comment): tokens is the
// map IntentsFromWire returned for this same OnBarResponse. When
// w's correlation_token matches an entry in tokens, that group's own
// real, already-minted CorrelationID is returned; an empty token, or
// one matching no intent from this callback, returns the zero
// CorrelationID — smatrend's own "no intents this bar" case.
func SignalFromWire(w *v1.DescribedSignal, tokens map[string]id.CorrelationID) (journal.Signal, id.CorrelationID, error) {
	if w == nil {
		return journal.Signal{}, id.CorrelationID{}, fmt.Errorf("%w: signal must be set", ErrInvalidWireValue)
	}
	// journal.NewRecord rejects an empty Signal.Strategy; reject it
	// here rather than returning an already-invalid journal.Signal a
	// caller only discovers is broken once it tries to journal it
	// (review finding).
	if w.GetStrategy() == "" {
		return journal.Signal{}, id.CorrelationID{}, fmt.Errorf("%w: signal strategy must not be empty", ErrInvalidWireValue)
	}

	values := w.GetValues()
	var copied map[string]string
	if len(values) > 0 {
		copied = make(map[string]string, len(values))
		for k, v := range values {
			copied[k] = v
		}
	}

	sig := journal.Signal{
		Strategy: w.GetStrategy(),
		Values:   copied,
	}

	corr := tokens[w.GetCorrelationToken()] // zero value on no match, matching the map's own convention

	return sig, corr, nil
}
