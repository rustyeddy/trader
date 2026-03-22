package sim

import (
	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/types"
)

type Position struct {
	Side  types.Units // +1 long, -1 short
	Entry types.Price // scaled price
	Stop  types.Price // scaled price
	Take  types.Price // scaled price
	Open  bool
}

func (p *Position) CheckExit(c market.Candle) (exitPrice types.Price, hit bool) {
	if !p.Open {
		return 0, false
	}

	if p.Side > 0 {
		// long: stop hit if low <= stop, take hit if high >= take
		if c.Low <= p.Stop {
			return p.Stop, true
		}
		if c.High >= p.Take {
			return p.Take, true
		}
	} else {
		// short: stop hit if high >= stop, take hit if low <= take
		if c.High >= p.Stop {
			return p.Stop, true
		}
		if c.Low <= p.Take {
			return p.Take, true
		}
	}
	return 0, false
}
