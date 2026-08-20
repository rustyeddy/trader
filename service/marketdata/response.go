package marketdata

import "github.com/rustyeddy/trader/marketdata"

// BarsResponse is the structured result of the Bars use case: every Bar
// in the requested dataset, in chronological order, alongside the
// canonical Manifest for every partition that was loaded to assemble
// the result (see marketdata.BarReader.Manifests for what that
// provenance discloses) — Manifests is not a per-Bar association;
// several Bars are typically assembled from one Manifest's partition,
// and the two slices do not correspond index-for-index. Bars
// materializes the full result rather than returning a Manager
// BarReader itself, since a Manager query is already fully resolved
// in-memory before Bars returns one (BarReader's own doc comment);
// this response is a plain value, never a live handle a caller must
// remember to Close. See Service.Bars's own doc comment for the
// memory-cost tradeoff that materialization decision makes.
type BarsResponse struct {
	Bars      []marketdata.Bar
	Manifests []marketdata.Manifest
}

// CoverageResponse is the structured result of the Coverage use case.
type CoverageResponse struct {
	Coverage marketdata.Coverage
}

// PlanResponse is the structured result of the Plan use case: the work
// required to make the requested dataset available. A PlanResponse
// describes work; it never performs any of it.
type PlanResponse struct {
	Plan marketdata.Plan
}

// SyncResponse is the structured result of the Sync use case: the Plan
// Service.Sync computed and executed against, alongside the raw
// acquisition outcome. Plan is included so a caller can see exactly
// what Sync was executing against, including any non-download Actions
// it necessarily skipped (marketdata.SyncResult.Skipped already
// explains why; Plan is what makes "why wasn't this raw partition
// synced" answerable without a second Plan call).
//
// Result is populated even when Sync itself returns a non-nil error:
// *marketdata.Manager.Sync documents returning whatever partial
// SyncResult it had accumulated before the failure, not a zero value,
// and Service.Sync preserves that same partial-progress contract rather
// than discarding it.
type SyncResponse struct {
	Plan   marketdata.Plan
	Result marketdata.SyncResult
}

// BuildResponse is the structured result of the Build use case, the
// mirror of SyncResponse for canonical publication. Result is likewise
// populated on partial failure, matching
// *marketdata.Manager.Build's own contract.
type BuildResponse struct {
	Plan   marketdata.Plan
	Result marketdata.BuildResult
}

// UpdateResponse is the structured result of the higher-level Update
// use case (issue #107): bring one dataset current by composing Plan,
// Sync, and Build.
//
// InitialPlan is the Plan Update computed before doing any work at
// all — what the dataset needed at the moment Update was called, before
// Sync's own downloads could change the world Build's later Plan call
// evaluates. It is not necessarily the same Plan Sync or Build each
// executed against: both recompute their own fresh Plan when Update
// calls them (Service.Sync/Service.Build's own contract), which is
// exactly what lets Build discover canonical work that only became
// possible after Sync's downloads landed — see Service.Update's own
// doc comment for why recomputation, not reuse of one Plan across all
// three stages, is the correct behavior here.
//
// SyncPerformed reports whether Update actually invoked Sync at all:
// InitialPlan may contain no raw-related Action (every raw partition
// already present and OK), in which case Sync is skipped entirely —
// deliberately, so Update never requires OANDA configuration for a
// dataset that only needs a canonical build from already-synced raw
// data. Sync is the zero SyncResponse when SyncPerformed is false, not
// a meaningful "nothing to do" result.
//
// Build is always attempted, even when InitialPlan reported nothing
// requiring canonical work: its own fresh Plan may still find nothing
// to do (the ordinary "already current" case) or, after a successful
// Sync, may find newly-possible canonical work InitialPlan could not
// yet see. Build is the zero BuildResponse only if Sync itself failed
// first (see below).
//
// If Sync is performed and returns an error, Update returns
// immediately with that error and Build is never attempted — building
// canonical data is deliberately not attempted against a raw
// acquisition that is known to have partially failed. Sync.Result
// still reports whatever partial progress Sync made before the
// failure (SyncResponse's own contract).
//
// FinalPlan is a Plan recomputed after Sync/Build complete
// successfully, and is populated only when that recomputation actually
// runs (never after a Sync or Build failure, which each return before
// reaching it). A non-empty FinalPlan.Actions means Update failed with
// ErrUpdateIncomplete: something — most notably an unresolved
// ActionRepairRaw, which neither Sync nor Build ever executes — is
// still outstanding even though neither stage itself returned an
// error. FinalPlan names exactly what remains for a caller to act on.
type UpdateResponse struct {
	InitialPlan   marketdata.Plan
	SyncPerformed bool
	Sync          SyncResponse
	Build         BuildResponse
	FinalPlan     marketdata.Plan
}
