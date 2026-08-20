package marketdata

import (
	"context"

	"github.com/rustyeddy/trader/marketdata"
)

// Update implements the higher-level Update use case (issue #107):
// bring req's dataset current by composing Plan, Sync, and Build. It is
// an application-level orchestration, not a new marketdata.Manager
// primitive — the CLI, and any future transport, calls this one Service
// method rather than reimplementing the Plan -> Sync -> Build sequence
// itself (ADR-022).
//
// # Why Sync and Build each recompute their own Plan
//
// Update computes one InitialPlan up front, purely to report what the
// dataset needed at the moment it was called. It does not reuse that
// Plan for Sync or Build: both are called through their own Service
// methods, each of which computes a fresh Plan internally. This is
// deliberate, not redundant work to be optimized away. Canonical
// normalization is gated on raw actually being present
// (marketdata.deriveActionsRawBuilt's own "gated scheduling" rule): a
// Plan computed before Sync downloads missing raw data cannot yet see
// the ActionNormalizeCanonical entries that become schedulable only
// once that raw data exists. Reusing InitialPlan for Build would
// silently skip exactly the canonical work Sync's own downloads just
// made possible.
//
// # Sync is skipped when nothing raw-related is needed
//
// Update calls Sync only when InitialPlan contains at least one
// ActionDownloadRaw or ActionRepairRaw entry. This means Update never
// requires an OANDA client to be configured for a dataset that only
// needs a canonical build from already-synced raw data — Manager.Sync
// itself requires OANDA configuration unconditionally, even for a Plan
// containing no download actions, so calling it needlessly would turn
// an otherwise offline-capable canonical build into a hard failure.
//
// # Failure stops the pipeline
//
// If Sync is performed and fails, Update returns immediately: Build is
// never attempted against raw data known to have partially failed to
// acquire. UpdateResponse.Sync still carries whatever partial progress
// Sync made (SyncResponse's own contract); UpdateResponse.Build remains
// the zero value in that case. If Sync succeeds (or was not needed) and
// Build then fails, Update returns with UpdateResponse.Build carrying
// Build's own partial progress.
//
// # No work when nothing is required
//
// When InitialPlan is empty (the dataset is already fully current),
// Sync is skipped (SyncPerformed is false) and Build is still called,
// but its own freshly-computed Plan is equally empty, so
// *marketdata.Manager.Build's loop performs no writes — the same
// "already current" outcome running Update again immediately after a
// successful Update produces, making repeated calls safe and
// deterministic.
func (s *Service) Update(ctx context.Context, req UpdateRequest) (UpdateResponse, error) {
	if err := req.Validate(); err != nil {
		return UpdateResponse{}, err
	}

	initialPlan, err := s.manager.Plan(ctx, req.query())
	if err != nil {
		return UpdateResponse{}, err
	}
	resp := UpdateResponse{InitialPlan: initialPlan}

	if planNeedsSync(initialPlan) {
		resp.SyncPerformed = true
		syncResp, err := s.Sync(ctx, SyncRequest(req))
		resp.Sync = syncResp
		if err != nil {
			return resp, err
		}
	}

	buildResp, err := s.Build(ctx, BuildRequest(req))
	resp.Build = buildResp
	if err != nil {
		return resp, err
	}

	return resp, nil
}

// planNeedsSync reports whether plan contains any Action Sync itself
// would act on or explicitly report skipping — ActionDownloadRaw or
// ActionRepairRaw. A plan containing only canonical Actions
// (ActionNormalizeCanonical, ActionDeriveCanonical) needs no Sync call
// at all.
func planNeedsSync(plan marketdata.Plan) bool {
	for _, a := range plan.Actions {
		if a.Kind == marketdata.ActionDownloadRaw || a.Kind == marketdata.ActionRepairRaw {
			return true
		}
	}
	return false
}
