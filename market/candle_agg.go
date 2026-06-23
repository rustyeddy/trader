package market

import "fmt"

// Aggregate builds a higher timeframe CandleSet from a lower timeframe CandleSet.
func (cs *candleSet) Aggregate(outTF Timeframe) (*candleSet, error) {
	return cs.aggregate(outTF, 1)
}

// AggregateH1 is an internal helper for trader type processing.
func (cs *candleSet) AggregateH1(minValid int) (*candleSet, error) {
	if cs == nil {
		return nil, fmt.Errorf("nil input candleset")
	}
	if cs.Timeframe != M1 {
		return nil, fmt.Errorf("AggregateH1 requires M1 source, got timeframe %d", cs.Timeframe)
	}
	return cs.aggregate(H1, minValid)
}

func (cs *candleSet) aggregate(outTF Timeframe, minValid int) (*candleSet, error) {
	if cs == nil {
		return nil, fmt.Errorf("nil input candleset")
	}
	if cs.Timeframe <= 0 || outTF <= 0 {
		return nil, fmt.Errorf("bad timeframe cs=%d out=%d", cs.Timeframe, outTF)
	}
	if outTF%cs.Timeframe != 0 {
		return nil, fmt.Errorf("outTF %d must be multiple of csTF %d", outTF, cs.Timeframe)
	}

	ratio := int(outTF / cs.Timeframe)
	if minValid < 1 {
		minValid = 1
	}
	if minValid > ratio {
		minValid = ratio
	}

	if len(cs.Candles) == 0 {
		return &candleSet{
			Instrument: cs.Instrument,
			Start:      cs.Start,
			Timeframe:  outTF,
			Scale:      cs.Scale,
			Source:     cs.Source,
		}, nil
	}

	inTF := Timestamp(cs.Timeframe)
	outStep := Timestamp(outTF)
	start := (cs.Start / outStep) * outStep
	end := cs.Start + Timestamp(len(cs.Candles)-1)*inTF
	outLen := int((end-start)/outStep) + 1

	out := &candleSet{
		Instrument: cs.Instrument,
		Start:      start,
		Timeframe:  outTF,
		Scale:      cs.Scale,
		Source:     cs.Source,
		Candles:    make([]Candle, outLen),
		Valid:      make([]uint64, (outLen+63)/64),
	}

	hasValidBits := len(cs.Valid) > 0
	for oi := 0; oi < outLen; oi++ {
		windowStart := start + Timestamp(oi)*outStep
		firstIdx := int((windowStart - cs.Start) / inTF)

		var (
			outC       Candle
			validCount int
			openSet    bool
			sumTicks   int64
			sumSpread  int64
		)

		for ii := 0; ii < ratio; ii++ {
			idx := firstIdx + ii
			if idx < 0 || idx >= len(cs.Candles) {
				continue
			}
			if hasValidBits && !bitIsSet(cs.Valid, idx) {
				continue
			}

			c := cs.Candles[idx]
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

		if !openSet || validCount < minValid {
			continue
		}

		outC.Ticks = int32(sumTicks)
		if sumTicks > 0 {
			outC.AvgSpread = Price((sumSpread + sumTicks/2) / sumTicks)
		}

		out.Candles[oi] = outC
		bitSet(out.Valid, oi)
	}

	return out, nil
}
