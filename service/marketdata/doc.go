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
// Service never reaches into marketdata/internal, never formats a
// response, and never depends on a transport framework — see the
// service package's own doc comment for the full set of rules every
// service subpackage follows.
package marketdata
