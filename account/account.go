package account

import (
	"fmt"
	"math"

	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/portfolio"
	"github.com/rustyeddy/trader/types"
)

type Account struct {
	ID          string
	Name        string
	Currency    string
	Balance     types.Money
	Equity      types.Money
	MarginUsed  types.Money
	FreeMargin  types.Money
	MarginLevel types.Money
	RiskPct     types.Rate

	Positions portfolio.Positions
	Trades    portfolio.Trades
}

func NewAccount(name string, deposit types.Money) *Account {
	act := &Account{
		ID:         types.NewULID(),
		Name:       name,
		Currency:   "USD",
		Balance:    deposit,
		Equity:     deposit,
		MarginUsed: 0.0,
		RiskPct:    types.RateFromFloat(0.05),
	}
	return act
}

// QuoteToAccount returns the current conversion rate from an instrument's
// quote currency into the account's base currency.
//
// It is used for position sizing and risk calculations when a price move
// denominated in quote currency must be expressed in account currency.
//
// Examples for a USD account:
//   - EURUSD -> 1.0
//   - USDJPY -> 1 / USDJPY
//   - EURGBP -> GBPUSD, or 1 / USDGBP if only the inverse exists
//
// The returned Rate is scaled by types.RateScale.
func (act *Account) QuoteToAccount(meta *market.Instrument, price types.Price) (types.Rate, error) {
	// Case 1: quote currency == account currency (EUR_USD, GBP_USD, etc.)
	if meta.QuoteCurrency == act.Currency {
		return types.Rate(types.RateScale), nil // 1.000000
	}

	// Case 2: base currency == account currency (USD_JPY, USD_CHF, etc.)
	if meta.BaseCurrency == act.Currency {

		// rateScaled = RateScale * PriceScale / midScaled
		r, err := types.MulDiv64(types.RateScale, int64(types.PriceScale), int64(price))
		if err != nil {
			return 0, err
		}
		return types.Rate(r), nil
	}

	// Case 3: Cross currency (future-proofing)
	return 0, fmt.Errorf("cross conversion not implemented for %s → %s", meta.QuoteCurrency, act.Currency)
}

func (act *Account) RealizePNL(pos *portfolio.Position, exit types.Price) (types.Money, error) {
	if act == nil {
		return 0, fmt.Errorf("nil account")
	}
	if pos == nil {
		return 0, fmt.Errorf("nil position")
	}
	if pos.Instrument == nil {
		return 0, fmt.Errorf("position instrument is empty")
	}
	if pos.Price <= 0 {
		return 0, fmt.Errorf("position entry price must be > 0")
	}
	if exit <= 0 {
		return 0, fmt.Errorf("exit price must be > 0")
	}
	if pos.Units <= 0 {
		return 0, fmt.Errorf("position units must be > 0")
	}

	qta, err := act.QuoteToAccount(pos.Instrument, exit)
	if err != nil {
		return 0, err
	}

	delta := float64(exit-pos.Price) / float64(types.PriceScale)
	pnlQuote := float64(pos.Side) * delta * float64(pos.Units)
	pnlAcct := pnlQuote * qta.Float64()
	pnlMoney := types.MoneyFromFloat(pnlAcct)

	act.Balance += pnlMoney
	act.Equity = act.Balance

	return pnlMoney, nil
}

func (act *Account) ClosePosition(pos *portfolio.Position, exit types.Price, exitTime types.Timestamp, reason string) (types.Money, error) {
	if act == nil {
		return 0, fmt.Errorf("nil account")
	}
	if pos.Instrument == nil {
		return 0, fmt.Errorf("position instrument is empty")
	}
	if exit <= 0 {
		return 0, fmt.Errorf("exit price must be > 0")
	}

	pnl, err := act.RealizePNL(pos, exit)
	if err != nil {
		return 0, err
	}

	act.Positions.Delete(pos.ID)
	act.Trades.Add(portfolio.Trade{
		EntryTime:  pos.Timestamp,
		ExitTime:   exitTime,
		Side:       pos.Side,
		EntryPrice: pos.Price,
		ExitPrice:  exit,
		Units:      pos.Units,
		PNL:        pnl,
		Reason:     reason,
	})

	return pnl, nil
}

func (act *Account) OpenPosition(t types.Timestamp, c market.Candle, req *portfolio.OpenRequest) {
	// Fill model: enter at bar close.
	entry := c.Close

	pos := portfolio.Position{
		Instrument: req.Instrument,
		ID:         types.NewULID(),
		Side:       req.Side,
		Units:      req.Units,
		Stop:       req.Stop,
		Take:       req.Take,
		Price:      entry,
		Timestamp:  t,
	}
	act.Positions.Add(pos)
}

func (act *Account) closePosition(t types.Timestamp, exit types.Price, reason string) {
	// p := act.Positions[""]
	// delta := float64(exit-p.EntryPrice) / float64(types.PriceScale)
	// pnlQuote := float64(p.Side) * delta * float64(p.Units)

	// qta, err := act.QuoteToAccount(context.TODO(), act.Instrument, exit)
	// if err != nil {
	// 	qta = types.Rate(types.RateScale) // fallback only if you want one
	// }

	// pnlAcct := pnlQuote * qta.Float64()
	// pnlMoney := types.MoneyFromFloat(pnlAcct)

	// act.Balance += pnlMoney
	// act.Equity = e.Account.Balance

	// e.Trades = append(act.Trades, portfolio.Trade{
	// 	EntryTime:  p.EntryTime,
	// 	ExitTime:   t,
	// 	Side:       p.Side,
	// 	EntryPrice: p.EntryPrice,
	// 	ExitPrice:  exit,
	// 	Units:      p.Units,
	// 	PNL:        pnlMoney,
	// 	Reason:     reason,
	// })
}

func (act *Account) TradeMargin(units types.Units, price types.Price, meta *market.Instrument) (types.Money, error) {
	if meta.MarginRate <= 0 {
		return 0, fmt.Errorf("invalid margin rate for %s: %d", meta.Name, meta.MarginRate)
	}

	u := types.Abs64(int64(units))
	p := int64(price)
	if p <= 0 {
		return 0, fmt.Errorf("invalid price: %d", p)
	}

	// Step 1: notionalQuoteMicro = u * p * MoneyScale / PriceScale
	// Do it as: (u*p)/PriceScale first, then *MoneyScale (more stable),
	// but we need micro units, so we compute in one MulDiv:
	up, err := types.MulDiv64(u, p, int64(types.PriceScale)) // quote units (unscaled)
	if err != nil {
		return 0, err
	}
	// quote micro units:
	notionalQuoteMicro, err := types.MulDiv64(up, int64(types.MoneyScale), 1)
	if err != nil {
		return 0, err
	}

	qta, err := act.QuoteToAccount(meta, price)
	if err != nil {
		return 0, err
	}

	// Step 2: convert to account micro units: *quoteToAccount / RateScale
	notionalAcctMicro, err := types.MulDiv64(notionalQuoteMicro, int64(qta), types.RateScale)
	if err != nil {
		return 0, err
	}

	// Step 3: margin = notionalAcctMicro * marginRate / RateScale
	marginMicro, err := types.MulDiv64(notionalAcctMicro, int64(meta.MarginRate), types.RateScale)
	if err != nil {
		return 0, err
	}

	return types.Money(marginMicro), nil
}

func RR(entry, stop, takeProfit float64) float64 {
	risk := math.Abs(entry - stop)
	reward := math.Abs(takeProfit - entry)
	if risk == 0 {
		return 0
	}
	return reward / risk
}
