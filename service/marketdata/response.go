package marketdata

import "github.com/rustyeddy/trader/marketdata"

// BarsResponse is the structured result of the Bars use case: every Bar
// in the requested dataset, in chronological order, alongside the
// canonical Manifest each one was assembled from (see
// marketdata.BarReader.Manifests for what that provenance discloses).
// Bars materializes the full result rather than returning a Manager
// BarReader itself, since a Manager query is already fully resolved
// in-memory before Bars returns one (BarReader's own doc comment);
// this response is a plain value, never a live handle a caller must
// remember to Close.
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
