package strategy

import (
	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/marketdata"
)

// BarEvent is one instrument's completed bar, the trigger for OnBar.
type BarEvent struct {
	// Instrument is the canonical instrument identity this bar
	// belongs to.
	Instrument instrument.ID
	// Interval is Bar's own aggregation interval.
	Interval marketdata.Interval
	// Bar is the completed bar itself.
	Bar marketdata.Bar
}
