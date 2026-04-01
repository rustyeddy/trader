package backtest

import (
	"context"
	"fmt"

	"github.com/rustyeddy/trader/account"
	"github.com/rustyeddy/trader/data"
	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/portfolio"
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
	OnBar(ctx *CandleContext, c market.Candle) *portfolio.OpenRequest
}

type Side int8

type CandleContext struct {
	CS         *market.CandleSet
	BarIndex   int
	Instrument string
	Idx        int
	Timestamp  types.Timestamp
	GapBars    int // missing bars between this and previous valid bar

	Pos     *portfolio.Position
	Account *account.Account
}

type CandleEngine struct {
	Instrument string
	types.Timeframe
	Pos     *portfolio.Position
	Account *account.Account
}

func NewCandleEngine(instrument string, tf types.Timeframe, account *account.Account) *CandleEngine {
	return &CandleEngine{
		Instrument: instrument,
		Timeframe:  tf,
		Account:    account,
	}
}

func (e *CandleEngine) Run(feed data.CandleIterator, strat CandleStrategy) error {
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
	var prevTS types.Timestamp
	var lastTS types.Timestamp
	var lastC market.Candle
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
			if exitPx, reason, hit := checkExit(e.Pos, c); hit {
				fmt.Printf("close position: %d - %d\n", barIndex, count)
				e.Account.ClosePosition(e.Pos, exitPx, ts, reason)
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

			qta, err := e.Account.QuoteToAccount(context.TODO(), e.Instrument, c.Close)
			if err != nil {
				return err
			}

			r := account.SizeRequest{
				Instrument:     e.Instrument,
				Entry:          c.Close,
				Stop:           req.Stop,
				QuoteToAccount: qta,
			}
			res, err := e.Account.SizePosition(r)
			if err != nil {
				return err
			}
			req.Units = res.Units
		}
		count++
		e.Account.OpenPosition(ts, c, req)
	}

	if err := feed.Err(); err != nil {
		return err
	}

	if e.Pos != nil && haveLast {
		fmt.Printf("close position: %d - %d\n", barIndex, count)
		e.Account.ClosePosition(e.Pos, lastC.Close, lastTS, "end_of_data")
	}

	return nil
}

// checkExit evaluates stop/take on OHLC.
// If both stop & take hit in same bar, we assume stop-first (pessimistic).
func checkExit(p *portfolio.Position, c market.Candle) (exitPx types.Price, reason string, hit bool) {
	if p == nil {
		return 0, "", false
	}

	hasStop := p.Stop != 0
	hasTake := p.Take != 0

	switch p.Side {
	case types.Long:
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
	case types.Short:
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
