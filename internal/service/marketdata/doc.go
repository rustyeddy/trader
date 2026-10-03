// Package marketdata is the application/service layer for Trader's
// historical market-data capabilities (ADR-022, issue #103), built as
// the first subpackage of service (issue #104).
//
// Service wraps a *marketdata.Manager and will expose the read-only
// (issue #105: Bars, Coverage, Plan) and mutating (issue #106: Sync,
// Build) M2 operations as transport-neutral use cases, plus the
// higher-level Update orchestration (issue #107) that composes them.
// This package intentionally does not yet implement those operations;
// it establishes the package's construction and request-DTO
// conventions those issues build on.
//
// ConvertStooqArchive (issue #434) owns native Stooq archive handling —
// discovery under an archive root, member extraction, and the reference
// listing defaults (DefaultListing) — so every transport converts an
// archive through the same operation (ADR-069).
//
// ResolveInstrument(s) resolves symbols through the Service's own resolver
// (issue #448, ADR-070). DatasetsCoverage, CanonicalizeDatasets, and
// UpdateDatasets (issue #439) act on several symbols at once and report
// a result per symbol; an omitted range defaults per symbol from
// Inventory (the canonical span, the raw span, or the canonical end
// through now).
//
// Service never reaches into marketdata/internal, never formats a
// response, and never depends on a transport framework — see the
// service package's own doc comment for the full set of rules every
// service subpackage follows.
package marketdata
