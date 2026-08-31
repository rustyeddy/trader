package backtest

import (
	"errors"
	"fmt"

	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/strategy"
)

// ErrInvalidRequest marks a RunRequest that is missing a service-level
// required field. Validate checks only what this service layer itself
// must have to invoke the use case — it does not duplicate
// backtest.RunnerParams' own validation, which still runs when Run
// assembles RunnerParams and calls backtest.NewRunner.
var ErrInvalidRequest = errors.New("service/backtest: invalid request")

// RunRequest is the request for the Run use case: replay Strategy over
// Span, starting from StartingCapital, sized per RiskFraction/
// AdverseDistance.
type RunRequest struct {
	// Strategy is this one run's strategy, freshly constructed by the
	// caller — strategy state must not be reused across runs, the same
	// requirement backtest.RunnerParams.Strategy already documents.
	Strategy strategy.Strategy
	// StrategyParameters is canonically marshaled into the resulting
	// Manifest. Pass nil for a strategy with no parameters.
	StrategyParameters any

	// Span is the half-open time range this run replays.
	Span marketdata.TimeRange
	// StartingCapital is this run's starting account balance, also
	// passed to EnvironmentFactory.NewEnvironment so the Account it
	// opens can be funded accordingly.
	StartingCapital num.Money
	// RiskFraction and AdverseDistance are this run's fixed position-
	// sizing policy.
	RiskFraction    num.Rate
	AdverseDistance num.Price

	// TraderVersion is an optional caller-supplied build/version
	// string, recorded in the resulting Manifest.
	TraderVersion string
}

// Validate reports whether r is well-formed enough to attempt,
// returning a wrapped ErrInvalidRequest for the first problem found.
func (r RunRequest) Validate() error {
	if r.Strategy == nil {
		return fmt.Errorf("%w: strategy must be set", ErrInvalidRequest)
	}
	if r.Span.Duration() <= 0 {
		return fmt.Errorf("%w: span must be set", ErrInvalidRequest)
	}
	if !r.StartingCapital.IsValid() {
		return fmt.Errorf("%w: starting capital must be valid", ErrInvalidRequest)
	}
	zero, err := num.ParseMoney("0", r.StartingCapital.Currency())
	if err != nil {
		return fmt.Errorf("%w: starting capital currency: %v", ErrInvalidRequest, err)
	}
	if sign, err := r.StartingCapital.Cmp(zero); err != nil || sign <= 0 {
		return fmt.Errorf("%w: starting capital must be positive", ErrInvalidRequest)
	}
	return nil
}
