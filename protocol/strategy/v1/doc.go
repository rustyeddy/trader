// Package strategyv1 is the generated Protobuf/gRPC wire schema for
// Strategy Protocol v1 (issue #377, ADR-062:
// docs/arch/adr-062-external-strategy-protocol-v1.org).
//
// # Scope
//
// This package holds only strategy.proto and its generated Go code
// (strategy.pb.go, strategy_grpc.pb.go) plus this small set of
// hand-written helpers (this file, version.go). It defines the wire
// contract itself — it does not implement either side of it:
//
//   - The host side (ExternalStrategyAdapter, implementing the gRPC
//     server) is issue #379.
//   - The guest side (strategysdk, the gRPC client) is issue #381.
//   - The translation between these wire types and Trader's own
//     domain types (order.Intent, instrument.ID, num.Price, and so
//     on) is issue #378, in its own package — never here.
//
// # Dependency direction
//
// This package depends only on the generated Protobuf/gRPC runtime
// support (google.golang.org/protobuf, google.golang.org/grpc) and the
// standard library — never on any github.com/rustyeddy/trader/...
// package, domain or otherwise (ADR-062: "wire types are deliberately
// not aliases of domain types"). See boundary_test.go for the
// mechanical guard. A field here that looks like a domain concept
// (instrument_id, correlation_token, and so on) is always a plain
// scalar (string, int64, ...) carrying that concept's own wire
// representation, never the domain type itself.
//
// # Versioning and compatibility
//
// This is Strategy Protocol v1. Per ADR-062:
//
//   - Within v1, only additive, wire-compatible changes are permitted:
//     new optional fields, new enum values, new RPCs. A change that
//     would break an existing v1 client or server (removing/renaming a
//     field, changing a field's number or type, changing RPC semantics
//     incompatibly) requires a new v2 package
//     (github.com/rustyeddy/trader/protocol/strategy/v2), not a v1
//     revision.
//   - The gRPC service name (trader.strategy.v1.StrategyHostService)
//     embeds its own version, so a single host process can in
//     principle serve both v1 and a future v2 side by side during a
//     migration window.
//   - Handshake's own protocol_version field is the explicit runtime
//     version check: a host that does not support the guest's
//     requested version rejects the connection outright
//     (ERROR_CODE_PROTOCOL_VERSION_MISMATCH) rather than attempting to
//     silently degrade.
//
// See strategy.proto's own comments for the reasoning behind each
// message and RPC; this file does not repeat what is already
// documented there.
package strategyv1
