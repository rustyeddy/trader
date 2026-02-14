package sim

import "github.com/rustyeddy/trader/pricing"

type Position struct {
	Side  int   // +1 long, -1 short
	Entry int32 // scaled price
	Stop  int32 // scaled price
	Take  int32 // scaled price
	Open  bool
}

func (p *Position) CheckExit(c pricing.Candle) (exitPrice int32, hit bool) {
	if !p.Open {
		return 0, false
	}

	if p.Side > 0 {
		// long: stop hit if low <= stop, take hit if high >= take
		if c.L <= p.Stop {
			return p.Stop, true
		}
		if c.H >= p.Take {
			return p.Take, true
		}
	} else {
		// short: stop hit if high >= stop, take hit if low <= take
		if c.H >= p.Stop {
			return p.Stop, true
		}
		if c.L <= p.Take {
			return p.Take, true
		}
	}
	return 0, false
}
