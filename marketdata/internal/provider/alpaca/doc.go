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
// # A partially-verified API-shape assumption — read before trusting this package's request/response wiring
//
// Nothing in this repository, and no live network access in the
// environment this package was originally written in, could verify
// Alpaca's real Market Data API v2 wire contract directly. This
// package's request construction and response parsing (see client.go
// and wireshape.go) were built against general, publicly documented
// knowledge of that API's shape. A subsequent code review (PR #312)
// confirmed the core request contract this package assumes against
// Alpaca's current official documentation directly — the single-symbol
// bars endpoint, timeframe=1Day, start/end, limit, page_token,
// adjustment=split, and feed=iex are all real, verified values, not
// guesses. The response *body* shape this package decodes against
// remains the still-unverified part: field names/types beyond what
// review specifically checked, error-response bodies, and pagination
// edge cases (an empty final page, a malformed token) have not been
// exercised against a real request. wireshape.go isolates every
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
// Alpaca's assumed JSON bar shape times a daily bar at midnight
// America/New_York civil time, expressed in UTC (for example
// "2024-01-02T05:00:00Z" during EST) — not the 09:30 regular-session
// open, and not literal midnight UTC the way Stooq's native
// "YYYY-MM-DD" dates already are.
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
