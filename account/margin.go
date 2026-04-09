package account

import (
	"fmt"

	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/types"
)

func TradeMargin(units types.Units, price types.Price, instrument string, quoteToAccount types.Rate) (types.Money, error) {
	meta, ok := market.Instruments[instrument]
	if !ok {
		return 0, fmt.Errorf("unknown instrument %s", instrument)
	}
	if quoteToAccount <= 0 {
		return 0, fmt.Errorf("invalid quoteToAccount rate: %d", quoteToAccount)
	}
	if meta.MarginRate <= 0 {
		return 0, fmt.Errorf("invalid margin rate for %s: %d", instrument, meta.MarginRate)
	}

	u := types.Abs64(int64(units))
	p := int64(price)
	if p <= 0 {
		return 0, fmt.Errorf("invalid price: %d", p)
	}

	ms := int64(types.MoneyScale)

	// Step 1: notionalQuoteMicro = u * p * MoneyScale / PriceScale
	// Do it as: (u*p)/PriceScale first, then *MoneyScale (more stable),
	// but we need micro units, so we compute in one MulDiv:
	up, err := types.MulDiv64(u, p, ms) // quote units (unscaled)
	if err != nil {
		return 0, err
	}
	// quote micro units:
	notionalQuoteMicro, err := types.MulDiv64(up, ms, 1)
	if err != nil {
		return 0, err
	}

	// Step 2: convert to account micro units: *quoteToAccount / RateScale
	notionalAcctMicro, err := types.MulDiv64(notionalQuoteMicro, int64(quoteToAccount), ms)
	if err != nil {
		return 0, err
	}

	// Step 3: margin = notionalAcctMicro * marginRate / RateScale
	marginMicro, err := types.MulDiv64(notionalAcctMicro, int64(meta.MarginRate), ms)
	if err != nil {
		return 0, err
	}

	return types.Money(marginMicro), nil
}
