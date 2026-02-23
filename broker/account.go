package broker

import (
	"context"
	"fmt"

	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/types"
)

type Account struct {
	ID          string
	Currency    string
	Balance     types.Money
	Equity      types.Money
	MarginUsed  types.Money
	FreeMargin  types.Money
	MarginLevel types.Money
}

func (act *Account) QuoteToRate(ctx context.Context, instrument string, prices market.TickSource) (types.Rate, error) {
	meta, ok := market.Instruments[instrument]
	if !ok {
		return 0, fmt.Errorf("unknown instrument %s", instrument)
	}

	// Case 1: quote currency == account currency (EUR_USD, GBP_USD, etc.)
	if meta.QuoteCurrency == act.Currency {
		return types.Rate(types.RateScale), nil // 1.000000
	}

	// Case 2: base currency == account currency (USD_JPY, USD_CHF, etc.)
	if meta.BaseCurrency == act.Currency {
		px, err := prices.GetTick(ctx, instrument)
		if err != nil {
			return 0, err
		}

		midScaled := int64(px.Mid()) // quote per base * PriceScale
		if midScaled <= 0 {
			return 0, fmt.Errorf("invalid mid price for %s: %d", instrument, midScaled)
		}

		// rateScaled = RateScale * PriceScale / midScaled
		r, err := types.MulDiv64(types.RateScale, int64(types.PriceScale), midScaled)
		if err != nil {
			return 0, err
		}
		return types.Rate(r), nil
	}

	// Case 3: Cross currency (future-proofing)
	return 0, fmt.Errorf("cross conversion not implemented for %s → %s", meta.QuoteCurrency, act.Currency)
}
