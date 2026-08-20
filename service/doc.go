// Package service is the root of Trader's application/service layer
// (ADR-022, issue #103; foundation established by issue #104).
//
// Transport adapters — the CLI today, REST/WebSocket/SSE later — are
// thin: they parse input, build a request type from a service
// subpackage, call one service operation, and format or encode the
// returned response. Every use case that coordinates calls into
// Trader's public/domain packages (marketdata, and later account,
// order, portfolio, and so on) lives in a subpackage of service, keyed
// by the domain area it orchestrates — for example service/marketdata.
//
// # Dependency direction
//
//	cmd/trader (or a future REST/WebSocket/SSE adapter)
//	   |
//	   v
//	service/<area>
//	   |
//	   v
//	<area> (public domain package, e.g. marketdata)
//	   |
//	   v
//	<area>/internal
//
// A service subpackage depends only on public domain packages, never on
// another package's internal/ tree — Go's own internal/ visibility rule
// already makes that a compile error for internal packages outside
// their own subtree (see docs/arch/package-boundaries.org), so no
// hand-written test is needed to enforce it. Domain packages never
// depend back on service: since a service subpackage already imports
// the domain package it orchestrates, any reverse import would be a
// compile-time import cycle, which the compiler rejects on its own.
//
// # What belongs in a service subpackage
//
// A service subpackage may:
//
//   - define request and response DTOs for its application use cases;
//   - coordinate calls across public Trader packages when a use case
//     requires it;
//   - translate application requests into domain types;
//   - return structured application results;
//   - propagate cancellation through context.Context.
//
// A service subpackage must not:
//
//   - contain trading or market-data business rules — those stay in the
//     domain package it orchestrates;
//   - know about CLI flags, positional arguments, or any other
//     transport-specific input shape;
//   - print to stdout or stderr;
//   - produce tables, Org output, JSON, HTML, or any other presentation
//     format;
//   - import net/http, a CLI framework (for example Cobra), gRPC,
//     WebSocket, or SSE packages, or any other transport framework;
//   - expose internal implementation details (provider-native types,
//     storage paths, internal caches) from the domain packages it
//     wraps.
//
// # Request/response convention
//
// Each service subpackage defines its own request and response types
// rather than sharing one generic envelope across domain areas. A
// request type is built by the transport adapter from already-parsed,
// already-validated transport input; it names domain types directly
// (for example instrument.ID, marketdata.Interval) and never a
// transport-specific representation (a raw string interval, an
// unparsed date, a CLI flag struct). A response type is a plain,
// structured value the caller can format however it needs; it is never
// pre-rendered as text, a table, or a specific encoding.
//
// service/marketdata's DatasetRequest is the first example of a common
// request shape reused by several operations in one subpackage; other
// subpackages are free to establish their own shared shapes as their
// own use cases require it, rather than being forced through one
// package-wide convention.
package service
