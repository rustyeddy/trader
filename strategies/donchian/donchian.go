// Package donchianv6 is Donchian breakout v6: adds a Monday/week-open entry
// block on top of the v5 news-day filter.
//
// When block_monday is true (default), no new positions are opened on Mondays
// (UTC). This avoids entering on the week-open bar where gaps from the weekend
// close can produce false breakout signals. The pending streak is preserved so
// that a breakout setup that starts Friday and would have confirmed Monday fires
// on Tuesday instead.
//
// Day-of-week derivation uses the unix epoch (1970-01-01 = Thursday):
//   - unixDay % 7 == 4  →  Monday
//   - unixDay % 7 == 1  →  Friday (available for future use via block_friday)
//
// Registers under "donchian-v6" and "donchian-breakout-v6".
package donchianv6

import (
	"context"
	"fmt"
	"math"

	"github.com/rustyeddy/trader/execution"
	"github.com/rustyeddy/trader/indicator"
	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/strategy"
	"github.com/rustyeddy/trader/types"
)

func init() {
	strategy.MustRegisterStrategy(build, "donchian", "donchian-breakout")
}

const (
	dowMonday = int64(4) // (unixDay % 7) value for Monday
	dowFriday = int64(1) // (unixDay % 7) value for Friday
)

// Breakout is the v6 Donchian strategy.
type Breakout struct {
	period        int
	closeStrength int32 // ×1000; e.g. 0.6 → 600
	confirmBars   int
	adxThreshold  types.Units // ×UnitsScale (==ValueScale); e.g. 25.0 → 25_000_000
	blockMonday   bool
	blockFriday   bool

	// Rolling channel buffer (completed bars only).
	highs []types.Price
	lows  []types.Price
	pos   int
	count int

	// Consecutive-close confirmation state (from v2).
	pendingSide  types.Side
	pendingCount int
	pendingLevel types.Price

	// ADX directional-strength indicator (from v4).
	adx *indicator.ADX

	// News-day block: set of unix day numbers (from v5).
	blockedDays map[int64]bool

	name string
}

// Config holds constructor parameters.
type Config struct {
	Period        int
	CloseStrength float64
	ConfirmBars   int
	ADXPeriod     int
	ADXThreshold  float64
	BlockedDays   map[int64]bool
	BlockMonday   bool
	BlockFriday   bool
}

func New(cfg Config) (*Breakout, error) {
	if cfg.Period <= 1 {
		return nil, fmt.Errorf("donchianv6: period must be > 1")
	}
	if cfg.CloseStrength < 0.5 || cfg.CloseStrength > 1.0 {
		return nil, fmt.Errorf("donchianv6: close_strength must be in [0.5, 1.0]")
	}
	cb := cfg.ConfirmBars
	if cb < 1 {
		cb = 2
	}
	ap := cfg.ADXPeriod
	if ap <= 0 {
		ap = 14
	}
	at := cfg.ADXThreshold
	if at <= 0 {
		at = 25.0
	}
	bd := cfg.BlockedDays
	if bd == nil {
		bd = map[int64]bool{}
	}
	adx, err := indicator.NewADX(ap, types.PriceScale)
	if err != nil {
		return nil, err
	}
	return &Breakout{
		period:        cfg.Period,
		closeStrength: int32(math.Round(cfg.CloseStrength * 1000)),
		confirmBars:   cb,
		adxThreshold:  types.Units(math.Round(at * float64(indicator.ValueScale))),
		blockMonday:   cfg.BlockMonday,
		blockFriday:   cfg.BlockFriday,
		highs:         make([]types.Price, cfg.Period),
		lows:          make([]types.Price, cfg.Period),
		adx:           adx,
		blockedDays:   bd,
		name: fmt.Sprintf("DONCHIAN-V6(%d,cs=%.2f,cb=%d,adx=%d/%.1f,nd=%d,mon=%v,fri=%v)",
			cfg.Period, cfg.CloseStrength, cb, ap, at, len(bd),
			cfg.BlockMonday, cfg.BlockFriday),
	}, nil
}

func (d *Breakout) Name() string            { return d.name }
func (d *Breakout) StopDescription() string { return "" }

func (d *Breakout) Reset() {
	for i := range d.highs {
		d.highs[i] = 0
		d.lows[i] = 0
	}
	d.pos = 0
	d.count = 0
	d.pendingSide = 0
	d.pendingCount = 0
	d.pendingLevel = 0
	d.adx.Reset()
}

func (d *Breakout) Ready() bool { return d.count >= d.period }

func (d *Breakout) channelHighLow() (types.Price, types.Price) {
	hi := d.highs[0]
	lo := d.lows[0]
	for i := 1; i < d.period; i++ {
		if d.highs[i] > hi {
			hi = d.highs[i]
		}
		if d.lows[i] < lo {
			lo = d.lows[i]
		}
	}
	return hi, lo
}

func (d *Breakout) advanceBar(c market.Candle) {
	d.highs[d.pos] = c.High
	d.lows[d.pos] = c.Low
	d.pos = (d.pos + 1) % d.period
	if d.count < d.period {
		d.count++
	}
	d.adx.Update(c)
}

// closeStrengthOK checks that the bar closed in the strong portion of its range.
// threshold is ×1000 (e.g. 600 means 60%). Uses integer arithmetic on Price units.
func closeStrengthOK(c market.Candle, side types.Side, threshold int32) bool {
	rng := int64(c.High - c.Low)
	if rng <= 0 {
		return false
	}
	if side == types.Long {
		return int64(c.Close-c.Low)*1000 >= int64(threshold)*rng
	}
	return int64(c.High-c.Close)*1000 >= int64(threshold)*rng
}

