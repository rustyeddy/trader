package trader

import (
	"context"
	"fmt"
	"math"
)

type Account struct {
	ID          string
	Name        string
	Currency    string
	Balance     Money
	Equity      Money
	MarginUsed  Money
	FreeMargin  Money
	MarginLevel Money
	RiskPct     Rate

	Lots    LotBook
	Matcher CloseMatcher
	Trades  []*Trade
}

func NewAccount(name string, deposit Money) *Account {
	act := &Account{
		ID:         NewULID(),
		Name:       name,
		Currency:   "USD",
		Balance:    deposit,
		Equity:     deposit,
		MarginUsed: 0.0,
		RiskPct:    RateFromFloat(0.005),
		Matcher:    FIFOMatcher{},
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
// The returned Rate is scaled by RateScale.
func (act *Account) QuoteToAccount(inst string, price Price) (Rate, error) {
	meta := GetInstrument(inst)
	if meta == nil {
		return 0, fmt.Errorf("failed to find instrument %s", inst)
	}
	if meta.QuoteCurrency == act.Currency {
		return Rate(rateScale), nil
	}

	if meta.BaseCurrency == act.Currency {
		r, err := mulDiv64(int64(MoneyScale), int64(PriceScale), int64(price))
		if err != nil {
			return 0, err
		}
		return Rate(r), nil
	}

	return 0, fmt.Errorf("cross conversion not implemented for %s → %s", meta.QuoteCurrency, act.Currency)
}

func (act *Account) AddLot(ctx context.Context, lot *Lot) error {
	if act == nil {
		return fmt.Errorf("nil account")
	}
	if lot.Instrument == "" {
		return fmt.Errorf("position instrument is nil")
	}
	if lot.Units <= 0 {
		return fmt.Errorf("position units must be > 0")
	}
	if lot.EntryPrice <= 0 {
		return fmt.Errorf("position price must be > 0")
	}
	if lot.ID == "" {
		panic("pos.common.id is nil")
	}

	act.Lots.Add(lot)
	return act.ResolveWithMarks(map[string]Price{
		lot.Instrument: lot.EntryPrice,
	})
}

func (act *Account) Resolve() error {
	return act.ResolveWithMarks(nil)
}

func lotUnrealizedPNL(lot *Lot, mark Price, qta Rate) (Money, error) {
	if lot == nil {
		return 0, fmt.Errorf("nil position")
	}
	if lot.RemainingUnits <= 0 {
		return 0, fmt.Errorf("position %q has invalid units %d", lot.ID, lot.RemainingUnits)
	}
	if qta <= 0 {
		return 0, fmt.Errorf("invalid quote-to-account rate %d", qta)
	}

	priceDelta := int64(mark) - int64(lot.EntryPrice)
	if priceDelta == 0 {
		return 0, nil
	}

	absDelta, err := absInt64Checked(priceDelta)
	if err != nil {
		return 0, err
	}
	absUnits, err := absInt64Checked(int64(lot.RemainingUnits))
	if err != nil {
		return 0, err
	}

	deltaUnits, err := mulChecked64(absDelta, absUnits)
	if err != nil {
		return 0, err
	}

	whole := deltaUnits / int64(PriceScale)
	frac := deltaUnits % int64(PriceScale)

	base, err := mulChecked64(whole, int64(qta))
	if err != nil {
		return 0, err
	}

	fracNum, err := mulChecked64(frac, int64(qta))
	if err != nil {
		return 0, err
	}
	fracPart, err := roundHalfAwayFromZero(fracNum, int64(PriceScale))
	if err != nil {
		return 0, err
	}

	if base > math.MaxInt64-fracPart {
		return 0, fmt.Errorf("position %q unrealized pnl overflow", lot.ID)
	}
	totalAbs := base + fracPart

	sign := int64(lot.Side)
	if sign != int64(Long) && sign != int64(Short) {
		return 0, fmt.Errorf("position %q has invalid side %d", lot.ID, lot.Side)
	}
	if priceDelta < 0 {
		sign = -sign
	}
	if sign < 0 {
		totalAbs = -totalAbs
	}

	return Money(totalAbs), nil
}

func (act *Account) ResolveWithMarks(marks map[string]Price) error {
	if act == nil {
		return fmt.Errorf("nil account")
	}

	equity := act.Balance
	var marginUsed Money

	err := act.Lots.Range(func(lot *Lot) error {
		if lot.Instrument == "" {
			return fmt.Errorf("position %q has nil instrument", lot.ID)
		}
		if lot.RemainingUnits <= 0 {
			return fmt.Errorf("position %q has invalid units %d", lot.ID, lot.RemainingUnits)
		}
		if lot.EntryPrice <= 0 {
			return fmt.Errorf("position %q has invalid entry price %d", lot.ID, lot.EntryPrice)
		}

		mark := lot.EntryPrice
		if marks != nil {
			if px, ok := marks[lot.Instrument]; ok {
				if px <= 0 {
					return fmt.Errorf("invalid mark for %s: %d", lot.Instrument, px)
				}
				mark = px
			}
		}

		inst := GetInstrument(lot.Instrument)
		if inst == nil {
			return fmt.Errorf("instrument is nil %s", lot.Instrument)
		}

		qta, err := act.QuoteToAccount(lot.Instrument, mark)
		if err != nil {
			return err
		}

		pnl, err := lotUnrealizedPNL(lot, mark, qta)
		if err != nil {
			return err
		}
		equity += pnl

		m, err := act.TradeMargin(lot.RemainingUnits, mark, lot.Instrument)
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
		v, err := signedMulDivRound(int64(act.Equity), int64(MoneyScale), int64(act.MarginUsed))
		if err != nil {
			return err
		}
		act.MarginLevel = Money(v)
	} else {
		act.MarginLevel = 0
	}

	return nil
}

func (act *Account) RealizePNL(lot *Lot, trade *Trade) (Money, error) {
	if act == nil {
		return 0, fmt.Errorf("nil account")
	}
	if lot == nil {
		return 0, fmt.Errorf("nil position")
	}
	if trade == nil {
		return 0, fmt.Errorf("nil trade")
	}
	if lot.Instrument == "" {
		return 0, fmt.Errorf("position instrument is empty")
	}
	if lot.Units <= 0 {
		return 0, fmt.Errorf("position units must be > 0")
	}
	if trade.ExitPrice <= 0 {
		return 0, fmt.Errorf("trade fill price must be > 0")
	}

	qta, err := act.QuoteToAccount(lot.Instrument, trade.ExitPrice)
	if err != nil {
		return 0, err
	}

	pnlMoney, err := lotUnrealizedPNL(lot, trade.ExitPrice, qta)
	if err != nil {
		return 0, err
	}

	act.Balance += pnlMoney
	act.Equity = act.Balance

	return pnlMoney, nil
}

func (act *Account) CloseLot(lot *Lot, trade *Trade) error {
	if act == nil {
		return fmt.Errorf("nil account")
	}
	if lot.Instrument == "" {
		return fmt.Errorf("position instrument is empty")
	}
	if trade.ExitPrice <= 0 {
		return fmt.Errorf("exit price must be > 0")
	}

	pnl, err := act.RealizePNL(lot, trade)
	if err != nil {
		return err
	}
	trade.PNL = pnl
	act.Trades = append(act.Trades, trade)
	act.Lots.Delete(lot.ID)
	return act.ResolveWithMarks(map[string]Price{lot.Instrument: trade.ExitPrice})
}

func (act *Account) TradeMargin(units Units, price Price, inst string) (Money, error) {
	meta := GetInstrument(inst)
	if meta == nil {
		return 0, fmt.Errorf("no such instrument %s\n", inst)
	}

	if meta.MarginRate <= 0 {
		return 0, fmt.Errorf("invalid margin rate for %s: %d", meta.Name, meta.MarginRate)
	}

	u := abs64(int64(units))
	p := int64(price)
	if p <= 0 {
		return 0, fmt.Errorf("invalid price: %d", p)
	}

	up, err := mulDiv64(u, p, int64(PriceScale))
	if err != nil {
		return 0, err
	}
	notionalQuoteMicro, err := mulDiv64(up, int64(MoneyScale), 1)
	if err != nil {
		return 0, err
	}

	qta, err := act.QuoteToAccount(meta.Name, price)
	if err != nil {
		return 0, err
	}

	notionalAcctMicro, err := mulDiv64(notionalQuoteMicro, int64(qta), int64(rateScale))
	if err != nil {
		return 0, err
	}

	marginMicro, err := mulDiv64(notionalAcctMicro, int64(meta.MarginRate), int64(rateScale))
	if err != nil {
		return 0, err
	}

	return Money(marginMicro), nil
}

// riskBudget returns the max allowed loss in account-money micro-units.
func (acct *Account) riskBudget() (Money, error) {
	if acct.Equity <= 0 {
		return 0, fmt.Errorf("account equity must be > 0")
	}
	if acct.RiskPct <= 0 {
		return 0, fmt.Errorf("account risk_pct must be > 0")
	}

	v, err := mulDivFloor64(int64(acct.Equity), int64(acct.RiskPct), int64(rateScale))
	if err != nil {
		return 0, err
	}
	if v <= 0 {
		return 0, fmt.Errorf("risk budget must be > 0")
	}
	return Money(v), nil
}

// lossPerUnit returns stop-loss exposure for 1 unit in account-money micro-units.
// It uses ceil so we never underestimate loss and accidentally oversize.
func (acct *Account) lossPerUnit(req *OpenRequest) (Money, error) {
	priceDist := abs64(int64(req.Price) - int64(req.TradeCommon.Stop))
	if priceDist == 0 {
		return 0, fmt.Errorf("entry and stop must differ")
	}

	qta, err := acct.QuoteToAccount(req.TradeCommon.Instrument, req.Price)
	if err != nil {
		return 0, err
	}

	v, err := mulDivCeil64(priceDist, int64(MoneyScale), int64(PriceScale))
	if err != nil {
		return 0, err
	}
	v, err = mulDivCeil64(v, int64(qta), int64(rateScale))
	if err != nil {
		return 0, err
	}
	if v <= 0 {
		return 0, fmt.Errorf("loss per unit must be > 0")
	}

	return Money(v), nil
}

// marginPerUnit returns margin needed for 1 unit in account-money micro-units.
// It uses ceil so we never underestimate required margin.
func (acct *Account) marginPerUnit(inst *Instrument, price Price) (Money, error) {
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

	v, err := mulDivCeil64(int64(price), int64(MoneyScale), int64(PriceScale))
	if err != nil {
		return 0, err
	}

	v, err = mulDivCeil64(v, int64(qta), int64(rateScale))
	if err != nil {
		return 0, err
	}

	v, err = mulDivCeil64(v, int64(inst.MarginRate), int64(rateScale))
	if err != nil {
		return 0, err
	}
	if v <= 0 {
		return 0, fmt.Errorf("margin per unit must be > 0")
	}

	return Money(v), nil
}

func (acct *Account) availableMargin() Money {
	if acct.FreeMargin > 0 {
		return acct.FreeMargin
	}

	fm := acct.Equity - acct.MarginUsed
	if fm > 0 {
		return fm
	}
	return 0
}

func (acct *Account) unitsByRisk(req *OpenRequest) (Units, error) {
	riskBudget, err := acct.riskBudget()
	if err != nil {
		return 0, err
	}

	lossPerUnit, err := acct.lossPerUnit(req)
	if err != nil {
		return 0, err
	}

	units := Units(int64(riskBudget) / int64(lossPerUnit))
	if units <= 0 {
		return 0, fmt.Errorf("risk budget too small for stop distance")
	}
	return units, nil
}

func (acct *Account) unitsByMargin(req *OpenRequest) (Units, error) {
	freeMargin := acct.availableMargin()
	if freeMargin <= 0 {
		return 0, fmt.Errorf("free margin must be > 0")
	}

	inst := GetInstrument(req.TradeCommon.Instrument)
	if inst == nil {
		return 0, fmt.Errorf("unknown instrument %s", req.TradeCommon.Instrument)
	}
	marginPerUnit, err := acct.marginPerUnit(inst, req.Price)
	if err != nil {
		return 0, err
	}

	units := Units(int64(freeMargin) / int64(marginPerUnit))
	if units <= 0 {
		return 0, fmt.Errorf("free margin too small for minimum position")
	}
	return units, nil
}

func minUnits(a, b Units) Units {
	if a < b {
		return a
	}
	return b
}

func (acct *Account) SizePosition(req *OpenRequest) error {
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
	case Short:
		if req.TradeCommon.Stop <= req.Price {
			return fmt.Errorf("short stop must be greater than price")
		}
	case Long:
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

	inst := GetInstrument(req.TradeCommon.Instrument)
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
