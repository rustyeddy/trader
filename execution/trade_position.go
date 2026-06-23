package execution

import "github.com/rustyeddy/trader/market"

// Position is the computed aggregate view of all open lots for one instrument.
// Hedged books keep separate long/short exposure and entry prices.
type Position struct {
	Instrument         string
	LongUnits          market.Units
	LongAvgEntryPrice  market.Price
	ShortUnits         market.Units
	ShortAvgEntryPrice market.Price
	NetUnits           market.Units
}

type positionAccum struct {
	pos             Position
	longWeightedPx  int64
	shortWeightedPx int64
}

// InstrumentPositions derives per-instrument Position from all open lots.
func InstrumentPositions(lb *LotBook) map[string]Position {
	if lb == nil {
		return nil
	}

	accums := make(map[string]*positionAccum)
	_ = lb.Range(func(lot *Lot) error {
		if lot == nil || lot.State != LotOpen || lot.RemainingUnits <= 0 {
			return nil
		}

		accum, ok := accums[lot.Instrument]
		if !ok {
			accum = &positionAccum{pos: Position{Instrument: lot.Instrument}}
			accums[lot.Instrument] = accum
		}

		units := int64(lot.RemainingUnits)
		weightedPx := int64(lot.EntryPrice) * units
		switch lot.Side {
		case market.Long:
			accum.pos.LongUnits += lot.RemainingUnits
			accum.longWeightedPx += weightedPx
			accum.pos.NetUnits += lot.RemainingUnits
		case market.Short:
			accum.pos.ShortUnits += lot.RemainingUnits
			accum.shortWeightedPx += weightedPx
			accum.pos.NetUnits -= lot.RemainingUnits
		}
		return nil
	})

	result := make(map[string]Position, len(accums))
	for inst, accum := range accums {
		if accum.pos.LongUnits > 0 {
			accum.pos.LongAvgEntryPrice = market.Price(accum.longWeightedPx / int64(accum.pos.LongUnits))
		}
		if accum.pos.ShortUnits > 0 {
			accum.pos.ShortAvgEntryPrice = market.Price(accum.shortWeightedPx / int64(accum.pos.ShortUnits))
		}
		result[inst] = accum.pos
	}

	return result
}