func (d *Breakout) adxGatePass(side types.Side) bool {
	if !d.adx.Ready() {
		return true
	}
	if d.adx.ValueUnits() < d.adxThreshold {
		return false
	}
	if side == types.Long {
		return d.adx.PlusDIUnits() > d.adx.MinusDIUnits()
	}
	return d.adx.MinusDIUnits() > d.adx.PlusDIUnits()
}

func (d *Breakout) Update(_ context.Context, ct *market.CandleTime, run strategy.StrategyContext) strategy.Signal {
	if ct == nil {
		return strategy.Hold("no candle")
	}

	if !d.Ready() {
		d.advanceBar(ct.Candle)
		return strategy.Hold("warming up")
	}

	currentDay := int64(ct.Timestamp) / 86400
	dow := currentDay % 7

	// Monday block.
	if d.blockMonday && dow == dowMonday {
		d.advanceBar(ct.Candle)
		return strategy.Hold("monday-block")
	}

	// Friday block (optional).
	if d.blockFriday && dow == dowFriday {
		d.advanceBar(ct.Candle)
		return strategy.Hold("friday-block")
	}

	// News-day block.
	if d.blockedDays[currentDay] {
		d.advanceBar(ct.Candle)
		return strategy.Hold("news-day-block")
	}

	hi, lo := d.channelHighLow()

	var side types.Side
	if d.pendingCount > 0 {
		switch d.pendingSide {
		case types.Long:
			if ct.Close > d.pendingLevel {
				side = types.Long
			}
		case types.Short:
			if ct.Close < d.pendingLevel {
				side = types.Short
			}
		}
		if side == 0 {
			switch {
			case ct.Close > hi:
				side = types.Long
			case ct.Close < lo:
				side = types.Short
			}
		}
	} else {
		switch {
		case ct.Close > hi:
			side = types.Long
		case ct.Close < lo:
			side = types.Short
		}
	}

	if side == 0 {
		d.pendingSide = 0
		d.pendingCount = 0
		d.pendingLevel = 0
		d.advanceBar(ct.Candle)
		return strategy.Hold("no breakout")
	}

	if side != d.pendingSide {
		if !closeStrengthOK(ct.Candle, side, d.closeStrength) {
			d.pendingSide = 0
			d.pendingCount = 0
			d.pendingLevel = 0
			d.advanceBar(ct.Candle)
			return strategy.Hold("weak close")
		}
		d.pendingSide = side
		d.pendingCount = 1
		if side == types.Long {
			d.pendingLevel = hi
		} else {
			d.pendingLevel = lo
		}
	} else {
		d.pendingCount++
	}

	if d.pendingCount < d.confirmBars {
		d.advanceBar(ct.Candle)
		return strategy.Hold(fmt.Sprintf("confirming break (%d/%d)", d.pendingCount, d.confirmBars))
	}

	if !d.adxGatePass(side) {
		d.advanceBar(ct.Candle)
		return strategy.Hold(fmt.Sprintf("adx-filtered(adx=%.1f,+DI=%.1f,-DI=%.1f)",
			d.adx.Float64(), d.adx.PlusDI(), d.adx.MinusDI()))
	}

	d.pendingSide = 0
	d.pendingCount = 0
	d.pendingLevel = 0

	// Check if already in the same side — planner handles reversal-closes.
	alreadySameSide := false
	if run != nil {
		_ = run.OpenLots().Range(func(lot *execution.Lot) error {
			if lot.State == execution.LotOpen && lot.Side == side {
				alreadySameSide = true
			}
			return nil
		})
	}

	d.advanceBar(ct.Candle)

	if alreadySameSide {
		return strategy.Hold("already-in-position")
	}

	reason := "donchian-v6-breakout-up"
	if side == types.Short {
		reason = "donchian-v6-breakout-down"
	}
	return strategy.Signal{Side: side, Reason: reason}
}

func build(params map[string]any) (strategy.Strategy, error) {
	period, _, err := strategy.GetInt32Param(params, "period")
	if err != nil {
		return nil, err
	}
	if period <= 1 {
		period = 20
	}
	closeStrength, ok, err := strategy.GetFloat64Param(params, "close_strength")
	if err != nil {
		return nil, err
	}
	if !ok {
		closeStrength = 0.6
	}
	confirmBars, ok, err := strategy.GetInt32Param(params, "confirm_bars")
	if err != nil {
		return nil, err
	}
	if !ok {
		confirmBars = 2
	}
	adxPeriod, ok, err := strategy.GetInt32Param(params, "adx_period")
	if err != nil {
		return nil, err
	}
	if !ok {
		adxPeriod = 14
	}
	adxThreshold, ok, err := strategy.GetFloat64Param(params, "adx_threshold")
	if err != nil {
		return nil, err
	}
	if !ok {
		adxThreshold = 25.0
	}
	blockMonday, ok, err := strategy.GetBoolParam(params, "block_monday")
	if err != nil {
		return nil, err
	}
	if !ok {
		blockMonday = true // default: block Monday entries
	}
	blockFriday, _, err := strategy.GetBoolParam(params, "block_friday")
	if err != nil {
		return nil, err
	}

	var blockedDays map[int64]bool
	newsDaysFile, ok, err := strategy.GetStringParam(params, "news_days_file")
	if err != nil {
		return nil, err
	}
	if ok && newsDaysFile != "" {
		blockedDays, err = strategy.LoadNewsDays(newsDaysFile)
		if err != nil {
			return nil, err
		}
	}

	return New(Config{
		Period:        int(period),
		CloseStrength: closeStrength,
		ConfirmBars:   int(confirmBars),
		ADXPeriod:     int(adxPeriod),
		ADXThreshold:  adxThreshold,
		BlockedDays:   blockedDays,
		BlockMonday:   blockMonday,
		BlockFriday:   blockFriday,
	})
}
