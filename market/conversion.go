package market

import (
	"context"
	"fmt"
)

func QuoteToAccountRate(instrument string,
	accountCurrency string,
	prices TickSource) (float64, error) {

	meta, ok := Instruments[instrument]
	if !ok {
		return 0, fmt.Errorf("unknown instrument %s", instrument)
	}

	// Case 1: quote currency == account currency (EUR_USD, GBP_USD, etc.)
	if meta.QuoteCurrency == accountCurrency {
		return 1.0, nil
	}

	// Case 2: USD is base (USD_JPY, USD_CHF, etc.)
	if meta.BaseCurrency == accountCurrency {
		px, err := prices.GetTick(context.Background(), instrument)
		if err != nil {
			return 0, err
		}
		// USD_JPY mid gives JPY per USD
		// We want USD per JPY
		return 1.0 / px.Mid(), nil
	}

	// Case 3: Cross currency (future-proofing)
	// Example: EUR_GBP with USD account
	return 0, fmt.Errorf(
		"cross conversion not implemented for %s → %s",
		meta.QuoteCurrency,
		accountCurrency,
	)
}
