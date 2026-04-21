package trader

import (
	"context"
	"fmt"
)

type CandleEngine struct {
	Instrument string
	Timeframe
	Pos     *Position
	Account *Account
}

func NewCandleEngine(instrument string, tf Timeframe, account *Account) *CandleEngine {
	return &CandleEngine{
		Instrument: instrument,
		Timeframe:  tf,
		Account:    account,
	}
}

func (e *CandleEngine) Run(feed candleIterator, strat Strategy) error {
	return e.RunContext(context.Background(), feed, strat)
}

func (e *CandleEngine) RunContext(ctx context.Context, feed candleIterator, strat Strategy) error {
	if e.Account == nil {
		return fmt.Errorf("candle backtest: nil account")
	}

	if feed == nil {
		return fmt.Errorf("candle backtest: nil feed")
	}
	if strat == nil {
		return fmt.Errorf("candle backtest: nil strategy")
	}
	defer feed.Close()
	strat.Reset()

	barIndex := 0
	var prevTS Timestamp
	var lastTS Timestamp
	var lastC Candle
	haveLast := false

	count := 0
	for feed.Next() {
		ts := feed.Timestamp()
		c := feed.Candle()

		lastTS = ts
		lastC = c
		haveLast = true

		gapBars := 0
		if prevTS != 0 {
			delta := int64(ts - prevTS)
			if delta > int64(e.Timeframe) {
				gapBars = int(delta/int64(e.Timeframe)) - 1
			}
		}

		prevTS = ts
		barCtx := withStrategyRuntime(ctx, e.Instrument, barIndex, gapBars, e.Account)
		ct := &CandleTime{Candle: c, Timestamp: ts}
		barPositions := Positions{positions: make(map[string]*Position, len(e.Account.Positions.Positions()))}
		for id, pos := range e.Account.Positions.Positions() {
			barPositions.positions[id] = pos
		}
		barIndex++
		if e.Pos != nil {
			if exitPx, _, hit := checkExit(e.Pos, c); hit {
				trade := &Trade{
					TradeCommon: e.Pos.TradeCommon,
					FillPrice:   exitPx,
					FillTime:    ts,
				}
				if err := e.Account.ClosePosition(e.Pos, trade); err != nil {
					return err
				}
				e.Pos = nil
			}
		}

		plan := strat.Update(barCtx, ct, &barPositions)
		if plan == nil {
			continue
		}

		if e.Pos != nil {
			for _, cl := range plan.Closes {
				if cl == nil {
					continue
				}
				if cl.Position != nil && cl.Position != e.Pos {
					continue
				}
				trade := &Trade{
					TradeCommon: e.Pos.TradeCommon,
					FillPrice:   cl.Price,
					FillTime:    cl.Timestamp,
				}
				if trade.FillPrice == 0 {
					trade.FillPrice = c.Close
				}
				if trade.FillTime == 0 {
					trade.FillTime = ts
				}
				if err := e.Account.ClosePosition(e.Pos, trade); err != nil {
					return err
				}
				e.Pos = nil
				break
			}
		}

		if e.Pos != nil || len(plan.Opens) == 0 {
			continue
		}

		req := plan.Opens[0]
		if req == nil {
			continue
		}

		if req.Units == 0 {
			if req.Stop == 0 {
				return fmt.Errorf("risk sizing requires a stop price")
			}
			if err := e.Account.SizePosition(req); err != nil {
				return err
			}
		}
		count++
		e.Account.OpenPosition(ts, c, req)
		e.Pos = e.Account.Positions.Positions()[req.ID]
	}

	if err := feed.Err(); err != nil {
		return err
	}

	if e.Pos != nil && haveLast {
		trade := &Trade{
			TradeCommon: e.Pos.TradeCommon,
			FillPrice:   lastC.Close,
			FillTime:    lastTS,
		}
		if err := e.Account.ClosePosition(e.Pos, trade); err != nil {
			return err
		}
		e.Pos = nil
	}

	return nil
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

// PipScaled returns the pip size in *scaled* int32 units, based on the instrument pip location.
// Example: EUR_USD pipLocation=-4, scale=1_000_000 => pipScaled=100.
func PipScaled(pipLocation int) Price {
	pow := int64(1)
	for i := 0; i < -pipLocation; i++ {
		pow *= 10
	}
	return Price(PriceScale / Scale6(pow))
}
