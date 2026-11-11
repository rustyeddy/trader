package strategy

import (
	"context"
	"time"

	"github.com/rustyeddy/trader/instrument"
	runtimeorder "github.com/rustyeddy/trader/internal/order"
	"github.com/rustyeddy/trader/marketdata"
)

// BarsEvent is one coherent cross-sectional snapshot (issue #467): every
// bar a runtime has for one completed time boundary, for the
// requirements the receiving strategy declared. Each declared
// requirement appears in exactly one of Bars or Missing, and both
// follow Descriptor.Requirements order, so delivery is deterministic
// and completeness is never ambiguous.
type BarsEvent struct {
	// Boundary is the completed time boundary every bar shares.
	Boundary time.Time
	// Interval is the single interval every requirement declares.
	Interval marketdata.Interval
	// Bars are the completed bars available at Boundary.
	Bars []BarEvent
	// Missing are declared requirements with no completed bar at
	// Boundary.
	Missing []instrument.ID
}

// BarsHandler is an optional Strategy capability selecting snapshot
// delivery. A runtime that finds a Strategy implementing it calls
// OnBars once per completed boundary and never calls OnBar for that
// Strategy; the implementer's OnBar is then not part of its contract.
// Intent, failure, warm-up and View rules are identical to OnBar's.
// Requirements must all declare one Interval.
type BarsHandler interface {
	OnBars(ctx context.Context, event BarsEvent, view View) ([]runtimeorder.Intent, error)
}

// NewBarsEvent assembles a BarsEvent from the bars present at one
// boundary, in requirement order. present holds the boundary's events;
// anything in requirements not found there is reported in Missing.
func NewBarsEvent(boundary time.Time, requirements []DataRequirement, present []BarEvent) BarsEvent {
	byInstrument := make(map[instrument.ID]BarEvent, len(present))
	for _, ev := range present {
		byInstrument[ev.Instrument] = ev
	}
	out := BarsEvent{Boundary: boundary}
	for _, req := range requirements {
		out.Interval = req.Interval
		if ev, ok := byInstrument[req.Instrument]; ok {
			out.Bars = append(out.Bars, ev)
		} else {
			out.Missing = append(out.Missing, req.Instrument)
		}
	}
	return out
}
