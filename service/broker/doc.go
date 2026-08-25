// Package broker is the application/service layer for Trader's broker
// account use cases (ADR-022, issue #154, M3-11).
//
// Service wraps a brokerpkg.Broker (github.com/rustyeddy/trader/broker)
// — the public broker port, never a specific adapter — and exposes
// account discovery, account snapshot, and order submit/cancel/replace
// as transport-neutral use cases. A caller (the CLI today, a future
// REST/WebSocket/SSE adapter) constructs a Service over whichever
// brokerpkg.Broker the composition root chose (the deterministic
// simulator, or eventually a real adapter such as OANDA); Service
// itself never imports or names any specific adapter, so it works
// identically regardless of which one is injected (ADR-008).
//
// Service never reaches into an adapter's own internal package, never
// formats a response, and never depends on a transport framework — see
// the service package's own doc comment for the full set of rules
// every service subpackage follows.
//
// # Account-scoped operations
//
// brokerpkg.Broker.OpenAccount is cheap and side-effect-free for every
// current adapter (ADR-007/ADR-008), so every account-scoped operation
// here (Snapshot, Submit, Cancel, Replace) opens the requested account
// fresh on each call rather than caching a brokerpkg.Account handle
// across calls — Service holds no per-account state of its own, only
// the brokerpkg.Broker it wraps. This keeps Service safe for
// concurrent use to exactly the extent the wrapped Broker's own
// concurrency contract already guarantees.
//
// # Scope
//
// Event streaming (brokerpkg.Account.Events) is deliberately not
// exposed here: a transport-neutral streaming use case is a
// sufficiently different shape (a live, held-open reader, not a single
// request/response) that it deserves its own deliberate design when a
// real transport consumer needs it, rather than being forced into this
// package's request/response convention now.
//
// # Logging and credential redaction
//
// Every Service operation logs exactly one structured completion or
// failure record (log.go, ADR-023, issue #156/M3-13), scoped with the
// canonical logging.ComponentBroker attribute and built only from
// typed request/result fields — account, order, and instrument
// identity, order status, a result count — never a whole request,
// response, or config value logged wholesale. Context-propagated
// correlation and causation (logging.WithCorrelationID/
// WithCausationID) reach every record automatically; Service has no
// correlation-specific code of its own.
//
// adapters/broker/sim (the only brokerpkg.Broker this package is
// exercised against today) carries no credentials at all, so no
// redaction concern exists yet in practice. A real broker adapter
// (OANDA, Milestone 5) will hold an API token or equivalent secret;
// when that lands, any adapter-level record that might otherwise
// include it must wrap the value with logging.Secret before it can
// reach a logger, exactly as ADR-023's own redaction mechanism
// requires — Service itself must keep logging only typed
// account/order/instrument fields, never a raw adapter config or
// credential value, so this boundary needs no adapter-specific
// redaction work of its own even once a real adapter exists. That
// adapter-level redaction work is explicitly deferred to Milestone 5;
// it is not part of this package's own scope.
package broker
