// Package tradertest provides deterministic public builders and
// assertions for testing code that consumes Trader's M1 domain packages
// (issue #31, M1-13): instrument, order, account, and portfolio.
//
// # Why this package exists
//
// By M1-12, every one of instrument, order, account, and portfolio had
// accumulated its own private, near-identical "build me a valid EUR/USD
// listing" or "build me a working order" test helper. tradertest exists
// to hold that boilerplate once, as public API, so an external consumer
// writing tests against Trader does not have to reinvent it — not to be
// a second, more convenient API layered on top of the real one.
//
// # What belongs here, and the rule for adding more
//
// A helper belongs in this package only if it is needed by an
// external-consumer test or replaces boilerplate that already existed,
// repeated, in M1's own tests. Concretely, that is:
//
//   - Instrument and listing builders (NewListing).
//   - Proposal, order, fill, and position builders (NewProposal,
//     NewOrder, NewFillFor, NewPosition).
//   - Account and portfolio snapshot builders (NewSnapshot,
//     NewPortfolio).
//   - Currency-aware exact-value assertions and a couple of order-state
//     assertions (AssertMoneyEqual and friends, AssertStatus,
//     AssertTerminal).
//
// Every builder here takes a small params struct with sensible defaults
// and returns the real domain type via the real domain constructor
// (order.NewProposal, account.NewSnapshot, and so on) — never a
// parallel object model, and never a value that bypasses the domain
// package's own validation. A Must-prefixed variant of each builder
// panics on error for terse test setup; the plain variant returns the
// error so a caller can deliberately build an invalid fixture and
// inspect what went wrong.
//
// # What does not belong here
//
// Deterministic time and identifiers are already public, already
// correct, and already exactly what every M1 package's own tests use
// directly: clock.NewSimulated for time, and
// id.NewGenerator(clock, id.NewDeterministic(seed1, seed2)) plus
// id.GenerateAccountID and friends for identifiers. tradertest does not
// wrap or re-export either — most builders here that need an identifier
// simply accept an *id.Generator, the same way the real constructors
// accept whatever identity a caller already has.
//
// marketdata.Quote and marketdata.Bar do not exist yet (M2, not M1), so
// there are no quote or bar fixtures here; adding them now would mean
// pre-designing M2's types from inside an M1 issue. In-memory historical
// sources, live feeds, simulated broker presets, strategy harnesses, and
// golden journal/report helpers are later-milestone tradertest
// responsibilities per the architecture document and do not belong in
// this package until the packages they build on exist.
package tradertest
