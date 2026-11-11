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
// # Wire protocol: delegated to the official Alpaca Go SDK
//
// Issue #297's first implementation constructed and decoded Alpaca's
// historical-bars HTTP/JSON protocol by hand, and documented the
// result as a partially-verified assumption (a subsequent review, PR
// #312, confirmed the core request contract directly against Alpaca's
// documentation, but the response body shape beyond a few checked
// fields remained unverified against a real request). Issue #323
// replaced that hand-written acquisition path with the official
// module github.com/alpacahq/alpaca-trade-api-go/v3, which Alpaca
// itself maintains — resolving that risk by construction rather than
// by further manual verification. One concrete, verified correction
// this produced: the SDK's GetBars/GetMultiBars always call the
// multi-symbol /v2/stocks/bars?symbols=... endpoint (a
// map-of-symbol-to-bars response), never the single-symbol
// /v2/stocks/{symbol}/bars path endpoint (a flat array) the original
// hand-written client assumed — the two are genuinely different wire
// shapes, and this package now uses whichever one Alpaca's own SDK
// actually exercises.
//
// client.go owns retry/backoff policy and rate limiting — concerns the
// SDK does not manage the way this package needs (see Client's own doc
// comment for the resulting, deliberately coarser cancellation
// contract: the SDK's historical-bars methods accept no
// context.Context and own their own internal pagination loop, so
// ctx cancellation is checked only between whole fetch attempts, not
// within a single SDK call). wireshape.go isolates the one
// SDK-type-to-Record conversion this package still owns, including a
// documented, deliberate float64-to-num.Price quantization step (see
// its own doc comment) made necessary by the SDK's Bar type decoding
// prices directly into float64 with no original decimal text
// recoverable afterward. See ADR-050
// (docs/arch/adr-050-alpaca-historical-provider.org) for the original
// design discussion this issue builds on.
//
// # Timestamp normalization: trading date, not literal fetched instant
//
// Alpaca's bars times a daily bar at midnight America/New_York civil
// time, expressed in UTC (for example "2024-01-02T05:00:00Z" during
// EST) — not the 09:30 regular-session open, and not literal midnight
// UTC the way Stooq's native "YYYY-MM-DD" dates already are. Storing
// that literal fetched instant verbatim would make Alpaca's raw
// partitions misaligned against USEquityCalendar's midnight-UTC D1
// anchor (built to agree with Stooq) on every single row. This package
// deliberately re-anchors: it converts the fetched timestamp to its
// trading date in America/New_York civil time, then stores Record.Time
// as that date at midnight UTC — see recordsFromSDKBars in
// wireshape.go. This makes both providers' D1 data normalize
// identically regardless of how each one originally timestamps a daily
// bar, and is exactly the kind of provider-specific convention
// difference ADR-047's internal-provider-seam design expects each
// provider package to absorb internally rather than leaking to
// marketdata.
package alpaca
