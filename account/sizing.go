package account

import (
	"fmt"
	"math"

	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/types"
)

type SizeRequest struct {
	Instrument string
	Entry      types.Price
	Stop       types.Price

	// Conversion rate from quote currency to account currency.
	// For EURUSD in a USD account, this is 1.0.
	// For USDJPY in a USD account, this is 1 / USDJPY.
	// For EURGBP in a USD account, this is GBPUSD (or 1 / USDGBP).
	QuoteToAccount types.Rate
}

type SizeResult struct {
	Units          types.Units
	StopPips       float64
	RiskAmount     types.Money
	LossPerUnit    types.Money
	EstimatedLoss  types.Money
	QuoteToAccount types.Rate
	RequiredMargin types.Money
}

func (acct *Account) SizePosition(req SizeRequest) (SizeResult, error) {
	if acct.RiskPct <= 0 {
		return SizeResult{}, fmt.Errorf("account risk_pct must be > 0")
	}
	if acct.Equity <= 0 {
		return SizeResult{}, fmt.Errorf("account equity must be > 0")
	}
	if req.Entry <= 0 || req.Stop <= 0 {
		return SizeResult{}, fmt.Errorf("entry and stop must be > 0")
	}
	if req.Entry == req.Stop {
		return SizeResult{}, fmt.Errorf("entry and stop must differ")
	}
	if req.QuoteToAccount <= 0 {
		return SizeResult{}, fmt.Errorf("quote_to_account must be > 0")
	}

	meta, ok := market.Instruments[req.Instrument]
	if !ok {
		return SizeResult{}, fmt.Errorf("unknown instrument %q", req.Instrument)
	}

	entryF := float64(req.Entry) / float64(types.PriceScale)
	stopF := float64(req.Stop) / float64(types.PriceScale)
	qta := req.QuoteToAccount.Float64()

	pip := meta.PipSize()
	stopPips := math.Abs(entryF-stopF) / pip
	if stopPips <= 0 {
		return SizeResult{}, fmt.Errorf("stop distance must be > 0")
	}

	riskAmountF := acct.Equity.Float64() * acct.RiskPct.Float64()
	pipValuePerUnit := pip * qta
	if pipValuePerUnit <= 0 {
		return SizeResult{}, fmt.Errorf("pip value per unit must be > 0")
	}

	unitsF := riskAmountF / (stopPips * pipValuePerUnit)
	units := types.Units(math.Floor(unitsF))
	if units <= meta.MinimumTradeSize {
		return SizeResult{}, fmt.Errorf("computed units <= 0")
	}

	lossPerUnitF := math.Abs(entryF-stopF) * qta

	return SizeResult{
		Units:         units,
		StopPips:      stopPips,
		RiskAmount:    types.MoneyFromFloat(riskAmountF),
		LossPerUnit:   types.MoneyFromFloat(lossPerUnitF),
		EstimatedLoss: types.MoneyFromFloat(float64(units) * lossPerUnitF),
	}, nil
}
