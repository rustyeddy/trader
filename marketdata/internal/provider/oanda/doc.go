// Package oanda reads Trader's preserved raw OANDA historical CSV corpus
// (issue #70, M2-02). It is an internal market-data provider: it lives under
// marketdata/internal/, which Go restricts to importers within the
// marketdata/ subtree, so nothing outside that subsystem can reach it — all
// access is through marketdata.Manager, per ADR-020.
//
// # What it does, and does not, do
//
// This package parses the existing raw archive; it does not download data,
// choose Trader's canonical storage format, mutate any dataset, or resample.
// A raw file's rows become provider-native Record values that a later
// normalization stage converts into canonical marketdata.Bar values. Record
// is deliberately close to the file: bid/ask OHLC and OANDA's own tick/
// activity count and complete flag, preserved rather than interpreted.
//
// To keep the dependency direction clean — and to let marketdata.Manager own
// and wire this provider without an import cycle — the package does not import
// the root marketdata package. It carries a provider-native RawInterval token
// rather than marketdata.Interval; mapping a RawInterval to the canonical
// Interval is a normalization concern, not this reader's.
//
// # Archive shape
//
// The corpus is laid out as
//
//	oanda/PAIR/YYYY/MM/PAIR-YYYY-MM-<tf>.csv
//
// with <tf> one of m1, h1, h4, d1. Each file is self-describing:
//
//	# schema=raw-v1 source=oanda instrument=EURUSD tf=h1 year=2020 month=05
//	time,bid_o,bid_h,bid_l,bid_c,ask_o,ask_h,ask_l,ask_c,volume,complete
//	2020-05-01T00:00:00Z,1.09439,1.09548,...,2235,true
//
// The file path is authoritative for a partition's instrument, interval,
// year, and month; the schema comment, when present, is cross-checked
// against it and a disagreement is an error. The daily files name the
// interval d1 in the path but tf=d in the comment; both map to D1.
//
// # Scope
//
// Scope is an exact, audited set of the 24 FX pairs preserved in the archive,
// not a currency-combination rule: two in-scope currencies do not by
// themselves make an in-scope pair (there is no USDEUR or CADNZD partition).
// A symbol outside that set — XAUUSD (gold is not an FX leg) or a valid-looking
// but absent pair — reports ErrInstrumentOutOfScope rather than being treated
// as an FX pair. W1 is not present in the raw corpus and is not invented: a w1
// partition reports ErrUnsupportedInterval.
//
// # Exactness and provenance
//
// Prices parse to exact num.Price values; they are never routed through
// float64. Timestamps are read from each row and never reconstructed from row
// position. Malformed rows — wrong field count, unparseable price, time,
// volume, or complete flag — are reported as errors rather than silently
// skipped or coerced.
//
// # Future live synchronization
//
// This package does not talk to OANDA's API; it only reads the preserved
// archive. When a later milestone adds live acquisition, it must pin
// dailyAlignment=17 and alignmentTimezone=America/New_York explicitly on
// every daily request rather than relying on OANDA's undocumented
// defaults (issue #74, ADR-020) — the archive's own D1 opens already
// match a 17:00 America/New_York rollover, which is only true because
// the legacy client happened to inherit those same defaults, not because
// it requested them. A pinned request survives a provider default
// changing without Trader silently mis-aligning new data against the
// old archive. There is no OANDA-native weekly partition in the
// preserved archive (see RawInterval); W1 is a derived interval in M2,
// built by resampling canonical D1, not synchronized from OANDA
// directly, so no weeklyAlignment parameter is pinned here.
package oanda
