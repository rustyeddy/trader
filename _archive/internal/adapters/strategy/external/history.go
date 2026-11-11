package external

import (
	"fmt"

	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/marketdata"
	v1 "github.com/rustyeddy/trader/protocol/strategy/v1"
)

// HistoryBarsQuery is the parsed form of a received
// *v1.GetHistoryBarsRequest — everything ExternalStrategyAdapter
// (#379) needs to validate against the active session/in-flight
// callback and to call strategy.History.HistoryBars.
type HistoryBarsQuery struct {
	SessionID        string
	CallbackSequence uint64
	Instrument       instrument.ID
	Interval         marketdata.Interval
	Count            int
}

// FromWireHistoryBarsRequest parses a received
// *v1.GetHistoryBarsRequest. It validates every field's own shape
// (non-empty session_id, a well-formed instrument_id/interval, a
// non-negative count) but does not itself check session/callback
// liveness or requirement membership — those require adapter-held
// session state this package does not have, and are
// ExternalStrategyAdapter's own job (ERROR_CODE_UNKNOWN_SESSION/
// ERROR_CODE_UNKNOWN_CALLBACK/ERROR_CODE_REQUIREMENT_NOT_DECLARED,
// per strategy.proto's own GetHistoryBarsRequest doc comment).
func FromWireHistoryBarsRequest(w *v1.GetHistoryBarsRequest) (HistoryBarsQuery, error) {
	if w == nil {
		return HistoryBarsQuery{}, fmt.Errorf("%w: history bars request must be set", ErrInvalidWireValue)
	}
	if w.GetSessionId() == "" {
		return HistoryBarsQuery{}, fmt.Errorf("%w: session_id must not be empty", ErrInvalidWireValue)
	}
	instID, err := parseInstrumentID(w.GetInstrumentId())
	if err != nil {
		return HistoryBarsQuery{}, err
	}
	interval, err := FromWireInterval(w.GetInterval())
	if err != nil {
		return HistoryBarsQuery{}, err
	}
	if w.GetCount() < 0 {
		return HistoryBarsQuery{}, fmt.Errorf("%w: count must not be negative", ErrInvalidWireValue)
	}
	return HistoryBarsQuery{
		SessionID:        w.GetSessionId(),
		CallbackSequence: w.GetCallbackSequence(),
		Instrument:       instID,
		Interval:         interval,
		Count:            int(w.GetCount()),
	}, nil
}

// ToWireHistoryBarsResponse builds the *v1.GetHistoryBarsResponse for
// bars, which must already be oldest-first — the same ordering
// strategy.History.HistoryBars itself returns, and the ordering
// GetHistoryBarsResponse.bars documents as part of the wire contract.
func ToWireHistoryBarsResponse(bars []marketdata.Bar) *v1.GetHistoryBarsResponse {
	return &v1.GetHistoryBarsResponse{Bars: ToWireBars(bars)}
}
