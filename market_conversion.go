package trader

import (
	"context"
	"fmt"
)

func QuoteToAccountRate(instrument string, accountCurrency string, prices TickSource) (float64, error) {
	meta, ok := Instruments[instrument]
	if !ok {
		return 0, fmt.Errorf("unknown instrument %s", instrument)
	}

	if meta.QuoteCurrency == accountCurrency {
		return 1.0, nil
	}

	if meta.BaseCurrency == accountCurrency {
		px, err := prices.GetTick(context.TODO(), instrument)
		if err != nil {
			return 0, err
		}
		mid := float64(px.Mid()) / float64(PriceScale)
		if mid <= 0 {
			return 0, fmt.Errorf("invalid mid price for %s", instrument)
		}
		return 1.0 / mid, nil
	}

	return 0, fmt.Errorf("cross conversion not implemented for %s -> %s", meta.QuoteCurrency, accountCurrency)
}
