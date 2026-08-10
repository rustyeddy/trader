package tradertest

import (
	"time"

	"github.com/rustyeddy/trader/account"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/portfolio"
)

// PortfolioParams builds a portfolio.Portfolio. Accounts is required
// and must be non-empty; BaseCurrency and AsOf default to "USD" and
// DefaultAsOf(). Rates is optional, matching portfolio.NewPortfolio's
// own contract — supply it when Accounts spans more than one currency.
type PortfolioParams struct {
	BaseCurrency string
	AsOf         time.Time
	Accounts     []account.Snapshot
	Rates        []portfolio.ConversionRate
}

// NewPortfolio returns a valid portfolio.Portfolio built from p,
// filling in defaults for zero-valued fields.
func NewPortfolio(p PortfolioParams) (portfolio.Portfolio, error) {
	if p.BaseCurrency == "" {
		p.BaseCurrency = "USD"
	}
	if p.AsOf.IsZero() {
		p.AsOf = defaultAsOf
	}

	base, err := num.ParseCurrency(p.BaseCurrency)
	if err != nil {
		return portfolio.Portfolio{}, err
	}

	return portfolio.NewPortfolio(portfolio.PortfolioParams{
		BaseCurrency: base,
		AsOf:         p.AsOf,
		Accounts:     p.Accounts,
		Rates:        p.Rates,
	})
}

// MustNewPortfolio is like NewPortfolio but panics on error.
func MustNewPortfolio(p PortfolioParams) portfolio.Portfolio {
	pf, err := NewPortfolio(p)
	if err != nil {
		panic(err)
	}
	return pf
}
