package review

import (
	"fmt"
	"time"

	"github.com/rustyeddy/trader/indicator"
	"github.com/rustyeddy/trader/market"
)

// ReviewPair computes a ReviewResult for one instrument from its W1, D1, and
// H4 candle histories. It is a pure function: all indicator state is local,
// all input is candles, all output is the returned ReviewResult.
func ReviewPair(instrument string, w1, d1, h4 []market.Candle, th Thresholds) (ReviewResult, error) {
	inst := market.GetInstrument(instrument)
	if inst == nil {
		return ReviewResult{}, fmt.Errorf("review: unknown instrument %q", instrument)
	}

	d1Snap, d1Bias, err := computeD1(inst, d1)
	if err != nil {
		return ReviewResult{}, fmt.Errorf("review %s: d1: %w", instrument, err)
	}
	h4Snap, h4Bias, err := computeH4(inst, h4, th)
	if err != nil {
		return ReviewResult{}, fmt.Errorf("review %s: h4: %w", instrument, err)
	}
	w1Snap, w1Bias, err := computeW1(inst, w1)
	if err != nil {
		return ReviewResult{}, fmt.Errorf("review %s: w1: %w", instrument, err)
	}

	bias := combineBias(d1Bias, h4Bias, w1Bias)

	setup := SetupSnapshot{
		PriceEMAATR: h4Snap.PriceEMA20ATR,
		Squeeze:     h4Snap.Squeeze,
		H4Aligned:   h4Bias == d1Bias && d1Bias != "neutral",
		W1Alignment: WeeklyAlignment(w1Bias, d1Bias),
	}
	setup.InValueZone = absF(setup.PriceEMAATR) >= th.ValueZoneMin && absF(setup.PriceEMAATR) <= th.ValueZoneMax

	bucket, notes := Classify(d1Snap, h4Snap, w1Snap, setup, d1Bias, w1Bias, th)

	return ReviewResult{
		Instrument: instrument,
		ScannedAt:  time.Now(),
		Bucket:     bucket,
		Bias:       bias,
		W1:         w1Snap,
		D1:         d1Snap,
		H4:         h4Snap,
		Setup:      setup,
		Notes:      notes,
	}, nil
}

// pricePips converts a fixed-point price delta to pips for the given
// instrument. Boundary conversion only — used for presentation fields.
func pricePips(inst *market.Instrument, delta market.Price) float64 {
	perPip := inst.PriceUnitsPerPip()
	if perPip == 0 {
		return 0
	}
	return float64(delta) / float64(perPip)
}

func absF(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}

// biasFromEMA reports "long" when close is above ema, "short" when below,
// "neutral" when equal.
func biasFromEMA(close, ema market.Price) string {
	switch {
	case close > ema:
		return "long"
	case close < ema:
		return "short"
	default:
		return "neutral"
	}
}

// combineBias resolves a single directional bias from D1 (primary), H4, and
// W1 signals. D1 leads; H4/W1 break ties toward neutral when they disagree.
func combineBias(d1, h4, w1 string) string {
	if d1 != "neutral" {
		return d1
	}
	if h4 != "neutral" {
		return h4
	}
	return w1
}

func computeD1(inst *market.Instrument, candles []market.Candle) (D1Snapshot, string, error) {
	adx, err := indicator.NewADX(14, market.PriceScale)
	if err != nil {
		return D1Snapshot{}, "", err
	}
	ci, err := indicator.NewChoppinessIndex(14, market.PriceScale)
	if err != nil {
		return D1Snapshot{}, "", err
	}
	atr, err := indicator.NewATR(14, market.PriceScale)
	if err != nil {
		return D1Snapshot{}, "", err
	}
	ema20, err := indicator.NewEMA(20, market.PriceScale)
	if err != nil {
		return D1Snapshot{}, "", err
	}
	ema50, err := indicator.NewEMA(50, market.PriceScale)
	if err != nil {
		return D1Snapshot{}, "", err
	}
	bb, err := indicator.NewBollingerBands(20, 2.0, market.PriceScale)
	if err != nil {
		return D1Snapshot{}, "", err
	}

	for _, c := range candles {
		adx.Update(c)
		ci.Update(c)
		atr.Update(c)
		ema20.Update(c)
		ema50.Update(c)
		bb.Update(c)
	}
	if !(adx.Ready() && ci.Ready() && atr.Ready() && ema20.Ready() && ema50.Ready() && bb.Ready()) {
		return D1Snapshot{}, "", fmt.Errorf("insufficient candles: got %d, need warmup for ADX/ATR/CI(14), EMA(50), BB(20)", len(candles))
	}

	last := candles[len(candles)-1]
	closeF := last.Close.Float64()
	atrF := atr.Float64()

	snap := D1Snapshot{
		ADX:      adx.Float64(),
		PlusDI:   adx.PlusDI(),
		MinusDI:  adx.MinusDI(),
		CI:       ci.Float64(),
		ATRPips:  pricePips(inst, atr.Price()),
		EMA20:    ema20.Float64(),
		EMA50:    ema50.Float64(),
		Close:    closeF,
		TrendPct: trendPct(candles, 20),
	}
	if atrF != 0 {
		snap.EMASepATR = (ema20.Float64() - ema50.Float64()) / atrF
		snap.PriceEMA20ATR = (closeF - ema20.Float64()) / atrF
		snap.BBWidthATR = (bb.Upper() - bb.Lower()) / atrF
	}
	snap.BBPctB = bb.PercentBPrice(last.Close)

	return snap, biasFromEMA(last.Close, ema20.Price()), nil
}

