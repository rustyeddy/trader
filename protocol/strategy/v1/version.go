package strategyv1

// ProtocolVersion is the current Strategy Protocol version string, the
// value both HandshakeRequest.protocol_version and
// HandshakeResponse.protocol_version carry for this package (doc.go's
// own "Versioning and compatibility" section). Both the host
// (ExternalStrategyAdapter, issue #379) and the guest (sdk,
// issue #381) use this constant rather than a hand-typed literal, so
// a future v2 package defines its own ProtocolVersion independently
// instead of every caller needing to know the current string.
const ProtocolVersion = "v1"
