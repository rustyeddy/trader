// Package sdk is the guest-side counterpart to
// internal/adapters/strategy/external's host-side ExternalStrategyAdapter
// (issue #379, ADR-062): a small Go helper/runtime so an out-of-tree
// Go strategy process can speak Strategy Protocol v1
// (protocol/strategy/v1, issue #377) without hand-writing gRPC
// plumbing, dialing the host's Unix-domain socket, performing the
// Handshake, or driving the Run stream itself.
//
// The intended author experience is close to:
//
//	func main() {
//	    if err := sdk.Serve(mystrategy.New(cfg)); err != nil {
//	        log.Fatal(err)
//	    }
//	}
//
// sdk is the supported external strategy API (ADR-065). Strategies use it
// alongside public values and pure indicator/analysis packages. Trader's
// orchestration, broker, storage, identity, and test infrastructure are internal.
//
// # sdk.Strategy is its own interface, not strategy.Strategy
//
// sdk.Strategy mirrors strategy.Strategy's shape (Describe,
// Start, OnBar, plus a sdk.FillHandler equivalent of
// strategy.FillHandler) closely enough that porting an in-process
// strategy's decision logic to a v1 guest is mostly mechanical, but
// OnBar returns ([]DescribedIntent, []DescribedSignal, error), not
// ([]internal/order.Intent, error): a guest never mints a real internal/id.IntentID,
// id.EventID, or id.CorrelationID (ADR-005/ADR-062's own intent-
// construction-ownership section) — it only describes what it wants
// built, exactly the shape the Run stream's own wire messages already
// carry. DescribedIntent's own typed constructors (Enter, Exit,
// AdjustStop, EnterWithStop, TargetExposure) mirror
// strategy.IntentFactory's method names so the port reads
// recognizably similar, without a guest ever holding — or needing —
// a real IntentFactory.
//
// Environment is correspondingly smaller than strategy.Environment:
// no Intents or Journal field, since the guest never mints canonical
// intents or writes journal records directly. Environment.Clock
// reflects the host's own injected clock — seeded from SessionStart's
// own start_time and advanced to each BarEvent's own timestamp as it
// arrives — never this process's local wall clock, so a guest
// observes the identical clock semantics an in-process strategy
// already has under both a real and a simulated host clock (ADR-062's
// own corrected "clock ownership" section). Clock exposes only Now; host-driven
// guest timers are not a Protocol v1 capability (ADR-065).
//
// # Dependency direction
//
// sdk depends on protocol/strategy/v1 and on order,
// marketdata, instrument, and num for the shared value types
// DescribedIntent and the wire conversions are built from (order.Side,
// num.Price, num.Quantity, instrument.ID, marketdata.Bar/Interval) —
// never on internal/order.Intent or internal/order.NewIntent themselves, which
// sdk never calls. It never imports strategy, and never
// imports backtest, service, cmd, adapters, broker, execution, risk,
// pipeline, or chart — the identical forbidden set
// internal/strategy/boundary_test.go already enforces for in-process
// strategies (see boundary_test.go in this package), since a guest
// process has exactly the same restrictions an in-process strategy
// does. sdk and internal/adapters/strategy/external both depend on
// protocol/strategy/v1 and do the field-by-field wire translation
// independently in both directions — that translation logic is not
// shared code between host and guest, since each side owns a
// different half of the mapping. Neither package imports the other.
//
// # Socket discovery
//
// Serve reads the Unix-domain socket path from the
// TRADER_STRATEGY_SOCKET environment variable — the exact contract
// internal/adapters/strategy/external.Process sets on the child process it
// launches (ADR-063) — rather than importing that host-side package's
// own SocketPathEnv constant, for the same "neither package imports
// the other" reason above. This is the one place sdk reads
// the process environment directly; see internal/config/arch_test.go's own
// exemption list, which names this package for exactly this reason —
// Serve is this process's own composition root, structurally the
// guest-side equivalent of a cmd/ binary's main(), just packaged as
// an importable library because the real main() lives in an external
// strategy author's own repository, not this one.
package sdk
