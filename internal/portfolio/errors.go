package portfolio

import "errors"

// ErrInvalidPortfolio reports a PortfolioParams value that fails
// NewPortfolio's validation.
var ErrInvalidPortfolio = errors.New("portfolio: invalid portfolio")
