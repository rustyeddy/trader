package account

import (
	"fmt"
	"math"

	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/types"
)

type SizeRequest struct {
	Instrument string
	AccountCCY string

	Equity  types.Money
	RiskPct types.Rate

	Entry types.Price
	Stop  types.Price
}

type SizeResult struct {
	Units         types.Units
	StopPips      float64
	RiskAmount    types.Money
	LossPerUnit   types.Money
	EstimatedLoss types.Money
}

func PipSize(pipLocation int) float64 {
	return math.Pow10(pipLocation)
}

func SizePosition(req SizeRequest) (SizeResult, error) {
	if req.RiskPct <= 0 {
		return SizeResult{}, fmt.Errorf("risk_pct must be > 0")
	}
	if req.Entry <= 0 || req.Stop <= 0 {
		return SizeResult{}, fmt.Errorf("entry and stop must be > 0")
	}
	if req.Entry == req.Stop {
		return SizeResult{}, fmt.Errorf("entry and stop must differ")
	}

	meta, ok := market.Instruments[req.Instrument]
	if !ok {
		return SizeResult{}, fmt.Errorf("unknown instrument %q", req.Instrument)
	}

	entryF := float64(req.Entry) / float64(types.PriceScale)
	stopF := float64(req.Stop) / float64(types.PriceScale)

	quoteToAccount := 0.0
	switch {
	case meta.QuoteCurrency == req.AccountCCY:
		quoteToAccount = 1.0
	case meta.BaseCurrency == req.AccountCCY:
		if entryF <= 0 {
			return SizeResult{}, fmt.Errorf("entry must be > 0")
		}
		quoteToAccount = 1.0 / entryF
	default:
		return SizeResult{}, fmt.Errorf("cross-currency conversion not implemented for %s account %s", req.Instrument, req.AccountCCY)
	}

	pip := PipSize(meta.PipLocation)
	stopPips := math.Abs(entryF-stopF) / pip
	if stopPips <= 0 {
		return SizeResult{}, fmt.Errorf("stop distance must be > 0")
	}

	riskAmountF := req.Equity.Float64() * req.RiskPct.Float64()
	pipValuePerUnit := pip * quoteToAccount
	unitsF := riskAmountF / (stopPips * pipValuePerUnit)
	units := types.Units(math.Floor(unitsF))

	if units <= 0 {
		return SizeResult{}, fmt.Errorf("computed units <= 0")
	}

	lossPerUnitF := math.Abs(entryF-stopF) * quoteToAccount

	return SizeResult{
		Units:         units,
		StopPips:      stopPips,
		RiskAmount:    types.MoneyFromFloat(riskAmountF),
		LossPerUnit:   types.MoneyFromFloat(lossPerUnitF),
		EstimatedLoss: types.MoneyFromFloat(float64(units) * lossPerUnitF),
	}, nil
}

// func Calculate(req SizeRequest) SizeResult {
// 	res := SizeResult{}
// 	return res
// }

// func Calculate(in SizeRequest) SizeResult {
// 	pip := pipSize(in.PipLocation)
// 	stopPips := math.Abs(in.EntryPrice-in.StopPrice) / pip

// 	riskAmt := in.Equity * in.RiskPct
// 	pipValuePerUnit := pip * in.QuoteToAccount

// 	units := riskAmt / (stopPips * pipValuePerUnit)

// 	return SIzeResult{
// 		Units:      math.Floor(units),
// 		StopPips:   stopPips,
// 		RiskAmount: riskAmt,
// 	}
// }
