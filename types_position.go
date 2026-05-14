package trader

// Position is the computed aggregate view of all open lots for one instrument.
type Position struct {
	Instrument    string
	NetUnits      Units
	AvgEntryPrice Price
	UnrealizedPL  Money
	MarginUsed    Money
}

// InstrumentPositions derives per-instrument Position from all open lots.
func InstrumentPositions(lb *LotBook) map[string]*Position {
	if lb == nil {
		return nil
	}
	result := make(map[string]*Position)
	_ = lb.Range(func(lot *Lot) error {
		if lot == nil || lot.State != LotOpen || lot.RemainingUnits <= 0 {
			return nil
		}
		pos, ok := result[lot.Instrument]
		if !ok {
			pos = &Position{Instrument: lot.Instrument}
			result[lot.Instrument] = pos
		}
		// accumulate net units (positive=long, negative=short)
		if lot.Side == Long {
			pos.NetUnits += lot.RemainingUnits
		} else {
			pos.NetUnits -= lot.RemainingUnits
		}
		return nil
	})

	// compute AvgEntryPrice per instrument (simple average of open lots)
	// We need a second pass to compute weighted average entry price
	type lotAccum struct {
		totalUnits Units
		weightedPx int64
	}
	accums := make(map[string]*lotAccum)
	_ = lb.Range(func(lot *Lot) error {
		if lot == nil || lot.State != LotOpen || lot.RemainingUnits <= 0 {
			return nil
		}
		a, ok := accums[lot.Instrument]
		if !ok {
			a = &lotAccum{}
			accums[lot.Instrument] = a
		}
		u := int64(lot.RemainingUnits)
		if u < 0 {
			u = -u
		}
		a.totalUnits += lot.RemainingUnits
		a.weightedPx += int64(lot.EntryPrice) * u
		return nil
	})
	for inst, a := range accums {
		if pos, ok := result[inst]; ok {
			absUnits := int64(a.totalUnits)
			if absUnits < 0 {
				absUnits = -absUnits
			}
			if absUnits > 0 {
				pos.AvgEntryPrice = Price(a.weightedPx / absUnits)
			}
		}
	}

	return result
}
