package trader

import (
	"fmt"
)

// CandleStrategy is called once per *valid* candle (Iterator() skips invalid bars).
// It may return an OrderRequest to open a position.
//
// This engine is intentionally simple:
//   - one position at a time
//   - market entries at close
//   - stop/take evaluated on OHLC of each bar
type CandleStrategy interface {
	Name() string
	Reset()
	OnBar(ctx *CandleContext, c Candle) *OpenRequest
}

type CandleSide int8

type CandleContext struct {
	CS         *candleSet
	BarIndex   int
	Instrument string
	Idx        int
	Timestamp  Timestamp
	GapBars    int // missing bars between this and previous valid bar

	Pos     *Position
	Account *Account
}

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

func (e *CandleEngine) Run(feed candleIterator, strat CandleStrategy) error {
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
		ctx := &CandleContext{
			Instrument: e.Instrument,
			Timestamp:  ts,
			Idx:        barIndex,
			BarIndex:   barIndex,
			GapBars:    gapBars,
			Pos:        e.Pos,
			Account:    e.Account,
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

		req := strat.OnBar(ctx, c)
		if req == nil || e.Pos != nil {
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
