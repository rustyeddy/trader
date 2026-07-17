package datamanager

import (
	"fmt"
	"time"

	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/types"
)

// Aggregate builds a higher timeframe CandleSet from a lower timeframe CandleSet.
func (cs *CandleSet) Aggregate(outTF types.Timeframe) (*CandleSet, error) {
	return cs.aggregate(outTF, 1)
}

// AggregateH1 is an internal helper for trader type processing.
func (cs *CandleSet) AggregateH1(minValid int) (*CandleSet, error) {
	if cs == nil {
		return nil, fmt.Errorf("nil input candleset")
	}
	if cs.Timeframe != types.M1 {
		return nil, fmt.Errorf("AggregateH1 requires M1 source, got timeframe %d", cs.Timeframe)
	}
	return cs.aggregate(types.H1, minValid)
}

func (cs *CandleSet) aggregate(outTF types.Timeframe, minValid int) (*CandleSet, error) {
	if cs == nil {
		return nil, fmt.Errorf("nil input candleset")
	}
	if cs.Timeframe <= 0 || outTF <= 0 {
		return nil, fmt.Errorf("bad timeframe cs=%d out=%d", cs.Timeframe, outTF)
	}
	if outTF%cs.Timeframe != 0 {
		return nil, fmt.Errorf("outTF %d must be multiple of csTF %d", outTF, cs.Timeframe)
	}
	if minValid < 1 {
		minValid = 1
	}

	if len(cs.Candles) == 0 {
		return &CandleSet{
			Instrument: cs.Instrument,
			Start:      cs.Start,
			Timeframe:  outTF,
			Scale:      cs.Scale,
			Source:     cs.Source,
		}, nil
	}

	// D1 buckets aren't evenly spaced by 86400 seconds: OANDA's true day
	// boundary is 17:00 America/New_York, DST-aware, so the calendar day
	// containing a DST transition is 23 or 25 wall-clock hours. A fixed
	// stride from a single anchor (correct for every other timeframe here,
	// since their grid boundaries don't depend on where the broker's
	// trading day begins) silently drifts D1 boundaries by an hour after
	// crossing a transition. Walk real day boundaries instead.
	if outTF == types.D1 {
		return cs.aggregateDaily(minValid)
	}

	ratio := int(outTF / cs.Timeframe)
	if minValid > ratio {
		minValid = ratio
	}

	inTF := types.Timestamp(cs.Timeframe)
	outStep := types.Timestamp(outTF)
	start := (cs.Start / outStep) * outStep
	end := cs.Start + types.Timestamp(len(cs.Candles)-1)*inTF
	outLen := int((end-start)/outStep) + 1

	out := &CandleSet{
		Instrument: cs.Instrument,
		Start:      start,
		Timeframe:  outTF,
		Scale:      cs.Scale,
		Source:     cs.Source,
		Candles:    make([]market.CandleTime, outLen),
		Valid:      make([]uint64, (outLen+63)/64),
	}

	hasValidBits := len(cs.Valid) > 0
	for oi := 0; oi < outLen; oi++ {
		windowStart := start + types.Timestamp(oi)*outStep
		out.Candles[oi].Timestamp = windowStart

		firstIdx := int((windowStart - cs.Start) / inTF)
		outC, validCount := aggregateWindow(cs, firstIdx, firstIdx+ratio, hasValidBits)
		if validCount < minValid {
			continue
		}
		out.Candles[oi].Candle = outC
		types.BitSet(out.Valid, oi)
	}

	return out, nil
}

// aggregateDaily builds a D1 CandleSet from cs (must be a sub-day
// timeframe, typically H1) by walking OANDA's true 17:00
// America/New_York-anchored day boundaries, DST-aware, rather than
// assuming every day spans a fixed 86400 seconds.
func (cs *CandleSet) aggregateDaily(minValid int) (*CandleSet, error) {
	inTF := types.Timestamp(cs.Timeframe)

	csStart := time.Unix(int64(cs.Start), 0).UTC()
	lastCandleTime := time.Unix(int64(cs.Start)+int64(len(cs.Candles)-1)*int64(inTF), 0).UTC()

	var boundaries []time.Time
	for b := types.DailyAlignmentBoundary(csStart); !b.After(lastCandleTime); b = nextDailyBoundary(b) {
		boundaries = append(boundaries, b)
	}

	out := &CandleSet{
		Instrument: cs.Instrument,
		Start:      types.FromTime(boundaries[0]),
		Timeframe:  types.D1,
		Scale:      cs.Scale,
		Source:     cs.Source,
		Candles:    make([]market.CandleTime, len(boundaries)),
		Valid:      make([]uint64, (len(boundaries)+63)/64),
	}

	hasValidBits := len(cs.Valid) > 0
	for oi, boundaryStart := range boundaries {
		out.Candles[oi].Timestamp = types.FromTime(boundaryStart)

		boundaryEnd := nextDailyBoundary(boundaryStart)
		if oi+1 < len(boundaries) {
			boundaryEnd = boundaries[oi+1]
		}

		firstIdx := int((types.FromTime(boundaryStart) - cs.Start) / inTF)
		endIdx := int((types.FromTime(boundaryEnd) - cs.Start) / inTF)

		outC, validCount := aggregateWindow(cs, firstIdx, endIdx, hasValidBits)
		if validCount < minValid {
			continue
		}
		out.Candles[oi].Candle = outC
		types.BitSet(out.Valid, oi)
	}

	return out, nil
}

// aggregateWindow folds cs.Candles[max(0,fromIdx):min(len,toIdx)] into a
// single OHLC candle, honoring cs.Valid when present. Returns the zero
// Candle and validCount==0 if no input candle in range was open-eligible.
func aggregateWindow(cs *CandleSet, fromIdx, toIdx int, hasValidBits bool) (market.Candle, int) {
	var (
		outC       market.Candle
		validCount int
		openSet    bool
		sumTicks   int64
		sumSpread  int64
	)

	for idx := fromIdx; idx < toIdx; idx++ {
		if idx < 0 || idx >= len(cs.Candles) {
			continue
		}
		if hasValidBits && !types.BitIsSet(cs.Valid, idx) {
			continue
		}

		c := cs.Candles[idx].Candle
		if !openSet {
			outC.Open = c.Open
			outC.High = c.High
			outC.Low = c.Low
			openSet = true
		} else {
			if c.High > outC.High {
				outC.High = c.High
			}
			if c.Low < outC.Low {
				outC.Low = c.Low
			}
		}

		outC.Close = c.Close
		if c.MaxSpread > outC.MaxSpread {
			outC.MaxSpread = c.MaxSpread
		}

		ticks := int64(c.Ticks)
		sumTicks += ticks
		if ticks > 0 {
			sumSpread += int64(c.AvgSpread) * ticks
		}
		validCount++
	}

	if !openSet {
		return market.Candle{}, 0
	}

	outC.Ticks = int32(sumTicks)
	if sumTicks > 0 {
		outC.AvgSpread = types.Price((sumSpread + sumTicks/2) / sumTicks)
	}
	return outC, validCount
}
