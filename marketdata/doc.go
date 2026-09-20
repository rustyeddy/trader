// Package marketdata provides supported market-data values and deterministic
// dataset semantics for external strategies and research (ADR-065): bars,
// intervals, time ranges, calendars, bar sets, and provenance manifests.
// Acquisition, provider access, normalization, storage, and Manager live in
// Trader's internal runtime and are not available through this package.
//
// TimeRange is half-open [start, end). Interval uses a typed Unit and count;
// Calendar aligns bars to sessions rather than assuming UTC clock alignment.
// FXCalendar uses New York rollover boundaries; USEquityCalendar supplies
// US equity sessions and daily bar semantics. Calendar calculations, dataset
// completeness classification, and manifest validation/revisions perform no
// provider or storage I/O. Callers supply timestamps explicitly.
//
// Bars carry exact num prices. Analytical consumers convert through
// num.Price.Float64 when needed. Manifest and BarSet record canonical dataset
// provenance and adjustment semantics without exposing a runtime handle.
package marketdata
