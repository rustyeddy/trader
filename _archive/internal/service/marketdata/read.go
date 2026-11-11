package marketdata

import (
	"context"
	"io"
	"log/slog"

	marketruntime "github.com/rustyeddy/trader/internal/marketdata"
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
		s.logOutcome(ctx, slog.LevelDebug, "bars queried", "bars query failed", req.DatasetRequest, err)
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
			s.logOutcome(ctx, slog.LevelDebug, "bars queried", "bars query failed", req.DatasetRequest, err)
			return BarsResponse{}, err
		}
		bars = append(bars, bar)
	}

	s.logOutcome(ctx, slog.LevelDebug, "bars queried", "bars query failed", req.DatasetRequest, nil,
		"bar_count", len(bars))
	return BarsResponse{Bars: bars, Manifests: reader.Manifests()}, nil
}

// Coverage implements the read-only Coverage use case (issue #105):
// coverage and gap reporting for req's dataset. Coverage performs no
// acquisition or build; it only reports what Manager already knows or
// can determine by inspecting the raw archive and canonical store.
//
// An omitted Range (issue #439) means the dataset's existing canonical
// span, from Inventory: [Canonical.First, Canonical.End). With no
// readable canonical data there is nothing to cover, and Coverage
// returns an empty Coverage (no partitions, zero Range) rather than an
// error.
func (s *Service) Coverage(ctx context.Context, req CoverageRequest) (CoverageResponse, error) {
	if err := req.Validate(); err != nil {
		return CoverageResponse{}, err
	}
	resp, err := s.coverage(ctx, req)
	s.logOutcome(ctx, slog.LevelDebug, "coverage queried", "coverage query failed", req.DatasetRequest, err)
	return resp, err
}

// coverage is Coverage without validation or its outcome log.
func (s *Service) coverage(ctx context.Context, req CoverageRequest) (CoverageResponse, error) {
	if req.Range.Start().IsZero() {
		inv, err := s.manager.Inventory(ctx, req.Instrument, req.Interval)
		if err != nil {
			return CoverageResponse{}, err
		}
		span := inv.Canonical
		if span == nil || span.First.IsZero() {
			return CoverageResponse{Coverage: marketruntime.Coverage{Instrument: req.Instrument, Interval: req.Interval}}, nil
		}
		if req.Range, err = marketdata.NewTimeRange(span.First, span.End); err != nil {
			return CoverageResponse{}, err
		}
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
		s.logOutcome(ctx, slog.LevelDebug, "plan computed", "plan computation failed", req.DatasetRequest, err)
		return PlanResponse{}, err
	}
	s.logOutcome(ctx, slog.LevelDebug, "plan computed", "plan computation failed", req.DatasetRequest, nil,
		"action_count", len(plan.Actions))
	return PlanResponse{Plan: plan}, nil
}

// Inventory implements the read-only Inventory use case (issue #435):
// what raw and canonical data exist for req's instrument and interval,
// without a caller-supplied range. Use it to default a range — the full
// raw span to canonicalize, or the canonical end to update from — and
// Coverage over that range for gaps. Inventory performs no
// acquisition, build, or write.
func (s *Service) Inventory(ctx context.Context, req InventoryRequest) (InventoryResponse, error) {
	if err := req.Validate(); err != nil {
		return InventoryResponse{}, err
	}
	dataset := DatasetRequest{Instrument: req.Instrument, Interval: req.Interval}
	inv, err := s.manager.Inventory(ctx, req.Instrument, req.Interval)
	s.logOutcome(ctx, slog.LevelDebug, "inventory queried", "inventory query failed", dataset, err,
		"has_raw", inv.Raw != nil, "has_canonical", inv.Canonical != nil)
	if err != nil {
		return InventoryResponse{}, err
	}
	return InventoryResponse{Inventory: inv}, nil
}
