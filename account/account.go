package account

import (
	"context"
	"fmt"
	"math"
	"math/bits"
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

	portfolio.Positions
	Trades []*portfolio.Trade
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
func (act *Account) QuoteToAccount(inst string, price types.Price) (types.Rate, error) {
	meta := types.GetInstrument(inst)
	if meta == nil {
		return 0, fmt.Errorf("failed to find instrument %s", inst)
	}
	// Case 1: quote currency == account currency (EUR_USD, GBP_USD, etc.)
	if meta.QuoteCurrency == act.Currency {
		return types.Rate(types.RateScale), nil // 1.000000
	}

	// Case 2: base currency == account currency (USD_JPY, USD_CHF, etc.)
	if meta.BaseCurrency == act.Currency {

		// rateScaled = RateScale * PriceScale / midScaled
		r, err := types.MulDiv64(int64(types.MoneyScale), int64(types.PriceScale), int64(price))
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
	if pos.Common.Instrument == "" {
		return fmt.Errorf("position instrument is nil")
	}
	if pos.Common.Units <= 0 {
		return fmt.Errorf("position units must be > 0")
	}
	if pos.FillPrice <= 0 {
		return fmt.Errorf("position price must be > 0")
	}
	if pos.Common.ID == "" {
		panic("pos.common.id is nil")
	}

	act.Positions.Add(pos)
	// At entry time, use the fill/entry price as the current mark.
	// That means unrealized PnL starts at 0 and only margin changes.
	return act.ResolveWithMarks(map[string]types.Price{
		pos.Common.Instrument: pos.FillPrice,
	})
}

func (act *Account) Resolve() error {
	// Fallback resolution: mark every open position at its entry price.
	// Useful immediately after opening positions.
	return act.ResolveWithMarks(nil)
}

func mulChecked64(a, b int64) (int64, error) {
	if a < 0 || b < 0 {
		return 0, fmt.Errorf("mulChecked64: invalid args a=%d b=%d", a, b)
	}

	hi, lo := bits.Mul64(uint64(a), uint64(b))
	if hi != 0 || lo > uint64(math.MaxInt64) {
		return 0, fmt.Errorf("mulChecked64: overflow a=%d b=%d", a, b)
	}

	return int64(lo), nil
}

func roundHalfAwayFromZero(num, den int64) (int64, error) {
	if num < 0 || den <= 0 {
		return 0, fmt.Errorf("roundHalfAwayFromZero: invalid args num=%d den=%d", num, den)
	}

	q := num / den
	r := num % den
	if r >= (den+1)/2 {
		if q == math.MaxInt64 {
			return 0, fmt.Errorf("roundHalfAwayFromZero: overflow")
		}
		q++
	}

	return q, nil
}

func absInt64Checked(v int64) (int64, error) {
	if v == math.MinInt64 {
		return 0, fmt.Errorf("absInt64Checked: overflow")
	}
	if v < 0 {
		return -v, nil
	}
	return v, nil
}

func signedMulDivRound(a, b, den int64) (int64, error) {
	if b < 0 || den <= 0 {
		return 0, fmt.Errorf("signedMulDivRound: invalid args a=%d b=%d den=%d", a, b, den)
	}

	absA, err := absInt64Checked(a)
	if err != nil {
		return 0, err
	}

	prod, err := mulChecked64(absA, b)
	if err != nil {
		return 0, err
	}

	q, err := roundHalfAwayFromZero(prod, den)
	if err != nil {
		return 0, err
	}

	if a < 0 {
		return -q, nil
	}

	return q, nil
}

func positionUnrealizedPNL(pos *portfolio.Position, mark types.Price, qta types.Rate) (types.Money, error) {
	if pos == nil {
		return 0, fmt.Errorf("nil position")
	}
	if pos.Common.Units <= 0 {
		return 0, fmt.Errorf("position %q has invalid units %d", pos.Common.ID, pos.Common.Units)
	}
	if qta <= 0 {
		return 0, fmt.Errorf("invalid quote-to-account rate %d", qta)
	}

	priceDelta := int64(mark) - int64(pos.FillPrice)
	if priceDelta == 0 {
		return 0, nil
	}

	absDelta, err := absInt64Checked(priceDelta)
	if err != nil {
		return 0, err
	}
	absUnits, err := absInt64Checked(int64(pos.Common.Units))
	if err != nil {
		return 0, err
	}

	deltaUnits, err := mulChecked64(absDelta, absUnits)
	if err != nil {
		return 0, err
	}

	// pnlMicro = round((abs(deltaPrice) * units * qta) / PriceScale)
	whole := deltaUnits / int64(types.PriceScale)
	frac := deltaUnits % int64(types.PriceScale)

	base, err := mulChecked64(whole, int64(qta))
	if err != nil {
		return 0, err
	}

	fracNum, err := mulChecked64(frac, int64(qta))
	if err != nil {
		return 0, err
	}
	fracPart, err := roundHalfAwayFromZero(fracNum, int64(types.PriceScale))
	if err != nil {
		return 0, err
	}

	if base > math.MaxInt64-fracPart {
		return 0, fmt.Errorf("position %q unrealized pnl overflow", pos.Common.ID)
	}
	totalAbs := base + fracPart

	sign := int64(pos.Common.Side)
	if sign != int64(types.Long) && sign != int64(types.Short) {
		return 0, fmt.Errorf("position %q has invalid side %d", pos.Common.ID, pos.Common.Side)
	}
	if priceDelta < 0 {
		sign = -sign
	}
	if sign < 0 {
		totalAbs = -totalAbs
	}

	return types.Money(totalAbs), nil
}

func (act *Account) ResolveWithMarks(marks map[string]types.Price) error {
	if act == nil {
		return fmt.Errorf("nil account")
	}

	equity := act.Balance
	var marginUsed types.Money

	err := act.Positions.Range(func(pos *portfolio.Position) error {
		if pos.Common.Instrument == "" {
			return fmt.Errorf("position %q has nil instrument", pos.Common.ID)
		}
		if pos.Common.Units <= 0 {
			return fmt.Errorf("position %q has invalid units %d", pos.Common.ID, pos.Common.Units)
		}
		if pos.FillPrice <= 0 {
			return fmt.Errorf("position %q has invalid entry price %d", pos.Common.ID, pos.FillPrice)
		}

		mark := pos.FillPrice
		if marks != nil {
			if px, ok := marks[pos.Common.Instrument]; ok {
				if px <= 0 {
					return fmt.Errorf("invalid mark for %s: %d", pos.Common.Instrument, px)
				}
				mark = px
			}
		}

		inst := types.GetInstrument(pos.Common.Instrument)
		if inst == nil {
			return fmt.Errorf("instrument is nil %s\n", inst)
		}

		qta, err := act.QuoteToAccount(pos.Common.Instrument, mark)
		if err != nil {
			return err
		}

		pnl, err := positionUnrealizedPNL(pos, mark, qta)
		if err != nil {
			return err
		}
		equity += pnl

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
		v, err := signedMulDivRound(int64(act.Equity), int64(types.MoneyScale), int64(act.MarginUsed))
		if err != nil {
			return err
		}
		act.MarginLevel = types.Money(v)
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
	if trade.Instrument == "" {
		return 0, fmt.Errorf("position instrument is empty")
	}
	if trade.TradeCommon.Units <= 0 {
		return 0, fmt.Errorf("position units must be > 0")
	}

	qta, err := act.QuoteToAccount(trade.Instrument, trade.ExitPrice)
	if err != nil {
		return 0, err
	}

	pos := &portfolio.Position{
		Common: trade.TradeCommon,
	}
	pnlMoney, err := positionUnrealizedPNL(pos, trade.ExitPrice, qta)
	if err != nil {
		return 0, err
	}

	act.Balance += pnlMoney
	act.Equity = act.Balance

	return pnlMoney, nil
}

func (act *Account) ClosePosition(pos *portfolio.Position, trade *portfolio.Trade) error {
	if act == nil {
		return fmt.Errorf("nil account")
	}
	if pos.Common.Instrument == "" {
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
	act.Trades = append(act.Trades, trade)
	act.Positions.Delete(pos.Common.ID)
	return nil
}

func (act *Account) OpenPosition(t types.Timestamp, c types.Candle, req *portfolio.OpenRequest) {
	// Fill model: enter at bar close.
	entry := c.Close

	pos := &portfolio.Position{
		Common:    req.TradeCommon,
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

func (act *Account) TradeMargin(units types.Units, price types.Price, inst string) (types.Money, error) {
	meta := types.GetInstrument(inst)
	if meta == nil {
		return 0, fmt.Errorf("no such instrument %s\n", inst)
	}

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

	qta, err := act.QuoteToAccount(meta.Name, price)
	if err != nil {
		return 0, err
	}

	// Step 2: convert to account micro units: *quoteToAccount / RateScale
	notionalAcctMicro, err := types.MulDiv64(notionalQuoteMicro, int64(qta), int64(types.RateScale))
	if err != nil {
		return 0, err
	}

	// Step 3: margin = notionalAcctMicro * marginRate / RateScale
	marginMicro, err := types.MulDiv64(notionalAcctMicro, int64(meta.MarginRate), int64(types.RateScale))
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

	v, err := types.MulDivFloor64(int64(acct.Equity), int64(acct.RiskPct), int64(types.RateScale))
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
	priceDist := types.Abs64(int64(req.Price) - int64(req.TradeCommon.Stop))
	if priceDist == 0 {
		return 0, fmt.Errorf("entry and stop must differ")
	}

	qta, err := acct.QuoteToAccount(req.TradeCommon.Instrument, req.Price)
	if err != nil {
		return 0, err
	}

	// quote move per unit -> account money micro-units
	v, err := types.MulDivCeil64(priceDist, int64(types.MoneyScale), int64(types.PriceScale))
	if err != nil {
		return 0, err
	}
	v, err = types.MulDivCeil64(v, int64(qta), int64(types.RateScale))
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
func (acct *Account) marginPerUnit(inst *types.Instrument, price types.Price) (types.Money, error) {
	if inst == nil {
		return 0, fmt.Errorf("instrument is nil")
	}
	if inst.MarginRate <= 0 {
		return 0, fmt.Errorf("invalid margin rate for %s: %d", inst.Name, inst.MarginRate)
	}
	if price <= 0 {
		return 0, fmt.Errorf("invalid price %d", price)
	}

	qta, err := acct.QuoteToAccount(inst.Name, price)
	if err != nil {
		return 0, err
	}

	// notional quote micro-units for 1 unit
	v, err := types.MulDivCeil64(int64(price), int64(types.MoneyScale), int64(types.PriceScale))
	if err != nil {
		return 0, err
	}

	// convert quote -> account
	v, err = types.MulDivCeil64(v, int64(qta), int64(types.RateScale))
	if err != nil {
		return 0, err
	}

	// apply margin rate
	v, err = types.MulDivCeil64(v, int64(inst.MarginRate), int64(types.RateScale))
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

	inst := types.GetInstrument(req.TradeCommon.Instrument)
	if inst == nil {
		return 0, fmt.Errorf("unknown instrument %s", req.TradeCommon.Instrument)
	}
	marginPerUnit, err := acct.marginPerUnit(inst, req.Price)
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
	if req.Instrument == "" {
		return fmt.Errorf("req.TradeCommon.Instrument is nil")
	}
	if req.Price <= 0 || req.Stop <= 0 {
		return fmt.Errorf("entry and stop must be > 0")
	}
	if req.Price == req.Stop {
		return fmt.Errorf("entry and stop must differ")
	}

	switch req.Side {
	case types.Short:
		if req.TradeCommon.Stop <= req.Price {
			return fmt.Errorf("short stop must be greater than price")
		}
	case types.Long:
		if req.Stop >= req.Price {
			return fmt.Errorf("long stop must be less than price")
		}
	default:
		return fmt.Errorf("invalid side %v", req.TradeCommon.Side)
	}

	unitsRisk, err := acct.unitsByRisk(req)
	if err != nil {
		return err
	}

	unitsMargin, err := acct.unitsByMargin(req)
	if err != nil {
		return err
	}

	inst := types.GetInstrument(req.TradeCommon.Instrument)
	if inst == nil {
		return fmt.Errorf("unknown instrument %s\n", req.TradeCommon.Instrument)
	}

	units := minUnits(unitsRisk, unitsMargin)
	if units < inst.MinimumTradeSize {
		return fmt.Errorf(
			"computed units %d < minimum trade size %d (risk=%d margin=%d)",
			units,
			inst.MinimumTradeSize,
			unitsRisk,
			unitsMargin,
		)
	}

	req.Units = units
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
