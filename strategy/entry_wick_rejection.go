package strategy

import (
	"time"

	"github.com/rustyeddy/trader/candlepattern"
	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/types"
)

// WickRejectionEntry adapts candlepattern.WickRejection to the EntryTrigger
// interface, maintaining the caller-visible lookback window
// WickRejection.Update expects — mirrors how D1ADXFilter (regime_d1adx.go)
// wraps indicator.ADX.
type WickRejectionEntry struct {
	detector *candlepattern.WickRejection
	lookback int
	window   []market.Candle
}

// NewWickRejectionEntry constructs a WickRejectionEntry wrapping a
// candlepattern.WickRejection built from the same tunables (see
// candlepattern.WickRejection's doc comment for their meaning). lookback
// validation (>= 1) is owned entirely by candlepattern.NewWickRejection;
// by the time it returns successfully, lookback is already known-valid.
func NewWickRejectionEntry(minWickRatio, maxClosePos, minWickATR float64, lookback, atrPeriod int, scale types.Scale6) (*WickRejectionEntry, error) {
	det, err := candlepattern.NewWickRejection(minWickRatio, maxClosePos, minWickATR, lookback, atrPeriod, scale)
	if err != nil {
		return nil, err
	}
	return &WickRejectionEntry{detector: det, lookback: lookback}, nil
}

func (e *WickRejectionEntry) Name() string {
	return "rejection-candle(" + e.detector.Name() + ")"
}

func (e *WickRejectionEntry) Ready() bool { return e.detector.Ready() }

// Tick appends c to the lookback window, trims it to len<=lookback (most-
// recent-last), and feeds it to the detector.
func (e *WickRejectionEntry) Tick(c market.Candle) {
	e.window = append(e.window, c)
	if len(e.window) > e.lookback {
		e.window = e.window[len(e.window)-e.lookback:]
	}
	e.detector.Update(e.window)
}

// Triggered ignores episodeStart/c — the detector's state, advanced via
// Tick, already reflects "as of the most recent bar", which callers are
// expected to call Tick for immediately before checking Triggered (see
// EntryTrigger.Tick's doc comment: called every bar).
func (e *WickRejectionEntry) Triggered(side types.Side, _ time.Time, _ market.Candle) bool {
	return e.detector.Ready() && e.detector.Matched() && e.detector.Side() == side
}

// Reset clears the lookback window (both this adapter's own copy and the
// detector's internal window/matched state, via Update(nil) — see its doc
// comment) so a new episode starts pattern evaluation fresh, without any
// candles from the prior episode leaking into the aggregate. ATR is
// deliberately left running: it's a continuous volatility read, not
// per-episode state, so resetting it would force an unnecessary re-warmup
// every time an episode boundary is crossed.
func (e *WickRejectionEntry) Reset() {
	e.window = nil
	e.detector.Update(nil)
}
