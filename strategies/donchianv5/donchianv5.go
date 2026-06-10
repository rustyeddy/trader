// Package donchianv5 is Donchian breakout v5: adds a high-impact news-day
// filter on top of the v4 ADX directional-strength gate.
//
// On days listed in the news_days_file (one UTC date per line, format
// YYYY-MM-DD, lines starting with '#' ignored), no new positions are opened.
// The pending streak is preserved across a blocked day so that a confirmed
// breakout can fire on the next tradeable bar without requiring a fresh setup.
//
// Registers under "donchian-v5" and "donchian-breakout-v5".
package donchianv5

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/rustyeddy/trader"
)

func init() {
	trader.RegisterStrategy(build, "donchian-v5", "donchian-breakout-v5")
}

// Breakout is the v5 Donchian strategy.
type Breakout struct {
	period        int
	closeStrength float64
	confirmBars   int
	adxThreshold  float64

	// Rolling channel buffer (completed bars only).
	highs []trader.Price
	lows  []trader.Price
	pos   int
	count int

	// Consecutive-close confirmation state (from v2).
	pendingSide  trader.Side
	pendingCount int
	pendingLevel trader.Price

	// ADX directional-strength indicator (from v4).
	adx *trader.ADX

	// News-day block: set of unix day numbers (unix_sec/86400) to skip.
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
	BlockedDays   map[int64]bool // pre-parsed set of days to block
}

