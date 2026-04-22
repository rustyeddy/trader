package trader

import "fmt"

func gapBarsSince(prevTS, ts Timestamp, tf Timeframe) int {
	if prevTS == 0 {
		return 0
	}

	delta := int64(ts - prevTS)
	if delta <= int64(tf) {
		return 0
	}

	return int(delta/int64(tf)) - 1
}

func snapshotPositions(src *Positions) *Positions {
	if src == nil {
		return &Positions{}
	}

	srcMap := src.Positions()
	if len(srcMap) == 0 {
		return &Positions{}
	}

	out := Positions{
		positions: make(map[string]*Position, len(srcMap)),
	}
	for id, pos := range srcMap {
		out.positions[id] = pos
	}

	return &out
}

func closePositionAtPrice(acct *Account, pos *Position, px Price, ts Timestamp) error {
	if acct == nil {
		return fmt.Errorf("nil account")
	}
	if pos == nil {
		return fmt.Errorf("nil position")
	}

	trade := &Trade{
		TradeCommon: pos.TradeCommon,
		FillPrice:   px,
		FillTime:    ts,
	}
	return acct.ClosePosition(pos, trade)
}

func closePositionFromRequest(acct *Account, pos *Position, cl *closeRequest, fallback CandleTime) error {
	if cl == nil {
		return fmt.Errorf("nil close request")
	}

	px := cl.Price
	if px == 0 {
		px = fallback.Close
	}

	ts := cl.Timestamp
	if ts == 0 {
		ts = fallback.Timestamp
	}

	return closePositionAtPrice(acct, pos, px, ts)
}

func firstMatchingClose(plan *StrategyPlan, current *Position) *closeRequest {
	if plan == nil || current == nil {
		return nil
	}

	for _, cl := range plan.Closes {
		if cl == nil {
			continue
		}
		if cl.Position != nil && cl.Position != current {
			continue
		}
		return cl
	}

	return nil
}

func firstOpenRequest(plan *StrategyPlan) *OpenRequest {
	if plan == nil || len(plan.Opens) == 0 {
		return nil
	}
	return plan.Opens[0]
}

func ensureSizedOpenRequest(acct *Account, req *OpenRequest) error {
	if req == nil {
		return nil
	}
	if req.Units != 0 {
		return nil
	}
	if req.Stop == 0 {
		return fmt.Errorf("risk sizing requires a stop price")
	}
	return acct.SizePosition(req)
}

func forceClosePositionAtEnd(acct *Account, pos *Position, lastC Candle, lastTS Timestamp) error {
	return closePositionAtPrice(acct, pos, lastC.Close, lastTS)
}

// checkExit evaluates stop/take on OHLC.
// If both stop & take hit in same bar, we assume stop-first (pessimistic).
func checkExit(p *Position, c Candle) (exitPx Price, reason string, hit bool) {
	if p == nil {
		return 0, "", false
	}

	hasStop := p.Stop != 0
	hasTake := p.Take != 0

	switch p.Side {
	case Long:
		stopHit := hasStop && c.Low <= p.Stop
		takeHit := hasTake && c.High >= p.Take
		if stopHit && takeHit {
			return p.Stop, "STOP&TAKE same bar (stop-first)", true
		}
		if stopHit {
			return p.Stop, "STOP", true
		}
		if takeHit {
			return p.Take, "TAKE", true
		}

	case Short:
		stopHit := hasStop && c.High >= p.Stop
		takeHit := hasTake && c.Low <= p.Take
		if stopHit && takeHit {
			return p.Stop, "STOP&TAKE same bar (stop-first)", true
		}
		if stopHit {
			return p.Stop, "STOP", true
		}
		if takeHit {
			return p.Take, "TAKE", true
		}
	}

	return 0, "", false
}
