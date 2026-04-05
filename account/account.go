package account

import (
	"context"
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
		RiskPct:    types.RateFromFloat(0.005),
	}
	return act
}

func (act *Account) Print() {
	fmt.Printf("Account: %+v\n", act)
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

func (act *Account) AddPosition(ctx context.Context, pos *portfolio.Position) error {
	if act == nil {
		return fmt.Errorf("nil account")
	}
	if pos.Common.Instrument == nil {
		return fmt.Errorf("position instrument is nil")
	}
	if pos.Common.Units <= 0 {
		return fmt.Errorf("position units must be > 0")
	}
	if pos.FillPrice <= 0 {
		return fmt.Errorf("position price must be > 0")
	}
	if pos.ID == "" {
		pos.ID = types.NewULID()
	}

	act.Positions.Add(pos)
	// At entry time, use the fill/entry price as the current mark.
	// That means unrealized PnL starts at 0 and only margin changes.
	return act.ResolveWithMarks(map[string]types.Price{
		pos.Common.Instrument.Name: pos.FillPrice,
	})
}

func (act *Account) Resolve() error {
	// Fallback resolution: mark every open position at its entry price.
	// Useful immediately after opening positions.
	return act.ResolveWithMarks(nil)
}

func (act *Account) ResolveWithMarks(marks map[string]types.Price) error {
	if act == nil {
		return fmt.Errorf("nil account")
	}

	equity := act.Balance
	var marginUsed types.Money

	err := act.Positions.Range(func(pos *portfolio.Position) error {
		if pos.Common.Instrument == nil {
			return fmt.Errorf("position %q has nil instrument", pos.ID)
		}
		if pos.Common.Units <= 0 {
			return fmt.Errorf("position %q has invalid units %d", pos.ID, pos.Common.Units)
		}
		if pos.FillPrice <= 0 {
			return fmt.Errorf("position %q has invalid entry price %d", pos.ID, pos.FillPrice)
		}

		mark := pos.FillPrice
		if marks != nil {
			if px, ok := marks[pos.Common.Instrument.Name]; ok {
				if px <= 0 {
					return fmt.Errorf("invalid mark for %s: %d", pos.Common.Instrument.Name, px)
				}
				mark = px
			}
		}

		qta, err := act.QuoteToAccount(pos.Common.Instrument, mark)
		if err != nil {
			return err
		}

		entryF := float64(pos.FillPrice) / float64(types.PriceScale)
		markF := float64(mark) / float64(types.PriceScale)
		unitsF := float64(pos.Common.Units)

		delta := markF - entryF
		pnlQuote := float64(pos.Common.Side) * delta * unitsF
		pnlAcct := pnlQuote * qta.Float64()
		equity += types.MoneyFromFloat(pnlAcct)

		m, err := act.TradeMargin(pos.Common.Units, mark, pos.Common.Instrument)
		if err != nil {
			return err
		}
		marginUsed += m

		return nil
	})
	if err != nil {
		return err
	}

	act.Equity = equity
	act.MarginUsed = marginUsed
	act.FreeMargin = act.Equity - act.MarginUsed

	if act.MarginUsed > 0 {
		// Stored as a scaled "money" for now because the field already exists.
		// This is a ratio, not really money. Example: 5.0 means equity is 5x margin used.
		act.MarginLevel = types.MoneyFromFloat(act.Equity.Float64() / act.MarginUsed.Float64())
	} else {
		act.MarginLevel = 0
	}

	return nil
}

func (act *Account) RealizePNL(trade *portfolio.Trade) (types.Money, error) {
	if act == nil {
		return 0, fmt.Errorf("nil account")
	}
	if trade == nil {
		return 0, fmt.Errorf("nil position")
	}
	if trade.Common.Instrument == nil {
		return 0, fmt.Errorf("position instrument is empty")
	}
	if trade.FillPrice <= 0 {
		return 0, fmt.Errorf("position entry price must be > 0")
	}
	if trade.ExitPrice <= 0 {
		return 0, fmt.Errorf("exit price must be > 0")
	}
	if trade.Common.Units <= 0 {
		return 0, fmt.Errorf("position units must be > 0")
	}

	qta, err := act.QuoteToAccount(trade.Common.Instrument, trade.ExitPrice)
	if err != nil {
		return 0, err
	}

	delta := float64(trade.ExitPrice-trade.FillPrice) / float64(types.PriceScale)
	pnlQuote := float64(trade.Common.Side) * delta * float64(trade.Common.Units)
	pnlAcct := pnlQuote * qta.Float64()
	pnlMoney := types.MoneyFromFloat(pnlAcct)

	act.Balance += pnlMoney
	act.Equity = act.Balance

	return pnlMoney, nil
}

func (act *Account) ClosePosition(pos *portfolio.Position, trade *portfolio.Trade) error {
	if act == nil {
		return fmt.Errorf("nil account")
	}
	if pos.Common.Instrument == nil {
		return fmt.Errorf("position instrument is empty")
	}
	if trade.ExitPrice <= 0 {
		return fmt.Errorf("exit price must be > 0")
	}

	pnl, err := act.RealizePNL(trade)
	if err != nil {
		return err
	}
	trade.PNL = pnl
	act.Trades.Add(trade)
	act.Positions.Delete(pos.ID)
	return nil
}

func (act *Account) OpenPosition(t types.Timestamp, c market.Candle, req *portfolio.OpenRequest) {
	// Fill model: enter at bar close.
	entry := c.Close

	pos := &portfolio.Position{
		ID:        types.NewULID(),
		Common:    req.Common,
		FillPrice: entry,
		FillTime:  t,
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

// riskBudget returns the max allowed loss in account-money micro-units.
func (acct *Account) riskBudget() (types.Money, error) {
	if acct.Equity <= 0 {
		return 0, fmt.Errorf("account equity must be > 0")
	}
	if acct.RiskPct <= 0 {
		return 0, fmt.Errorf("account risk_pct must be > 0")
	}

	v, err := types.MulDivFloor64(int64(acct.Equity), int64(acct.RiskPct), types.RateScale)
	if err != nil {
		return 0, err
	}
	if v <= 0 {
		return 0, fmt.Errorf("risk budget must be > 0")
	}
	return types.Money(v), nil
}

// lossPerUnit returns stop-loss exposure for 1 unit in account-money micro-units.
// It uses ceil so we never underestimate loss and accidentally oversize.
func (acct *Account) lossPerUnit(req *portfolio.OpenRequest) (types.Money, error) {
	priceDist := types.Abs64(int64(req.Price) - int64(req.Common.Stop))
	if priceDist == 0 {
		return 0, fmt.Errorf("entry and stop must differ")
	}

	qta, err := acct.QuoteToAccount(req.Common.Instrument, req.Price)
	if err != nil {
		return 0, err
	}

	// quote move per unit -> account money micro-units
	v, err := types.MulDivCeil64(priceDist, int64(types.MoneyScale), int64(types.PriceScale))
	if err != nil {
		return 0, err
	}
	v, err = types.MulDivCeil64(v, int64(qta), types.RateScale)
	if err != nil {
		return 0, err
	}
	if v <= 0 {
		return 0, fmt.Errorf("loss per unit must be > 0")
	}

	return types.Money(v), nil
}

// marginPerUnit returns margin needed for 1 unit in account-money micro-units.
// It uses ceil so we never underestimate required margin.
func (acct *Account) marginPerUnit(inst *portfolio.CommonPortfolio, price types.Price) (types.Money, error) {
	if inst.Instrument == nil {
		return 0, fmt.Errorf("instrument is nil")
	}
	if inst.Instrument.MarginRate <= 0 {
		return 0, fmt.Errorf("invalid margin rate for %s: %d", inst.Instrument.Name, inst.Instrument.MarginRate)
	}
	if price <= 0 {
		return 0, fmt.Errorf("invalid price %d", price)
	}

	qta, err := acct.QuoteToAccount(inst.Instrument, price)
	if err != nil {
		return 0, err
	}

	// notional quote micro-units for 1 unit
	v, err := types.MulDivCeil64(int64(price), int64(types.MoneyScale), int64(types.PriceScale))
	if err != nil {
		return 0, err
	}

	// convert quote -> account
	v, err = types.MulDivCeil64(v, int64(qta), types.RateScale)
	if err != nil {
		return 0, err
	}

	// apply margin rate
	v, err = types.MulDivCeil64(v, int64(inst.Instrument.MarginRate), types.RateScale)
	if err != nil {
		return 0, err
	}
	if v <= 0 {
		return 0, fmt.Errorf("margin per unit must be > 0")
	}

	return types.Money(v), nil
}

func (acct *Account) availableMargin() types.Money {
	// Prefer already-resolved FreeMargin when present.
	if acct.FreeMargin > 0 {
		return acct.FreeMargin
	}

	// Otherwise derive it from current account state.
	fm := acct.Equity - acct.MarginUsed
	if fm > 0 {
		return fm
	}

	return 0
}

func (acct *Account) unitsByRisk(req *portfolio.OpenRequest) (types.Units, error) {
	riskBudget, err := acct.riskBudget()
	if err != nil {
		return 0, err
	}

	lossPerUnit, err := acct.lossPerUnit(req)
	if err != nil {
		return 0, err
	}

	units := types.Units(int64(riskBudget) / int64(lossPerUnit))
	if units <= 0 {
		return 0, fmt.Errorf("risk budget too small for stop distance")
	}
	return units, nil
}

func (acct *Account) unitsByMargin(req *portfolio.OpenRequest) (types.Units, error) {
	freeMargin := acct.availableMargin()
	if freeMargin <= 0 {
		return 0, fmt.Errorf("free margin must be > 0")
	}

	marginPerUnit, err := acct.marginPerUnit(&req.Common, req.Price)
	if err != nil {
		return 0, err
	}

	units := types.Units(int64(freeMargin) / int64(marginPerUnit))
	if units <= 0 {
		return 0, fmt.Errorf("free margin too small for minimum position")
	}
	return units, nil
}

func minUnits(a, b types.Units) types.Units {
	if a < b {
		return a
	}
	return b
}

func (acct *Account) SizePosition(req *portfolio.OpenRequest) error {
	if acct == nil {
		return fmt.Errorf("account is nil")
	}
	if req == nil {
		return fmt.Errorf("request is nil")
	}
	if req.Common.Instrument == nil {
		return fmt.Errorf("req.Common.Instrument is nil")
	}
	if req.Price <= 0 || req.Common.Stop <= 0 {
		return fmt.Errorf("entry and stop must be > 0")
	}
	if req.Price == req.Common.Stop {
		return fmt.Errorf("entry and stop must differ")
	}

	switch req.Common.Side {
	case types.Short:
		if req.Common.Stop <= req.Price {
			return fmt.Errorf("short stop must be greater than price")
		}
	case types.Long:
		if req.Common.Stop >= req.Price {
			return fmt.Errorf("long stop must be less than price")
		}
	default:
		return fmt.Errorf("invalid side %v", req.Common.Side)
	}

	unitsRisk, err := acct.unitsByRisk(req)
	if err != nil {
		return err
	}

	unitsMargin, err := acct.unitsByMargin(req)
	if err != nil {
		return err
	}

	units := minUnits(unitsRisk, unitsMargin)
	if units < req.Common.Instrument.MinimumTradeSize {
		return fmt.Errorf(
			"computed units %d < minimum trade size %d (risk=%d margin=%d)",
			units,
			req.Common.Instrument.MinimumTradeSize,
			unitsRisk,
			unitsMargin,
		)
	}

	req.Common.Units = units
	return nil
}

func RR(entry, stop, takeProfit float64) float64 {
	risk := math.Abs(entry - stop)
	reward := math.Abs(takeProfit - entry)
	if risk == 0 {
		return 0
	}
	return reward / risk
}
