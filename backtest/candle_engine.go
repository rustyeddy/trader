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
	OnBar(ctx *CandleContext, c market.OHLC) *OrderRequest
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
	CS        *market.CandleSet
	Idx       int
	Timestamp types.Timestamp
	GapBars   int // missing bars between this and previous valid bar

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
	CS         *market.CandleSet
	AccountCCY string

	Balance types.Money
	Pos     Position
	Trades  []Trade
}

func NewCandleEngine(cs *market.CandleSet, startingBalance types.Money, accountCCY string) *CandleEngine {
	return &CandleEngine{
		CS:         cs,
		AccountCCY: accountCCY,
		Balance:    startingBalance,
	}
}

func (e *CandleEngine) Run(strat CandleStrategy) error {
	if e.CS == nil {
		return fmt.Errorf("candle backtest: nil CandleSet")
	}
	if e.CS.Timeframe != 3600 {
		return fmt.Errorf("candle backtest: expected H1 CandleSet (3600s), got %d", e.CS.Timeframe)
	}
	if strat == nil {
		return fmt.Errorf("candle backtest: nil strategy")
	}

	strat.Reset()

	it := e.CS.Iterator()
	prevIdx := -1

	for it.Next() {
		idx := it.Index()
		t := it.Timestamp()
		c := it.Candle()

		gapBars := 0
		if prevIdx != -1 {
			gapBars = idx - prevIdx - 1
		}
		prevIdx = idx

		ctx := &CandleContext{
			CS:        e.CS,
			Idx:       idx,
			Timestamp: t,
			GapBars:   gapBars,
			Pos:       &e.Pos,
			Balance:   &e.Balance,
		}

		// 1) Handle exits on this bar.
		if e.Pos.Open {
			if exitPx, reason, hit := checkExit(e.Pos, c); hit {
				e.closePosition(t, exitPx, reason)
			}
		}

		// 2) Strategy entry.
		req := strat.OnBar(ctx, c)
		if req == nil {
			continue
		}
		if e.Pos.Open {
			// One position at a time (for now)
			continue
		}
		if req.Units == 0 {
			continue
		}

		e.openPosition(idx, t, c, req)
	}

	return nil
}

func (e *CandleEngine) openPosition(idx int, t types.Timestamp, c market.OHLC, req *OrderRequest) {
	// Fill model: enter at bar close.
	entry := c.C

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
	pnlQuote := float64(pnlScaled) / float64(e.CS.Scale)

	// If quote currency matches account, treat pnlQuote as account currency.
	pnlAcct := pnlQuote
	if meta, ok := market.Instruments[e.CS.Name]; ok {
		if e.AccountCCY != "" && meta.QuoteCurrency != "" && meta.QuoteCurrency != e.AccountCCY {
			// TODO: add FX conversion using a price source. For now, leave as quote currency.
			pnlAcct = pnlQuote
		}
	}

	e.Balance += types.Money(pnlAcct)
	e.Trades = append(e.Trades, Trade{
		EntryTime:  p.EntryTime,
		ExitTime:   t,
		Side:       p.Side,
		EntryPrice: p.EntryPrice,
		ExitPrice:  exit,
		Units:      p.Units,
		PNL:        types.Money(pnlAcct),
		Reason:     reason,
	})
}

// checkExit evaluates stop/take on OHLC.
// If both stop & take hit in same bar, we assume stop-first (pessimistic).
func checkExit(p Position, c market.OHLC) (exitPx types.Price, reason string, hit bool) {
	if !p.Open {
		return 0, "", false
	}

	hasStop := p.Stop != 0
	hasTake := p.Take != 0

	switch p.Side {
	case Long:
		stopHit := hasStop && c.L <= p.Stop
		takeHit := hasTake && c.H >= p.Take
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
		stopHit := hasStop && c.H >= p.Stop
		takeHit := hasTake && c.L <= p.Take
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
	return types.Price(types.PriceScale / pow)
}
