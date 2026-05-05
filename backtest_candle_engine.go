package trader

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

// func (e *CandleEngine) Run(feed candleIterator, strat Strategy) error {
// 	return e.RunContext(context.Background(), feed, strat)
// }

// func (e *CandleEngine) RunContext(ctx context.Context, feed candleIterator, strat Strategy) error {
// 	if e.Account == nil {
// 		return fmt.Errorf("candle backtest: nil account")
// 	}

// 	if feed == nil {
// 		return fmt.Errorf("candle backtest: nil feed")
// 	}
// 	if strat == nil {
// 		return fmt.Errorf("candle backtest: nil strategy")
// 	}
// 	defer feed.Close()
// 	strat.Reset()

// 	barIndex := 0
// 	var prevTS Timestamp
// 	var lastTS Timestamp
// 	var lastC Candle
// 	haveLast := false

// 	count := 0
// 	for feed.Next() {
// 		ts := feed.Timestamp()
// 		c := feed.Candle()

// 		lastTS = ts
// 		lastC = c
// 		haveLast = true

// 		gapBars := gapBarsSince(prevTS, ts, e.Timeframe)
// 		prevTS = ts

// 		barCtx := withStrategyRuntime(ctx, e.Instrument, barIndex, gapBars, e.Account)
// 		ct := &CandleTime{Candle: c, Timestamp: ts}
// 		barPositions := snapshotPositions(&e.Account.Positions)
// 		barIndex++

// 		if e.Pos != nil {
// 			if exitPx, _, hit := checkExit(e.Pos, c); hit {
// 				if err := closePositionAtPrice(e.Account, e.Pos, exitPx, ts); err != nil {
// 					return err
// 				}
// 				e.Pos = nil
// 			}
// 		}

// 		plan := strat.Update(barCtx, ct, barPositions)
// 		if plan == nil {
// 			continue
// 		}

// 		if e.Pos != nil {
// 			if cl := firstMatchingClose(plan, e.Pos); cl != nil {
// 				if err := closePositionFromRequest(e.Account, e.Pos, cl, *ct); err != nil {
// 					return err
// 				}
// 				e.Pos = nil
// 			}
// 		}

// 		if e.Pos != nil {
// 			continue
// 		}

// 		req := firstOpenRequest(plan)
// 		if req == nil {
// 			continue
// 		}

// 		if err := ensureSizedOpenRequest(e.Account, req); err != nil {
// 			return err
// 		}

// 		count++
// 		e.Account.OpenPosition(ts, c, req)
// 		e.Pos = e.Account.Positions.Positions()[req.ID]
// 	}

// 	_ = count

// 	if err := feed.Err(); err != nil {
// 		return err
// 	}

// 	if e.Pos != nil && haveLast {
// 		if err := forceClosePositionAtEnd(e.Account, e.Pos, lastC, lastTS); err != nil {
// 			return err
// 		}
// 		e.Pos = nil
// 	}

// 	return nil
// }

// PipScaled returns the pip size in *scaled* int32 units, based on the instrument pip location.
// Example: EUR_USD pipLocation=-4, scale=1_000_000 => pipScaled=100.
func PipScaled(pipLocation int) Price {
	pow := int64(1)
	for i := 0; i < -pipLocation; i++ {
		pow *= 10
	}
	return Price(PriceScale / Scale6(pow))
}