func computeH4(inst *market.Instrument, candles []market.Candle, th Thresholds) (H4Snapshot, string, error) {
	adx, err := indicator.NewADX(14, market.PriceScale)
	if err != nil {
		return H4Snapshot{}, "", err
	}
	ci, err := indicator.NewChoppinessIndex(14, market.PriceScale)
	if err != nil {
		return H4Snapshot{}, "", err
	}
	atr, err := indicator.NewATR(14, market.PriceScale)
	if err != nil {
		return H4Snapshot{}, "", err
	}
	ema20, err := indicator.NewEMA(20, market.PriceScale)
	if err != nil {
		return H4Snapshot{}, "", err
	}
	ema50, err := indicator.NewEMA(50, market.PriceScale)
	if err != nil {
		return H4Snapshot{}, "", err
	}
	bb, err := indicator.NewBollingerBands(20, 2.0, market.PriceScale)
	if err != nil {
		return H4Snapshot{}, "", err
	}

	for _, c := range candles {
		adx.Update(c)
		ci.Update(c)
		atr.Update(c)
		ema20.Update(c)
		ema50.Update(c)
		bb.Update(c)
	}
	if !(adx.Ready() && ci.Ready() && atr.Ready() && ema20.Ready() && ema50.Ready() && bb.Ready()) {
		return H4Snapshot{}, "", fmt.Errorf("insufficient candles: got %d, need warmup for ADX/ATR/CI(14), EMA(20), EMA(50), BB(20)", len(candles))
	}

	last := candles[len(candles)-1]
	closeF := last.Close.Float64()
	atrF := atr.Float64()

	snap := H4Snapshot{
		ADX:     adx.Float64(),
		CI:      ci.Float64(),
		ATRPips: pricePips(inst, atr.Price()),
		EMA20:   ema20.Float64(),
		EMA50:   ema50.Float64(),
		Close:   closeF,
	}
	if atrF != 0 {
		snap.PriceEMA20ATR = (closeF - ema20.Float64()) / atrF
		snap.EMASepATR = (ema20.Float64() - ema50.Float64()) / atrF
		widthATR := (bb.Upper() - bb.Lower()) / atrF
		snap.Squeeze = widthATR < th.H4SqueezeWidthATR
	}

	return snap, biasFromEMA(last.Close, ema20.Price()), nil
}

func computeW1(inst *market.Instrument, candles []market.Candle) (W1Snapshot, string, error) {
	ema20, err := indicator.NewEMA(20, market.PriceScale)
	if err != nil {
		return W1Snapshot{}, "", err
	}
	atr, err := indicator.NewATR(14, market.PriceScale)
	if err != nil {
		return W1Snapshot{}, "", err
	}

	for _, c := range candles {
		ema20.Update(c)
		atr.Update(c)
	}
	if !(ema20.Ready() && atr.Ready()) {
		return W1Snapshot{}, "", fmt.Errorf("insufficient candles: got %d, need warmup for EMA(20), ATR(14)", len(candles))
	}

	last := candles[len(candles)-1]
	atrF := atr.Float64()

	snap := W1Snapshot{
		EMA20:   ema20.Float64(),
		Close:   last.Close.Float64(),
		ATRPips: pricePips(inst, atr.Price()),
	}
	if atrF != 0 {
		snap.WeekUsedPct = (last.High.Float64() - last.Low.Float64()) / atrF
	}

	return snap, biasFromEMA(last.Close, ema20.Price()), nil
}

// trendPct reports the percentage of the last n candles where the
// body/range ratio exceeds 0.6 (a directional, non-indecisive bar).
func trendPct(candles []market.Candle, n int) float64 {
	if len(candles) < n {
		n = len(candles)
	}
	if n == 0 {
		return 0
	}
	window := candles[len(candles)-n:]
	trending := 0
	counted := 0
	for _, c := range window {
		rng := int64(c.High - c.Low)
		if rng <= 0 {
			continue
		}
		body := int64(c.Close - c.Open)
		if body < 0 {
			body = -body
		}
		counted++
		if float64(body)/float64(rng) > 0.6 {
			trending++
		}
	}
	if counted == 0 {
		return 0
	}
	return 100 * float64(trending) / float64(counted)
}
