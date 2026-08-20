package marketdata

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/rustyeddy/trader/marketdata"
)

// ErrUpdateIncomplete marks an Update call that ran Sync and/or Build
// but left the dataset with outstanding required work afterward — most
// notably an ActionRepairRaw entry, which neither Sync nor Build ever
// resolves (see Update's own doc comment). UpdateResponse.FinalPlan
// names exactly what remains.
var ErrUpdateIncomplete = errors.New("service/marketdata: update: dataset still requires action after update")

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
// # Sync is skipped unless raw data must actually be downloaded
//
// Update calls Sync only when InitialPlan contains at least one
// ActionDownloadRaw entry — never merely because it contains
// ActionRepairRaw. *marketdata.Manager.Sync executes only
// ActionDownloadRaw; every other Action Kind, including
// ActionRepairRaw, it reports in SyncResult.Skipped and otherwise
// ignores (Manager.Sync's own "only raw downloads" doc comment). A plan
// containing only a repair requirement would therefore gain nothing
// from a Sync call — Sync cannot perform the repair — while still
// paying the same unconditional OANDA-configuration requirement every
// Sync call has, turning an unrelated repair need into a needless hard
// failure for a dataset that may not even need a download. Since Build
// cannot resolve a raw repair either (build.go's own "only canonical
// builds" scope split), an unresolved ActionRepairRaw is instead caught
// by the final-Plan check below, which reports it clearly rather than
// silently leaving Sync to skip it and Update to report success anyway.
//
// # Success requires a clean final Plan, not just error-free calls
//
// Sync and Build both returning without error is not sufficient to
// claim the dataset is now current: an ActionRepairRaw entry survives
// both untouched, since neither stage ever executes one. Update
// therefore recomputes the Plan one final time after Sync/Build
// complete and fails with ErrUpdateIncomplete if any Action remains —
// this catches ActionRepairRaw specifically today, and, by checking the
// actual resulting state rather than special-casing one ActionKind,
// remains correct if a future ActionKind neither Sync nor Build handles
// is ever introduced. UpdateResponse.FinalPlan is always populated when
// this final check runs, so a caller can see exactly what is still
// outstanding.
//
// # Failure stops the pipeline
//
// If Sync is performed and fails, Update returns immediately: Build is
// never attempted against raw data known to have partially failed to
// acquire. UpdateResponse.Sync still carries whatever partial progress
// Sync made (SyncResponse's own contract); UpdateResponse.Build and
// FinalPlan remain their zero values in that case. If Sync succeeds (or
// was not needed) and Build then fails, Update returns with
// UpdateResponse.Build carrying Build's own partial progress, and
// FinalPlan is likewise never computed.
//
// # No work when nothing is required
//
// When InitialPlan is empty (the dataset is already fully current),
// Sync is skipped (SyncPerformed is false) and Build is still called,
// but its own freshly-computed Plan is equally empty, so
// *marketdata.Manager.Build's loop performs no writes — the same
// "already current" outcome running Update again immediately after a
// successful Update produces, making repeated calls safe and
// deterministic. The final Plan check likewise finds nothing
// outstanding and Update returns success.
func (s *Service) Update(ctx context.Context, req UpdateRequest) (resp UpdateResponse, err error) {
	if err := req.Validate(); err != nil {
		return UpdateResponse{}, err
	}

	// Named returns plus one deferred log call, rather than a logOutcome
	// call before every return statement below: Update has four distinct
	// return points (initial Plan failure, Sync failure, Build failure,
	// ErrUpdateIncomplete) and one success path, and every one of them
	// needs the identical "log the overall outcome exactly once" record.
	// A defer reading the named resp/err results is what makes that
	// single log call correct regardless of which return statement below
	// actually fires — see Update's own doc comment for why this
	// higher-level record is a deliberate, distinct event from the
	// inner Sync/Build calls' own logOutcome records, not a duplicate of
	// them.
	defer func() {
		s.logOutcome(ctx, slog.LevelInfo, "update completed", "update failed", req.DatasetRequest, err,
			"sync_performed", resp.SyncPerformed,
			"downloaded_partitions", len(resp.Sync.Result.Downloaded),
			"published_partitions", len(resp.Build.Result.Published),
		)
	}()

	initialPlan, err := s.manager.Plan(ctx, req.query())
	if err != nil {
		return UpdateResponse{}, err
	}
	resp = UpdateResponse{InitialPlan: initialPlan}

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

	finalPlan, err := s.manager.Plan(ctx, req.query())
	if err != nil {
		return resp, err
	}
	resp.FinalPlan = finalPlan
	if len(finalPlan.Actions) > 0 {
		return resp, fmt.Errorf("%w: %d action(s) remain, first: %s %04d-%02d (%s)",
			ErrUpdateIncomplete, len(finalPlan.Actions),
			finalPlan.Actions[0].Kind, finalPlan.Actions[0].Year, int(finalPlan.Actions[0].Month),
			finalPlan.Actions[0].Reason)
	}

	return resp, nil
}

// planNeedsSync reports whether plan contains an ActionDownloadRaw
// entry — the only Action Kind *marketdata.Manager.Sync ever executes.
// A plan containing only ActionRepairRaw, ActionNormalizeCanonical, or
// ActionDeriveCanonical entries needs no Sync call at all; see Update's
// own doc comment for why ActionRepairRaw specifically must not trigger
// one.
func planNeedsSync(plan marketdata.Plan) bool {
	for _, a := range plan.Actions {
		if a.Kind == marketdata.ActionDownloadRaw {
			return true
		}
	}
	return false
}
