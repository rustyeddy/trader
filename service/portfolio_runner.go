package service

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/rustyeddy/trader"
)

// PortfolioRunConfig controls a multi-instrument live portfolio run.
type PortfolioRunConfig struct {
	Instruments []InstrumentRunConfig

	// DrawdownCircuitPct halts all new opens when account equity drops this
	// percentage below its peak. 0 disables the circuit breaker.
	DrawdownCircuitPct float64

	Log *slog.Logger
}

// InstrumentRunConfig is one instrument+strategy entry in the portfolio.
type InstrumentRunConfig struct {
	Instrument  string        // OANDA format, e.g. "USD_CHF"
	Granularity string        // "H1" or "D"
	TickInterval time.Duration // how often to poll; defaults to half the bar period
	Strategy    trader.LiveStrategy
	RiskPct     float64
	MaxUnits    int64
}

// RunPortfolio runs all instruments concurrently until ctx is cancelled.
// Each instrument runs in its own goroutine via RunLiveStrategy.
// A shared drawdown circuit breaker wraps every open request.
func (s *Service) RunPortfolio(ctx context.Context, cfg PortfolioRunConfig) error {
	if len(cfg.Instruments) == 0 {
		return fmt.Errorf("portfolio: no instruments configured")
	}
	if s.OANDA == nil {
		return fmt.Errorf("portfolio: OANDA client not configured")
	}
	if err := s.ResolveAccount(ctx); err != nil {
		return fmt.Errorf("portfolio: %w", err)
	}

	log := cfg.Log
	if log == nil {
		log = slog.Default()
	}

	cb := &drawdownCircuitBreaker{
		svc:      s,
		limitPct: cfg.DrawdownCircuitPct,
		log:      log,
	}

	log.Info("portfolio: starting",
		"instruments", len(cfg.Instruments),
		"account", s.AccountID,
		"drawdown_limit_pct", cfg.DrawdownCircuitPct,
	)

	var wg sync.WaitGroup
	errs := make(chan error, len(cfg.Instruments))

	for _, inst := range cfg.Instruments {
		inst := inst // capture
		tick := inst.TickInterval
		if tick <= 0 {
			tick = defaultTickInterval(inst.Granularity)
		}
		wg.Add(1)
		go func(inst InstrumentRunConfig) {
			defer wg.Done()
			strategy := &circuitBreakerStrategy{inner: inst.Strategy, cb: cb}
			err := s.RunLiveStrategy(ctx, LiveRunConfig{
				Instrument:   inst.Instrument,
				TickInterval: tick,
				Strategy:     strategy,
				RiskPct:      inst.RiskPct,
				MaxUnits:     inst.MaxUnits,
			})
			if err != nil && ctx.Err() == nil {
				errs <- fmt.Errorf("%s: %w", inst.Instrument, err)
			}
		}(inst)
	}

	wg.Wait()
	close(errs)

	// Collect any non-context errors.
	var first error
	for e := range errs {
		if first == nil {
			first = e
		}
		log.Error("portfolio: instrument error", "err", e)
	}
	return first
}

// defaultTickInterval returns a sensible poll interval for a given granularity.
// For D1 we poll hourly (bar closes once a day); for H1 every 5 minutes.
func defaultTickInterval(granularity string) time.Duration {
	switch granularity {
	case "D":
		return 60 * time.Minute
	case "H4":
		return 15 * time.Minute
	default: // H1
		return 5 * time.Minute
	}
}

// ── drawdown circuit breaker ─────────────────────────────────────────────────

// drawdownCircuitBreaker tracks account equity peak and blocks opens when
// equity has fallen more than limitPct percent below that peak.
type drawdownCircuitBreaker struct {
	mu       sync.Mutex
	peakNAV  float64
	svc      *Service
	limitPct float64
	log      *slog.Logger
}

// allowOpen returns true when the circuit breaker permits a new open.
func (cb *drawdownCircuitBreaker) allowOpen(ctx context.Context) bool {
	if cb.limitPct <= 0 {
		return true
	}
	nav, err := cb.svc.accountNAV(ctx)
	if err != nil {
		cb.log.Warn("circuit breaker: could not fetch NAV", "err", err)
		return true // fail open — don't block on transient errors
	}
	cb.mu.Lock()
	defer cb.mu.Unlock()
	if nav > cb.peakNAV {
		cb.peakNAV = nav
	}
	if cb.peakNAV <= 0 {
		return true
	}
	dd := (cb.peakNAV - nav) / cb.peakNAV * 100
	if dd >= cb.limitPct {
		cb.log.Warn("circuit breaker: halting new opens",
			"nav", nav,
			"peak", cb.peakNAV,
			"drawdown_pct", dd,
			"limit_pct", cb.limitPct,
		)
		return false
	}
	return true
}

// accountNAV fetches the current account NAV (equity) from OANDA.
func (s *Service) accountNAV(ctx context.Context) (float64, error) {
	summary, err := s.OANDA.GetAccountSummary(ctx, s.AccountID)
	if err != nil {
		return 0, err
	}
	return summary.NAV, nil
}

// ── circuit breaker strategy wrapper ─────────────────────────────────────────

// circuitBreakerStrategy wraps a LiveStrategy and suppresses opens when the
// drawdown circuit breaker is tripped.
type circuitBreakerStrategy struct {
	inner trader.LiveStrategy
	cb    *drawdownCircuitBreaker
}

func (s *circuitBreakerStrategy) Name() string { return s.inner.Name() }

func (s *circuitBreakerStrategy) Tick(ctx context.Context, price trader.LivePrice, trades []trader.LiveTrade) *trader.LivePlan {
	plan := s.inner.Tick(ctx, price, trades)
	if plan == nil {
		return nil
	}
	// Strip open request if circuit breaker is tripped.
	if plan.Open != nil && !s.cb.allowOpen(ctx) {
		plan.Open = nil
		plan.Reason = "circuit-breaker: drawdown limit reached"
	}
	return plan
}
