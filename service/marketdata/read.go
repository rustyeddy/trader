package marketdata

import (
	"context"
	"io"

	"github.com/rustyeddy/trader/marketdata"
)

// Bars implements the read-only Bars use case (issue #105): the
// canonical Bar data for req's dataset. Bars performs no acquisition,
// build, or other mutation — see Sync and Build (issue #106) for the
// corresponding mutating operations. Cancellation propagates through
// ctx into the underlying *marketdata.Manager.Bars call and every
// subsequent read of its result.
//
// On any error, including ctx cancellation partway through draining
// the result, Bars returns a zero BarsResponse rather than a partial
// one — the same "no partial results on error" contract
// *marketdata.Manager.Bars itself documents.
func (s *Service) Bars(ctx context.Context, req BarsRequest) (BarsResponse, error) {
	if err := req.Validate(); err != nil {
		return BarsResponse{}, err
	}

	reader, err := s.manager.Bars(ctx, req.query())
	if err != nil {
		return BarsResponse{}, err
	}
	defer reader.Close()

	var bars []marketdata.Bar
	for {
		bar, err := reader.Next(ctx)
		if err != nil {
			if err == io.EOF {
				break
			}
			return BarsResponse{}, err
		}
		bars = append(bars, bar)
	}

	return BarsResponse{Bars: bars, Manifests: reader.Manifests()}, nil
}

// Coverage implements the read-only Coverage use case (issue #105):
// coverage and gap reporting for req's dataset. Coverage performs no
// acquisition or build; it only reports what Manager already knows or
// can determine by inspecting the raw archive and canonical store.
func (s *Service) Coverage(ctx context.Context, req CoverageRequest) (CoverageResponse, error) {
	if err := req.Validate(); err != nil {
		return CoverageResponse{}, err
	}

	cov, err := s.manager.Coverage(ctx, req.query())
	if err != nil {
		return CoverageResponse{}, err
	}
	return CoverageResponse{Coverage: cov}, nil
}

// Plan implements the read-only Plan use case (issue #105): the
// acquisition/build work required to make req's dataset available.
// Plan never downloads, builds, or publishes anything itself; a caller
// wanting that work actually performed executes the returned Plan
// through Sync and Build (issue #106) or the higher-level Update
// orchestration (issue #107).
func (s *Service) Plan(ctx context.Context, req PlanRequest) (PlanResponse, error) {
	if err := req.Validate(); err != nil {
		return PlanResponse{}, err
	}

	plan, err := s.manager.Plan(ctx, req.query())
	if err != nil {
		return PlanResponse{}, err
	}
	return PlanResponse{Plan: plan}, nil
}
