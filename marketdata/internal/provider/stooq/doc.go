// Package stooq reads and imports Trader's preserved raw Stooq daily
// equity CSV history (issue #303, EQ-03A). It is an internal
// market-data provider: it lives under marketdata/internal/, which Go
// restricts to importers within the marketdata/ subtree, so nothing
// outside that subsystem can reach it — all access is through
// marketdata.Manager, per ADR-020/ADR-047.
//
// # What it does, and does not, do
//
// This package imports Stooq's own native single-file-per-instrument
// CSV export into Trader's raw-archive partition layout, and then reads
// and inspects that raw archive the same way marketdata/internal/
// provider/oanda reads its own. It does not choose Trader's canonical
// storage format, resample, or apply any corporate-action adjustment —
// canonical bars store exactly what Stooq delivers (ADR-047, EQ-05
// (#298) deferral).
//
// Unlike oanda, this package has no live acquisition client: Stooq is
// used here as a purely offline, local-file data source (issue #303's
// own explicit constraint — "should not require network access to
// Stooq"). There is no Sync-equivalent network fetch; Import (import.go)
// is the one, explicit, operator-run acquisition step, reading a
// caller-supplied Stooq CSV export once and writing it out as raw
// partitions. Running Import again over an updated Stooq export is how
// a caller brings the raw archive current — there is no automatic
// "extend" the way oanda.Client.FetchCandles provides.
//
// # Archive shape
//
// The raw archive this package produces and reads matches ADR-020's own
// partition convention, adapted for Stooq's single-price schema:
//
//	stooq/SYMBOL/YYYY/MM/SYMBOL-YYYY-MM-d1.csv
//
// Each file is self-describing:
//
//	# schema=raw-v1 source=stooq instrument=SPY tf=d1 year=2020 month=05
//	date,open,high,low,close,volume
//	2020-05-01,282.80,283.19,278.85,282.79,74424000
//
// The file path is authoritative for a partition's symbol, year, and
// month; the schema comment, when present, is cross-checked against it
// and a disagreement is an error — mirroring oanda's own reader exactly.
//
// # Time semantics (deferred to EQ-03/#296)
//
// A Record's Time is midnight UTC of the row's own trading date. This
// is a deliberate, minimal convention — not a claim that midnight UTC
// is a real NYSE/Nasdaq session boundary — chosen only so each date
// falls unambiguously inside the correct UTC calendar month for
// Trader's existing month-partitioned raw/canonical layout. Validating
// that a record's date actually falls on a real trading-calendar
// boundary (as marketdata's own FX normalization already does via
// Calendar.Bar for FX data) requires a real U.S. equity trading
// calendar, which does not exist yet — ADR-047 defers it to EQ-03
// (#296). Until then, this package and its marketdata-side normalizer
// trust each row's own date verbatim, the same way the row's own OHLC
// values are trusted verbatim (see the corporate-action deferral
// above).
//
// # Exactness and provenance
//
// Prices parse to exact num.Price values; they are never routed through
// float64. Malformed rows — wrong field count, an unparseable date,
// price, or volume — are reported as errors rather than silently
// skipped or coerced, matching oanda's own reader discipline. Sequence-
// level checks (duplicate or out-of-order dates) and OHLC-shape
// validation are deliberately not this package's job — they live in
// marketdata's own normalization stage (normalize_stooq.go), the exact
// same layering oanda's raw reader and marketdata's normalize_oanda.go
// already split the work along.
package stooq
