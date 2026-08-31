package backtest

import (
	"context"

	"github.com/rustyeddy/trader/backtest"
)

// Run implements the Run use case: build a fresh Environment for this
// one call, then carry req through the full canonical M5 backtest path
// (backtest.NewRunner/Run) using it.
//
// Sequencing follows issue #221 review exactly: req is validated,
// then ctx is checked for cancellation *before* EnvironmentFactory.
// NewEnvironment is ever called — a pre-canceled request must not
// construct or open a broker/account it will never use. Only once
// both checks pass does Run ask the factory for an Environment,
// validate that Environment's own required fields, and hand
// everything to backtest.NewRunner. backtest.Runner.Run performs its
// own ctx.Err() check before any further work, so cancellation between
// this point and Runner's own first step is still caught there.
func (s *Service) Run(ctx context.Context, req RunRequest) (RunResponse, error) {
	if err := req.Validate(); err != nil {
		return RunResponse{}, err
	}
	if err := ctx.Err(); err != nil {
		s.logRunOutcome(ctx, req, RunResponse{}, err)
		return RunResponse{}, err
	}

	env, err := s.environments.NewEnvironment(ctx, EnvironmentRequest{
		StartingCapital: req.StartingCapital,
		Span:            req.Span,
	})
	if err != nil {
		s.logRunOutcome(ctx, req, RunResponse{}, err)
		return RunResponse{}, err
	}
	if err := env.validate(); err != nil {
		s.logRunOutcome(ctx, req, RunResponse{}, err)
		return RunResponse{}, err
	}

	runner, err := backtest.NewRunner(backtest.RunnerParams{
		Manager:            s.manager,
		Resolver:           s.resolver,
		Clock:              env.Clock,
		IDs:                env.IDs,
		Pipeline:           env.Pipeline,
		Account:            env.Account,
		RiskFraction:       req.RiskFraction,
		AdverseDistance:    req.AdverseDistance,
		RiskRules:          env.RiskRules,
		FillModel:          env.FillModel,
		SlippageModel:      env.SlippageModel,
		CommissionModel:    env.CommissionModel,
		Strategy:           req.Strategy,
		StrategyParameters: req.StrategyParameters,
		Span:               req.Span,
		TraderVersion:      req.TraderVersion,
		Logger:             s.logger,
		Journal:            env.Journal,
	})
	if err != nil {
		s.logRunOutcome(ctx, req, RunResponse{}, err)
		return RunResponse{}, err
	}

	result, err := runner.Run(ctx)
	resp := toResponse(result)
	s.logRunOutcome(ctx, req, resp, err)
	return resp, err
}
