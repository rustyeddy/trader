package backtest

import (
	"context"
	"errors"
	"fmt"

	"github.com/rustyeddy/trader/backtest"
	"github.com/rustyeddy/trader/broker"
	"github.com/rustyeddy/trader/clock"
	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/journal"
	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/pipeline"
)

// ErrInvalidEnvironment marks an Environment an EnvironmentFactory
// returned that is missing a required field — a composition-seam
// failure, reported here rather than left for backtest.NewRunner to
// surface less locally (issue #221 review).
var ErrInvalidEnvironment = errors.New("service/backtest: invalid environment")

// EnvironmentRequest describes what one run's Environment must be
// built for — everything an EnvironmentFactory needs to size the
// account it opens and validate the span it will replay, and nothing
// it does not (the Strategy itself is orthogonal to broker/pipeline
// wiring and stays on RunRequest, never here).
type EnvironmentRequest struct {
	StartingCapital num.Money
	Span            marketdata.TimeRange
}

// Environment is one run's complete, mutually consistent, freshly
// constructed set of stateful dependencies (issue #221 review): Clock,
// IDs, Account, and Pipeline must all originate from the same
// construction — Pipeline verifies its own injected broker.Broker.
// Name() matches Account.Broker() before any mutation (ADR-033), so an
// Account/Pipeline pair built from two different underlying brokers is
// already rejected one layer down, but Service validates the simpler
// "every field is actually set" precondition itself (see Validate)
// before ever reaching backtest.NewRunner.
//
// RiskRules, FillModel, SlippageModel, and CommissionModel must
// describe the actual Pipeline/Account this Environment returns — the
// same requirement backtest.RunnerParams itself documents — since
// these become the resulting Manifest's own reproducibility record.
//
// Journal is this one run's own durable record destination (issue
// #218, M5-10). It is run-scoped, not a Service-level singleton: a
// real journal adapter normally has a run-specific destination and
// sequence, and Runner never closes an externally supplied Journal, so
// whatever constructs Environment (the EnvironmentFactory or its own
// caller) owns Journal's lifecycle, exactly as it owns Account's and
// Pipeline's. A nil Journal is accepted and treated as
// journal.Discard(), matching Runner's own convention.
type Environment struct {
	Clock    *clock.Simulated
	IDs      *id.Generator
	Account  broker.Account
	Pipeline *pipeline.Pipeline

	RiskRules       []backtest.ComponentInfo
	FillModel       backtest.ComponentInfo
	SlippageModel   backtest.ComponentInfo
	CommissionModel backtest.ComponentInfo

	Journal journal.Recorder
}

// validate reports whether env has every field backtest.RunnerParams
// requires, returning a wrapped ErrInvalidEnvironment for the first
// problem found.
func (env Environment) validate() error {
	if env.Clock == nil {
		return fmt.Errorf("%w: clock must be set", ErrInvalidEnvironment)
	}
	if env.IDs == nil {
		return fmt.Errorf("%w: ids must be set", ErrInvalidEnvironment)
	}
	if env.Account == nil {
		return fmt.Errorf("%w: account must be set", ErrInvalidEnvironment)
	}
	if _, ok := env.Account.(backtest.MarketObserver); !ok {
		return fmt.Errorf("%w: account must implement backtest.MarketObserver for a mark-to-market equity curve", ErrInvalidEnvironment)
	}
	if env.Pipeline == nil {
		return fmt.Errorf("%w: pipeline must be set", ErrInvalidEnvironment)
	}
	if env.FillModel.Name() == "" {
		return fmt.Errorf("%w: fill model must be set", ErrInvalidEnvironment)
	}
	if env.SlippageModel.Name() == "" {
		return fmt.Errorf("%w: slippage model must be set", ErrInvalidEnvironment)
	}
	if env.CommissionModel.Name() == "" {
		return fmt.Errorf("%w: commission model must be set", ErrInvalidEnvironment)
	}
	return nil
}

// EnvironmentFactory builds a fresh Environment for exactly one run.
// The concrete broker/pipeline/risk-engine choice (a simulated broker
// today) lives entirely behind this interface — Service never imports
// a concrete adapter to obtain one (issue #221 review: "service/
// backtest never imports the concrete adapter" is the invariant this
// interface exists to protect). A composition root injects an
// implementation of this interface into New; the implementation itself
// may live anywhere convenient (an adapter package, cmd/trader/
// backtest, or elsewhere) — Service does not constrain that.
type EnvironmentFactory interface {
	NewEnvironment(ctx context.Context, req EnvironmentRequest) (Environment, error)
}
