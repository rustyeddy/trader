// Package alpaca implements Trader's Alpaca historical-equity-data
// provider (issue #297, EQ-04): a live-API acquisition client plus the
// same raw-partition read/write/inspect contract oanda and stooq
// already establish, so marketdata's own Manager/Coverage/Plan/Sync/
// Build pipeline can drive Alpaca exactly like either existing
// provider (ADR-047's internal provider seam).
//
// # Scope
//
// This package fetches, persists, and reads back daily (D1) OHLCV bars
// for US equities/ETFs. It owns nothing about instrument identity,
// canonical normalization, or calendar alignment — those remain
// marketdata's own concerns (see marketdata/normalize_alpaca.go and
// USEquityCalendar, issue #296, which this provider's D1 data
// validates against exactly like Stooq's).
//
// # An unverified API-shape assumption — read before trusting this package's request/response wiring
//
// Nothing in this repository, and no live network access in the
// environment this package was written in, could verify Alpaca's real
// Market Data API v2 wire contract. This package's request
// construction and response parsing (see client.go and wireshape.go)
// are built against the author's general knowledge of that API's
// publicly documented shape — endpoint path, header names, query
// parameters, and JSON field names — and are an explicit, named
// design assumption, not a confirmed fact. wireshape.go isolates every
// JSON-shape-specific type and parsing function into one small file
// specifically so that correcting it against the real API (via the
// opt-in smoke test in marketdata/internal/provider/alpaca/smoke_test.go,
// run by an operator with real credentials) is a small, contained
// change, not a redesign of this package. See ADR-050
// (docs/arch/adr-050-alpaca-historical-provider.org) for the full
// discussion of this risk and how it is scoped.
//
// # Timestamp normalization: trading date, not literal fetched instant
//
// Alpaca's assumed JSON bar shape times a daily bar at the session's
// real open instant in UTC (for example "2024-01-02T05:00:00Z" for a
// 00:00 America/New_York session open in winter), not at literal
// midnight UTC the way Stooq's native "YYYY-MM-DD" dates already are.
// Storing that literal fetched instant verbatim would make Alpaca's
// raw partitions misaligned against USEquityCalendar's midnight-UTC D1
// anchor (built to agree with Stooq) on every single row. This package
// deliberately re-anchors: it converts the fetched timestamp to its
// trading date in America/New_York civil time, then stores Record.Time
// as that date at midnight UTC — see recordFromWireBar in wireshape.go.
// This makes both providers' D1 data normalize identically regardless
// of how each one originally timestamps a daily bar, and is exactly
// the kind of provider-specific convention difference ADR-047's
// internal-provider-seam design expects each provider package to
// absorb internally rather than leaking to marketdata.
package alpaca
