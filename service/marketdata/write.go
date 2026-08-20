package marketdata

import (
	"context"
	"log/slog"
)

// Sync implements the mutating Sync use case (issue #106): acquire the
// raw data required to make req's dataset available. Sync computes a
// fresh Plan for req (never a caller-supplied or previously-computed
// Plan — the same "never its own recomputed plan" wording in
// *marketdata.Manager.Sync's doc comment describes Manager's contract
// once handed a Plan; Service.Sync is what actually produces the Plan
// Manager executes, from req, every call) and executes it through
// *marketdata.Manager.Sync. Only ActionDownloadRaw entries are ever
// acquired; canonical publication is Build's job (issue #106), not
// Sync's — see *marketdata.Manager.Sync's own doc comment for the full
// "only raw downloads" contract this inherits unchanged.
//
// Unlike the read-only operations (Bars, Coverage, Plan), Sync does not
// return a zero SyncResponse on error: *marketdata.Manager.Sync
// documents returning whatever partial SyncResult it had already
// accumulated when a later Action fails or ctx is cancelled mid-loop,
// so a caller can see exactly what did and did not complete. Service.Sync
// preserves that same partial-progress contract rather than discarding
// it — see SyncResponse's own doc comment.
func (s *Service) Sync(ctx context.Context, req SyncRequest) (SyncResponse, error) {
	if err := req.Validate(); err != nil {
		return SyncResponse{}, err
	}

	plan, err := s.manager.Plan(ctx, req.query())
	if err != nil {
		s.logOutcome(ctx, slog.LevelInfo, "sync completed", "sync failed", req.DatasetRequest, err)
		return SyncResponse{}, err
	}

	result, err := s.manager.Sync(ctx, plan)
	s.logOutcome(ctx, slog.LevelInfo, "sync completed", "sync failed", req.DatasetRequest, err,
		"downloaded_count", len(result.Downloaded), "skipped_count", len(result.Skipped))
	return SyncResponse{Plan: plan, Result: result}, err
}

// Build implements the mutating Build use case (issue #106): build and
// publish the canonical data required to make req's dataset available,
// from whatever raw data already exists. Build computes a fresh Plan
// for req, the same way Sync does, and executes it through
// *marketdata.Manager.Build. Only ActionNormalizeCanonical and
// ActionDeriveCanonical entries are ever published; acquiring raw data
// is Sync's job, not Build's — see *marketdata.Manager.Build's own doc
// comment for the full "only canonical builds" contract this inherits
// unchanged.
//
// Like Sync, Build preserves *marketdata.Manager.Build's partial-
// progress contract: BuildResponse.Result is populated even when Build
// returns a non-nil error.
func (s *Service) Build(ctx context.Context, req BuildRequest) (BuildResponse, error) {
	if err := req.Validate(); err != nil {
		return BuildResponse{}, err
	}

	plan, err := s.manager.Plan(ctx, req.query())
	if err != nil {
		s.logOutcome(ctx, slog.LevelInfo, "build completed", "build failed", req.DatasetRequest, err)
		return BuildResponse{}, err
	}

	result, err := s.manager.Build(ctx, plan)
	s.logOutcome(ctx, slog.LevelInfo, "build completed", "build failed", req.DatasetRequest, err,
		"published_count", len(result.Published), "skipped_count", len(result.Skipped))
	return BuildResponse{Plan: plan, Result: result}, err
}
