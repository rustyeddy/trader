package account

import (
	"fmt"

	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/types"
)

// SizingInputs carries the scalars position-sizing math actually needs.
// It lets sizing run against any equity/margin source (a backtest Account's
// live fields, or a live account snapshot) without requiring a full ledger.
type SizingInputs struct {
	Equity       types.Money
	MarginUsed   types.Money
	FreeMargin   types.Money
	RiskFraction types.Rate
	Currency     string
}

// sizingInputs snapshots the five scalars SizePosition needs from acct.
func (acct *Account) sizingInputs() SizingInputs {
	return SizingInputs{
		Equity:       acct.Equity,
		MarginUsed:   acct.MarginUsed,
		FreeMargin:   acct.FreeMargin,
		RiskFraction: acct.RiskFraction,
		Currency:     acct.Currency,
	}
}

// riskBudget returns the max allowed loss in account-money micro-units.
func (in SizingInputs) riskBudget() (types.Money, error) {
	if in.Equity <= 0 {
		return 0, fmt.Errorf("account equity must be > 0")
	}
	if in.RiskFraction <= 0 {
		return 0, fmt.Errorf("account risk fraction must be > 0")
	}

	v, err := types.MulDivFloor64(int64(in.Equity), int64(in.RiskFraction), int64(types.RateScale))
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
func (in SizingInputs) lossPerUnit(req *OpenRequest) (types.Money, error) {
	priceDist, err := types.AbsInt64Checked(int64(req.Price) - int64(req.TradeCommon.Stop))
	if err != nil {
		return 0, err
	}
	if priceDist == 0 {
		return 0, fmt.Errorf("entry and stop must differ")
	}

	quoteToAccountRate, err := quoteToAccountRateFor(in.Currency, req.TradeCommon.Instrument, req.Price)
	if err != nil {
		return 0, err
	}

	v, err := types.MulDivCeil64(priceDist, int64(types.MoneyScale), int64(types.PriceScale))
	if err != nil {
		return 0, err
	}
	v, err = types.MulDivCeil64(v, int64(quoteToAccountRate), int64(types.RateScale))
	if err != nil {
		return 0, err
	}
	if v <= 0 {
		return 0, fmt.Errorf("loss per unit must be > 0")
	}

	return types.Money(v), nil
}

// marginRequiredPerUnit returns margin needed for 1 unit in account-money micro-units.
// It uses ceil so we never underestimate required margin.
func (in SizingInputs) marginRequiredPerUnit(inst *market.Instrument, price types.Price) (types.Money, error) {
	if inst == nil {
		return 0, fmt.Errorf("instrument metadata is nil")
	}
	if inst.MarginRate <= 0 {
		return 0, fmt.Errorf("invalid margin rate for %s: %d", inst.Name, inst.MarginRate)
	}
	if price <= 0 {
		return 0, fmt.Errorf("invalid price %d", price)
	}

	quoteToAccountRate, err := quoteToAccountRateFor(in.Currency, inst.Name, price)
	if err != nil {
		return 0, err
	}

	v, err := types.MulDivCeil64(int64(price), int64(types.MoneyScale), int64(types.PriceScale))
	if err != nil {
		return 0, err
	}

	v, err = types.MulDivCeil64(v, int64(quoteToAccountRate), int64(types.RateScale))
	if err != nil {
		return 0, err
	}

	v, err = types.MulDivCeil64(v, int64(inst.MarginRate), int64(types.RateScale))
	if err != nil {
		return 0, err
	}
	if v <= 0 {
		return 0, fmt.Errorf("margin per unit must be > 0")
	}

	return types.Money(v), nil
}

// availableMargin returns the usable margin for new positions.
// It prefers the cached FreeMargin but falls back to computing Equity − MarginUsed
// in case the field is stale.
func (in SizingInputs) availableMargin() types.Money {
	if in.FreeMargin > 0 {
		return in.FreeMargin
	}

	fm := in.Equity - in.MarginUsed
	if fm > 0 {
		return fm
	}
	return 0
}

// unitsByRisk returns how many units can be opened without exceeding the
// account's per-trade risk budget (RiskFraction × Equity).
func (in SizingInputs) unitsByRisk(req *OpenRequest) (types.Units, error) {
	riskBudget, err := in.riskBudget()
	if err != nil {
		return 0, err
	}

	lossPerUnit, err := in.lossPerUnit(req)
	if err != nil {
		return 0, err
	}

	units := types.Units(int64(riskBudget) / int64(lossPerUnit))
	if units <= 0 {
		return 0, fmt.Errorf("risk budget too small for stop distance")
	}
	return units, nil
}

// unitsByMargin returns how many units can be opened given the account's
// current free margin.
func (in SizingInputs) unitsByMargin(req *OpenRequest) (types.Units, error) {
	freeMargin := in.availableMargin()
	if freeMargin <= 0 {
		return 0, fmt.Errorf("free margin must be > 0")
	}

	inst := market.GetInstrument(req.TradeCommon.Instrument)
	if inst == nil {
		return 0, fmt.Errorf("unknown instrument: %s", req.TradeCommon.Instrument)
	}
	marginPerUnit, err := in.marginRequiredPerUnit(inst, req.Price)
	if err != nil {
		return 0, err
	}

	units := types.Units(int64(freeMargin) / int64(marginPerUnit))
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
func SizePosition(in SizingInputs, req *OpenRequest) error {
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

	unitsRisk, err := in.unitsByRisk(req)
	if err != nil {
		return err
	}

	unitsMargin, err := in.unitsByMargin(req)
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

// SizePosition computes and sets req.Units using the account's own
// Equity/MarginUsed/FreeMargin/RiskFraction/Currency. See the package-level
// SizePosition for the underlying math.
func (acct *Account) SizePosition(req *OpenRequest) error {
	if acct == nil {
		return fmt.Errorf("account is nil")
	}
	return SizePosition(acct.sizingInputs(), req)
}
