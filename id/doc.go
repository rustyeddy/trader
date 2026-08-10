// Package id implements Trader-owned identifiers and correlation metadata,
// as decided by issue #24 (M1-06).
//
// # Six identifier kinds
//
// RunID, OrderID, FillID, EventID, CorrelationID, and AccountID are each a
// distinct Go type — a RunID can never be assigned where an OrderID is
// expected, caught at compile time, not at runtime. Internally they share
// one generic implementation (ID[K], in id.go) rather than six hand-copied
// ones, but nothing about that is visible at a call site: id.RunID is used
// exactly like any other named type.
//
// This is deliberately fewer kinds than issue #24 originally proposed; see
// "Deferred identifiers" below for which ones were cut, and why.
//
// Each kind's canonical string form carries a short prefix identifying it —
// "run_01J8Z3K3R2N4XG9YB6HFA1V7ZQ", "ord_01J8Z3K5H7T1MDCE9WNRP2VXY0" — so a
// stray ID string in a log line or database column is identifiable on
// sight, without cross-referencing code. The 26 characters after the
// prefix are a ULID (https://github.com/ulid/spec): a 48-bit millisecond
// timestamp followed by 80 bits of entropy, Crockford Base32 encoded. ULID
// is an internal encoding choice — see id/internal/ulid — never a public
// dependency; nothing in this package's exported API exposes a raw ULID
// value, a third-party ULID type, or the underlying [16]byte
// representation.
//
// # Construction
//
// Parse and the concrete Parse<Kind> functions (ParseRunID, ParseOrderID,
// ...) validate external or persisted text. Generate and the concrete
// Generate<Kind> functions produce new identifiers from a Generator. There
// is no way to construct a non-zero ID holding an arbitrary value: every
// path validates the kind prefix and the ULID body, or comes from a
// Generator that owns real entropy and a real clock.
//
// # Trader-owned versus broker-native identifiers
//
// A broker's own identifier for an order, account, or fill is not an
// id.OrderID or id.AccountID: broker identifiers are opaque strings with
// no ULID structure. Parsing one as a Trader ID simply fails — that
// structural rejection is what keeps the two from being confused, rather
// than a parallel BrokerOrderID type defined here. Packages that talk to a
// specific broker define their own native-identifier types (BrokerOrderID,
// BrokerAccountID, ...) alongside whatever mapping they need between the
// two; that mapping is a broker-adapter concern, not this package's.
//
// # AccountID has a different lifecycle
//
// Every other kind is generated fresh for the event it identifies: a new
// RunID per run, a new OrderID per order. AccountID is not: it is
// generated once when the account entity is created and then persisted
// for that account's lifetime, never regenerated on a later run.
//
// # Deferred identifiers
//
// Issue #24 originally proposed SessionID, IntentID, ProposalID, and
// InstrumentID alongside the six kinds this package actually exports.
// They were cut from the initial scope on review, using this rule of
// thumb: would two otherwise-identical instances of this thing ever need
// to coexist and be distinguished by Trader? That is clearly true for
// runs, orders, fills, and accounts. It was not yet true for sessions,
// intents, and proposals — none of those have a concrete, persisted
// domain object in the codebase yet to justify a generated identity for.
// They will be added, alongside their corresponding domain objects, when
// that changes.
//
// InstrumentID was cut for a different reason: an instrument has a
// natural, canonical identity (EUR/USD, an exchange/symbol/asset triple,
// ...), not an arbitrary generated one. A ULID-based InstrumentID would
// have given EUR/USD a fresh identity every run, which is exactly wrong.
// Instrument identity is expected to come from a canonical instrument
// registry once the architecture document's instrument package exists,
// not from this package.
//
// # Generation
//
// Generator produces monotonic ULIDs from an injected clock.Clock and an
// entropy EntropySource, per ADR-015: it never calls time.Now itself.
// Multiple identifiers generated within the same millisecond receive
// strictly increasing entropy rather than fresh random values, preserving
// lexicographic sort order at millisecond resolution; see generate.go.
// Random is the production EntropySource; Deterministic is a seeded,
// injectable EntropySource for tests and replay — given the same simulated
// clock and seed, it produces the same sequence of identifiers every time.
// (EntropySource, not Source: Source is a separate type below, for
// Metadata's originating-component label — the two are unrelated despite
// the similar name.)
//
// # Zero value
//
// The zero value of every ID kind is unset, not the identifier that
// happens to encode sixteen zero bytes: IsZero reports true for it,
// String names it visibly ("<unset id>") instead of returning something
// that could pass for real output, and MarshalText/MarshalJSON reject it
// outright. Parse rejects any input that would decode to the all-zero
// value for the same reason — that bit pattern is permanently reserved for
// "unset."
//
// # Correlation metadata
//
// Metadata bundles the fields needed to trace one identifier through a
// multi-stage workflow: EventID identifies this event, CorrelationID is
// shared across every stage of the workflow, CausationID names the
// specific EventID that directly caused this one, Timestamp is the
// canonical UTC event time, and Source names the originating component.
// A typical intent-to-fill chain shares one CorrelationID throughout while
// each stage's CausationID points at the EventID immediately before it:
//
//	Intent:   correlation = workflow ID, causation = zero (nothing caused it)
//	Proposal: correlation = same workflow ID, causation = intent's EventID
//	Order:    correlation = same workflow ID, causation = proposal's EventID
//	Fill:     correlation = same workflow ID, causation = order's EventID
//
// See metadata.go.
package id
