package portfolio

import (
	"fmt"
	"time"

	"github.com/rustyeddy/trader/num"
)

// ConversionRate is one exchange rate, with provenance, that
// NewPortfolio may use to convert an account's Equity into a
// Portfolio's BaseCurrency. num.Rate alone cannot answer "where did
// this rate come from and when was it observed," which a report
// explaining a converted total needs.
type ConversionRate struct {
	// From is the currency this rate converts out of.
	From num.Currency
	// To is the currency this rate converts into. NewPortfolio rejects
	// any ConversionRate whose To does not equal the Portfolio's
	// BaseCurrency: a rate targeting some other currency cannot be used
	// here, and accepting it silently would invite confusing a
	// same-shaped but wrongly directed rate for a usable one.
	To num.Currency
	// Rate converts one unit of From into To: amountInTo =
	// amountInFrom * Rate. Must be strictly positive; a real exchange
	// rate is never zero or negative.
	Rate num.Rate
	// AsOf is when this rate was observed.
	AsOf time.Time
	// Source names where this rate came from, for example
	// "oanda.rates" or "ecb.reference". May be empty.
	Source string
}

// checkConversionRate validates r against base, the Portfolio's
// BaseCurrency.
func checkConversionRate(r ConversionRate, base num.Currency) error {
	if !r.From.IsValid() {
		return fmt.Errorf("conversion rate: from currency must be valid")
	}
	if !r.To.IsValid() {
		return fmt.Errorf("conversion rate: to currency must be valid")
	}
	if !r.To.Equal(base) {
		return fmt.Errorf("conversion rate: to currency %s does not match base currency %s", r.To, base)
	}
	if r.From.Equal(base) {
		return fmt.Errorf("conversion rate: from currency %s must not equal base currency", r.From)
	}
	if r.Rate.Sign() <= 0 {
		return fmt.Errorf("conversion rate: rate must be strictly positive")
	}
	if r.AsOf.IsZero() {
		return fmt.Errorf("conversion rate: as-of time must be set")
	}
	return nil
}
