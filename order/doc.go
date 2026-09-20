// Package order provides the supported vocabulary shared by external strategy
// intents and Trader's runtime: Side, PositionSide, and IntentKind (ADR-065).
// Side describes buy/sell transactions; PositionSide describes flat/long/short
// holdings. IntentKind names the requested action. Values expose String and
// Valid methods; zero Side and IntentKind values are invalid, while a zero
// PositionSide is Flat.
//
// Guest strategies describe actions through sdk. Canonical intents, proposals,
// requests, orders, fills, identity, and lifecycle transitions are private to
// Trader's runtime and are not exported by this package.
package order
