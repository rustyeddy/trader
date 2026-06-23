package execution

import (
	"fmt"

	"github.com/rustyeddy/trader/market"
)

// riskBudget returns the max allowed loss in account-money micro-units.
func (acct *Account) riskBudget() (market.Money, error) {
	if acct.Equity <= 0 {
		return 0, fmt.Errorf("account equity must be > 0")
	}
	if acct.RiskFraction <= 0 {
		return 0, fmt.Errorf("account risk fraction must be > 0")
	}

	v, err := market.MulDivFloor64(int64(acct.Equity), int64(acct.RiskFraction), int64(market.RateScale))
	if err != nil {
		return 0, err
	}
	if v <= 0 {
		return 0, fmt.Errorf("risk budget must be > 0")
	}
	return market.Money(v), nil
}

// lossPerUnit returns stop-loss exposure for 1 unit in account-money micro-units.
// It uses ceil so we never underestimate loss and accidentally oversize.
func (acct *Account) lossPerUnit(req *OpenRequest) (market.Money, error) {
	priceDist, err := market.AbsInt64Checked(int64(req.Price) - int64(req.TradeCommon.Stop))
	if err != nil {
		return 0, err
	}
	if priceDist == 0 {
		return 0, fmt.Errorf("entry and stop must differ")
	}

	quoteToAccountRate, err := acct.quoteToAccountRate(req.TradeCommon.Instrument, req.Price)
	if err != nil {
		return 0, err
	}

	v, err := market.MulDivCeil64(priceDist, int64(market.MoneyScale), int64(market.PriceScale))
	if err != nil {
		return 0, err
	}
	v, err = market.MulDivCeil64(v, int64(quoteToAccountRate), int64(market.RateScale))
	if err != nil {
		return 0, err
	}
	if v <= 0 {
		return 0, fmt.Errorf("loss per unit must be > 0")
	}

	return market.Money(v), nil
}

// marginRequiredPerUnit returns margin needed for 1 unit in account-money micro-units.
// It uses ceil so we never underestimate required margin.
func (acct *Account) marginRequiredPerUnit(inst *market.Instrument, price market.Price) (market.Money, error) {
	if inst == nil {
		return 0, fmt.Errorf("instrument metadata is nil")
	}
	if inst.MarginRate <= 0 {
		return 0, fmt.Errorf("invalid margin rate for %s: %d", inst.Name, inst.MarginRate)
	}
	if price <= 0 {
		return 0, fmt.Errorf("invalid price %d", price)
	}

	quoteToAccountRate, err := acct.quoteToAccountRate(inst.Name, price)
	if err != nil {
		return 0, err
	}

	v, err := market.MulDivCeil64(int64(price), int64(market.MoneyScale), int64(market.PriceScale))
	if err != nil {
		return 0, err
	}

	v, err = market.MulDivCeil64(v, int64(quoteToAccountRate), int64(market.RateScale))
	if err != nil {
		return 0, err
	}

	v, err = market.MulDivCeil64(v, int64(inst.MarginRate), int64(market.RateScale))
	if err != nil {
		return 0, err
	}
	if v <= 0 {
		return 0, fmt.Errorf("margin per unit must be > 0")
	}

	return market.Money(v), nil
}

// availableMargin returns the usable margin for new positions.
// It prefers the cached FreeMargin but falls back to computing Equity − MarginUsed
// in case the field is stale.
func (acct *Account) availableMargin() market.Money {
	if acct.FreeMargin > 0 {
		return acct.FreeMargin
	}

	fm := acct.Equity - acct.MarginUsed
	if fm > 0 {
		return fm
	}
	return 0
}

// unitsByRisk returns how many units can be opened without exceeding the
// account's per-trade risk budget (RiskFraction × Equity).
func (acct *Account) unitsByRisk(req *OpenRequest) (market.Units, error) {
	riskBudget, err := acct.riskBudget()
	if err != nil {
		return 0, err
	}

	lossPerUnit, err := acct.lossPerUnit(req)
	if err != nil {
		return 0, err
	}

	units := market.Units(int64(riskBudget) / int64(lossPerUnit))
	if units <= 0 {
		return 0, fmt.Errorf("risk budget too small for stop distance")
	}
	return units, nil
}

// unitsByMargin returns how many units can be opened given the account's
// current free margin.
func (acct *Account) unitsByMargin(req *OpenRequest) (market.Units, error) {
	freeMargin := acct.availableMargin()
	if freeMargin <= 0 {
		return 0, fmt.Errorf("free margin must be > 0")
	}

	inst := market.GetInstrument(req.TradeCommon.Instrument)
	if inst == nil {
		return 0, fmt.Errorf("unknown instrument: %s", req.TradeCommon.Instrument)
	}
	marginPerUnit, err := acct.marginRequiredPerUnit(inst, req.Price)
	if err != nil {
		return 0, err
	}

	units := market.Units(int64(freeMargin) / int64(marginPerUnit))
	if units <= 0 {
		return 0, fmt.Errorf("free margin too small for minimum position")
	}
	return units, nil
}

// SizePosition computes and sets req.Units as the lesser of:
//   - the units allowed by the risk budget (unitsByRisk)
//   - the units allowed by available margin (unitsByMargin)
//
// Returns an error if the computed size is below the instrument's minimum
// trade size or if any input is invalid.
func (acct *Account) SizePosition(req *OpenRequest) error {
	if acct == nil {
		return fmt.Errorf("account is nil")
	}
	if req == nil {
		return fmt.Errorf("request is nil")
	}
	if req.Instrument == "" {
		return fmt.Errorf("request instrument must not be empty")
	}
	if req.Price <= 0 || req.Stop <= 0 {
		return fmt.Errorf("entry and stop must be > 0")
	}
	if req.Price == req.Stop {
		return fmt.Errorf("entry and stop must differ")
	}

	switch req.Side {
	case market.Short:
		if req.TradeCommon.Stop <= req.Price {
			return fmt.Errorf("short stop must be greater than price")
		}
	case market.Long:
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

	inst := market.GetInstrument(req.TradeCommon.Instrument)
	if inst == nil {
		return fmt.Errorf("unknown instrument: %s", req.TradeCommon.Instrument)
	}

	units := unitsMargin
	if unitsRisk < unitsMargin {
		units = unitsRisk
	}
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
