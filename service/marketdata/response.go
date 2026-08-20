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
