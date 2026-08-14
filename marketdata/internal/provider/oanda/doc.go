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
// The 24 in-scope FX pairs are those whose base and quote are both in-scope
// FX currencies (AUD, CAD, CHF, EUR, GBP, JPY, NZD, USD). XAUUSD is present
// in the corpus but out of scope: XAU is not an in-scope currency, so opening
// an XAUUSD partition reports ErrInstrumentOutOfScope rather than silently
// treating gold as an FX pair. W1 is not present in the raw corpus and is not
// invented: a w1 partition reports an unsupported-interval error.
//
// # Exactness and provenance
//
// Prices parse to exact num.Price values; they are never routed through
// float64. Timestamps are read from each row and never reconstructed from row
// position. Malformed rows — wrong field count, unparseable price, time,
// volume, or complete flag — are reported as errors rather than silently
// skipped or coerced.
package oanda
