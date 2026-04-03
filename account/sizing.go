package account

import (
	"fmt"
	"math"

	"github.com/rustyeddy/trader/portfolio"
	"github.com/rustyeddy/trader/types"
)

func (acct *Account) SizePosition(req *portfolio.OpenRequest) error {
	if acct == nil {
		return fmt.Errorf("account is nil")
	}
	if req == nil {
		return fmt.Errorf("request is nil")
	}
	if acct.RiskPct <= 0 {
		return fmt.Errorf("account risk_pct must be > 0")
	}
	if acct.Equity <= 0 {
		return fmt.Errorf("account equity must be > 0")
	}
	if req.Price <= 0 || req.Stop <= 0 {
		return fmt.Errorf("entry and stop must be > 0")
	}
	if req.Price == req.Stop {
		return fmt.Errorf("entry and stop must differ")
	}
	if req.Instrument == nil {
		return fmt.Errorf("req.Instrument is nil")
	}

	switch req.Side {
	case types.Short:
		if req.Stop <= req.Price {
			return fmt.Errorf("short stop must be less than price")
		}

	case types.Long:
		if req.Stop >= req.Price {
			return fmt.Errorf("long stop must be greater than price")
		}

	default:
		return fmt.Errorf("invalid side %v", req.Side)
	}

	qta, err := acct.QuoteToAccount(req.Instrument, req.Price)
	if err != nil {
		return err
	}

	entryF := float64(req.Price) / float64(types.PriceScale)
	stopF := float64(req.Stop) / float64(types.PriceScale)

	// Risk budget in account currency, e.g. equity * 0.5%.
	riskBudget := acct.Equity.Float64() * acct.RiskPct.Float64()
	if riskBudget <= 0 {
		return fmt.Errorf("risk budget must be > 0")
	}

	// Loss per 1 unit in quote currency, then convert to account currency.
	priceDist := math.Abs(entryF - stopF)
	lossPerUnitAcct := priceDist * qta.Float64()
	if lossPerUnitAcct <= 0 {
		return fmt.Errorf("loss per unit must be > 0")
	}

	unitsF := riskBudget / lossPerUnitAcct
	units := types.Units(math.Floor(unitsF))
	if units < req.Instrument.MinimumTradeSize {
		return fmt.Errorf("computed units < minimum trade size")
	}

	req.Units = units
	margin, err := acct.TradeMargin(units, req.Price, req.Instrument)
	if err != nil {
		return err
	}
	if acct.FreeMargin > 0 && margin > acct.FreeMargin {
		return fmt.Errorf("required margin %v exceeds free margin %v", margin, acct.FreeMargin)
	}

	return nil
}
