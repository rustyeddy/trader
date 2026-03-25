package backtest

import (
	"fmt"

	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/types"
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
	OnBar(ctx *CandleContext, c market.Candle) *OrderRequest
}

type Side int8

const (
	Long  Side = +1
	Short Side = -1
)

type OrderRequest struct {
	Side   Side
	Units  types.Units // base units, e.g. 1000
	Stop   types.Price // scaled price (0 = none)
	Take   types.Price // scaled price (0 = none)
	Reason string
}

type CandleContext struct {
	CS         *market.CandleSet
	BarIndex   int
	Instrument string
	Idx        int
	Timestamp  types.Timestamp
	GapBars    int // missing bars between this and previous valid bar

	Pos     *Position
	Balance *types.Money
}

type Position struct {
	Open       bool
	Side       Side
	EntryPrice types.Price
	Units      types.Units
	Stop       types.Price
	Take       types.Price
	EntryIdx   int
	EntryTime  types.Timestamp
}

type Trade struct {
	EntryTime  types.Timestamp
	ExitTime   types.Timestamp
	Side       Side
	EntryPrice types.Price
	ExitPrice  types.Price
	Units      types.Units
	PNL        types.Money // account currency (best-effort)
	Reason     string
}

type CandleEngine struct {
	Instrument string
	types.Timeframe

	AccountCCY string
	Scale      types.Scale6

	Balance types.Money
	Pos     Position
	Trades  []Trade
}

func NewCandleEngine(
	instrument string,
	tf types.Timeframe,
	scale types.Scale6,
	startingBalance types.Money,
	accountCCY string,
) *CandleEngine {
	return &CandleEngine{
		Instrument: instrument,
		Timeframe:  tf,
		Scale:      scale,
		AccountCCY: accountCCY,
		Balance:    startingBalance,
	}
}

func (e *CandleEngine) Run(feed CandleFeed, strat CandleStrategy) error {
	if feed == nil {
		return fmt.Errorf("candle backtest: nil feed")
	}
	if strat == nil {
		return fmt.Errorf("candle backtest: nil strategy")
	}
	defer feed.Close()

	strat.Reset()

	barIndex := 0
	var prevTS types.Timestamp

	var lastTS types.Timestamp
	var lastC market.Candle
	haveLast := false

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
			Pos:        &e.Pos,
			Balance:    &e.Balance,
		}
		barIndex++

		if e.Pos.Open {
			if exitPx, reason, hit := checkExit(e.Pos, c); hit {
				e.closePosition(ts, exitPx, reason)
			}
		}

		req := strat.OnBar(ctx, c)
		if req == nil || e.Pos.Open || req.Units == 0 {
			continue
		}

		e.openPosition(ctx.BarIndex, ts, c, req)
	}

	if err := feed.Err(); err != nil {
		return err
	}

	if e.Pos.Open && haveLast {
		e.closePosition(lastTS, lastC.Close, "end_of_data")
	}

	return nil
}

func (e *CandleEngine) openPosition(idx int, t types.Timestamp, c market.Candle, req *OrderRequest) {
	// Fill model: enter at bar close.
	entry := c.Close

	e.Pos = Position{
		Open:       true,
		Side:       req.Side,
		EntryPrice: entry,
		Units:      req.Units,
		Stop:       req.Stop,
		Take:       req.Take,
		EntryIdx:   idx,
		EntryTime:  t,
	}
}

func (e *CandleEngine) closePosition(t types.Timestamp, exit types.Price, reason string) {
	p := e.Pos
	e.Pos.Open = false

	// PnL in quote currency (best-effort): (exit-entry) * units (long) or opposite (short)
	deltaScaled := int64(exit - p.EntryPrice) // scaled price units
	pnlScaled := int64(p.Side) * deltaScaled * int64(p.Units)

	// Convert scaled PnL to float quote currency
	pnlQuote := float64(pnlScaled) / float64(e.Scale)

	// If quote currency matches account, treat pnlQuote as account currency.
	pnlAcct := pnlQuote
	if meta, ok := market.Instruments[e.Instrument]; ok {
		if e.AccountCCY != "" && meta.QuoteCurrency != "" && meta.QuoteCurrency != e.AccountCCY {
			// TODO: add FX conversion using a price source. For now, leave as quote currency.
			pnlAcct = pnlQuote
		}
	}

	pnlMoney := types.MoneyFromFloat(pnlAcct)
	e.Balance += pnlMoney
	e.Trades = append(e.Trades, Trade{
		EntryTime:  p.EntryTime,
		ExitTime:   t,
		Side:       p.Side,
		EntryPrice: p.EntryPrice,
		ExitPrice:  exit,
		Units:      p.Units,
		PNL:        pnlMoney,
		Reason:     reason,
	})
}

// checkExit evaluates stop/take on OHLC.
// If both stop & take hit in same bar, we assume stop-first (pessimistic).
func checkExit(p Position, c market.Candle) (exitPx types.Price, reason string, hit bool) {
	if !p.Open {
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
func PipScaled(pipLocation int) types.Price {
	pow := int64(1)
	for i := 0; i < -pipLocation; i++ {
		pow *= 10
	}
	return types.Price(types.PriceScale / types.Scale6(pow))
}
