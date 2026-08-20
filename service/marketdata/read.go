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
//
// # Materialization is a deliberate, revisitable v0 decision
//
// *marketdata.Manager.Bars already fully resolves its result in memory
// before returning a BarReader (BarReader's own doc comment); draining
// that reader here into BarsResponse.Bars necessarily produces a
// second, independent []marketdata.Bar the same size as the reader's
// own — both are live for the duration of this call, before reader
// goes out of scope and becomes eligible for garbage collection. For a
// query spanning years of historical data this is a real, non-trivial
// transient memory cost, not a rounding error.
//
// This is accepted for v0 rather than solved speculatively: it mirrors
// the identical tradeoff marketdata's own internal readAllBars already
// makes for the same reason (query.go), and BarsResponse is a plain
// value — never a live handle a caller must remember to Close — which
// is the service layer's own stated convention (service/doc.go). If a
// real caller (CLI, REST, or otherwise) needs to query a range large
// enough that this transient doubling actually matters, the fix
// belongs in a bounded or streaming service response shape introduced
// deliberately at that point — not a speculative abstraction added
// here before any consumer exists to validate its shape.
func (s *Service) Bars(ctx context.Context, req BarsRequest) (BarsResponse, error) {
	if err := req.Validate(); err != nil {
		return BarsResponse{}, err
	}

	reader, err := s.manager.Bars(ctx, req.query())
	if err != nil {
		return BarsResponse{}, err
	}
	defer func() { _ = reader.Close() }()

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