func New(cfg Config) (*Breakout, error) {
	if cfg.Period <= 1 {
		return nil, fmt.Errorf("donchianv5: period must be > 1")
	}
	if cfg.CloseStrength < 0.5 || cfg.CloseStrength > 1.0 {
		return nil, fmt.Errorf("donchianv5: close_strength must be in [0.5, 1.0]")
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
	adx, err := trader.NewADX(ap, trader.PriceScale)
	if err != nil {
		return nil, err
	}
	return &Breakout{
		period:        cfg.Period,
		closeStrength: cfg.CloseStrength,
		confirmBars:   cb,
		adxThreshold:  at,
		highs:         make([]trader.Price, cfg.Period),
		lows:          make([]trader.Price, cfg.Period),
		adx:           adx,
		blockedDays:   bd,
		name: fmt.Sprintf("DONCHIAN-V5(%d,cs=%.2f,cb=%d,adx=%d/%.1f,nd=%d)",
			cfg.Period, cfg.CloseStrength, cb, ap, at, len(bd)),
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

func (d *Breakout) channelHighLow() (trader.Price, trader.Price) {
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

func (d *Breakout) advanceBar(c trader.Candle) {
	d.highs[d.pos] = c.High
	d.lows[d.pos] = c.Low
	d.pos = (d.pos + 1) % d.period
	if d.count < d.period {
		d.count++
	}
	d.adx.Update(c)
}

func closeStrengthOK(c trader.Candle, side trader.Side, threshold float64) bool {
	rng := float64(c.High - c.Low)
	if rng <= 0 {
		return false
	}
	if side == trader.Long {
		return float64(c.Close-c.Low)/rng >= threshold
	}
	return float64(c.High-c.Close)/rng >= threshold
}

func (d *Breakout) adxGatePass(side trader.Side) bool {
	if !d.adx.Ready() {
		return true
	}
	if d.adx.Float64() < d.adxThreshold {
		return false
	}
	if side == trader.Long {
		return d.adx.PlusDI() > d.adx.MinusDI()
	}
	return d.adx.MinusDI() > d.adx.PlusDI()
}

func (d *Breakout) Update(ctx context.Context, ct *trader.CandleTime, run *trader.Backtest) *trader.StrategyPlan {
	_ = ctx
	if ct == nil {
		return &trader.DefaultStrategyPlan
	}

	if !d.Ready() {
		d.advanceBar(ct.Candle)
		return &trader.StrategyPlan{Reason: "warming up"}
	}

	// News-day block: skip entry but preserve pending streak.
	currentDay := int64(ct.Timestamp) / 86400
	if d.blockedDays[currentDay] {
		d.advanceBar(ct.Candle)
		return &trader.StrategyPlan{Reason: "news-day-block"}
	}

	hi, lo := d.channelHighLow()

	var side trader.Side
	if d.pendingCount > 0 {
		switch d.pendingSide {
		case trader.Long:
			if ct.Close > d.pendingLevel {
				side = trader.Long
			}
		case trader.Short:
			if ct.Close < d.pendingLevel {
				side = trader.Short
			}
		}
		if side == 0 {
			switch {
			case ct.Close > hi:
				side = trader.Long
			case ct.Close < lo:
				side = trader.Short
			}
		}
	} else {
		switch {
		case ct.Close > hi:
			side = trader.Long
		case ct.Close < lo:
			side = trader.Short
		}
	}

	if side == 0 {
		d.pendingSide = 0
		d.pendingCount = 0
		d.pendingLevel = 0
		d.advanceBar(ct.Candle)
		return &trader.StrategyPlan{Reason: "no breakout"}
	}

	if side != d.pendingSide {
		if !closeStrengthOK(ct.Candle, side, d.closeStrength) {
			d.pendingSide = 0
			d.pendingCount = 0
			d.pendingLevel = 0
			d.advanceBar(ct.Candle)
			return &trader.StrategyPlan{Reason: "weak close"}
		}
		d.pendingSide = side
		d.pendingCount = 1
		if side == trader.Long {
			d.pendingLevel = hi
		} else {
			d.pendingLevel = lo
		}
	} else {
		d.pendingCount++
	}

	if d.pendingCount < d.confirmBars {
		d.advanceBar(ct.Candle)
		return &trader.StrategyPlan{
			Reason: fmt.Sprintf("confirming break (%d/%d)", d.pendingCount, d.confirmBars),
		}
	}

	if !d.adxGatePass(side) {
		d.advanceBar(ct.Candle)
		return &trader.StrategyPlan{
			Reason: fmt.Sprintf("adx-filtered(adx=%.1f,+DI=%.1f,-DI=%.1f)",
				d.adx.Float64(), d.adx.PlusDI(), d.adx.MinusDI()),
		}
	}

	d.pendingSide = 0
	d.pendingCount = 0
	d.pendingLevel = 0

	plan := emitOpen(ct, run, side)
	d.advanceBar(ct.Candle)
	if side == trader.Long {
		plan.Reason = "donchian-v5-breakout-up"
	} else {
		plan.Reason = "donchian-v5-breakout-down"
	}
	return plan
}

func emitOpen(ct *trader.CandleTime, run *trader.Backtest, side trader.Side) *trader.StrategyPlan {
	plan := &trader.StrategyPlan{}

	alreadyOpen := false
	if run != nil && run.State != nil && run.State.Lots != nil {
		_ = run.State.Lots.Range(func(lot *trader.Lot) error {
			if lot.State != trader.LotOpen {
				return nil
			}
			if lot.Side == side {
				alreadyOpen = true
				return nil
			}
			plan.Closes = append(plan.Closes, &trader.CloseRequest{
				Request: trader.Request{
					TradeCommon: lot.TradeCommon,
					Reason:      "donchian-v5-reverse",
					Candle:      ct.Candle,
					RequestType: trader.RequestClose,
					Price:       ct.Close,
					Timestamp:   ct.Timestamp,
				},
				Lot:        lot,
				CloseCause: trader.CloseManual,
			})
			return nil
		})
	}

	if alreadyOpen {
		return plan
	}

	inst := ""
	if run != nil && run.Request != nil {
		inst = run.Request.Instrument
	}
	open := trader.NewOpenRequest(inst, ct, side, 0, 0, "donchian-v5-breakout")
	plan.Opens = append(plan.Opens, open)
	return plan
}

// LoadNewsDays parses a text file of news dates (one YYYY-MM-DD per line,
// lines starting with '#' ignored) and returns a set of unix day numbers.
func LoadNewsDays(path string) (map[int64]bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("news_days_file: %w", err)
	}
	defer f.Close()

	days := map[int64]bool{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Strip inline comments.
		if idx := strings.IndexByte(line, '#'); idx >= 0 {
			line = strings.TrimSpace(line[:idx])
		}
		t, err := time.Parse("2006-01-02", line)
		if err != nil {
			return nil, fmt.Errorf("news_days_file: invalid date %q: %w", line, err)
		}
		unixDay := t.UTC().Unix() / 86400
		days[unixDay] = true
	}
	return days, sc.Err()
}

func build(params map[string]any) (trader.Strategy, error) {
	period, _, err := trader.GetInt32Param(params, "period")
	if err != nil {
		return nil, err
	}
	if period <= 1 {
		period = 20
	}
	closeStrength, ok, err := trader.GetFloat64Param(params, "close_strength")
	if err != nil {
		return nil, err
	}
	if !ok {
		closeStrength = 0.6
	}
	confirmBars, ok, err := trader.GetInt32Param(params, "confirm_bars")
	if err != nil {
		return nil, err
	}
	if !ok {
		confirmBars = 2
	}
	adxPeriod, ok, err := trader.GetInt32Param(params, "adx_period")
	if err != nil {
		return nil, err
	}
	if !ok {
		adxPeriod = 14
	}
	adxThreshold, ok, err := trader.GetFloat64Param(params, "adx_threshold")
	if err != nil {
		return nil, err
	}
	if !ok {
		adxThreshold = 25.0
	}

	var blockedDays map[int64]bool
	newsDaysFile, ok, err := trader.GetStringParam(params, "news_days_file")
	if err != nil {
		return nil, err
	}
	if ok && newsDaysFile != "" {
		blockedDays, err = LoadNewsDays(newsDaysFile)
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
	})
}
