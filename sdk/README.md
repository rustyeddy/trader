# sdk

`sdk` is the guest-side Go SDK for writing an out-of-tree
Trader strategy that speaks [Strategy Protocol
v1](../protocol/strategy/v1) (issue #377, ADR-062) — the counterpart
to `adapters/strategy/external.Host`, the host-side gRPC server
(issue #379) a Trader process runs, and to
`adapters/strategy/external.Process`, which launches your strategy
binary and connects it (issue #380, ADR-063).

It hides all the protobuf/gRPC transport plumbing — dialing the host's
Unix-domain socket, performing the Handshake, driving the long-lived
`Run` stream, calling `GetHistoryBars` on your behalf — behind a small
interface that mirrors Trader's own in-process `strategy.Strategy`
contract closely enough that porting decision logic between the two is
mostly mechanical.

## Author experience

```go
package main

import (
    "log"

    "github.com/rustyeddy/trader/sdk"
)

func main() {
    if err := sdk.Serve(NewMyStrategy(cfg)); err != nil {
        log.Fatal(err)
    }
}
```

`Serve` reads the Unix-domain socket path from `TRADER_STRATEGY_SOCKET`
(set automatically by `adapters/strategy/external.Process` when it
launches your binary), dials it, and drives your strategy until the
host ends the session or your process receives SIGINT/SIGTERM.

See [`examples/sdk-minimal`](../examples/sdk-minimal)
for a complete, minimal, compiling example.

## Implementing a strategy

```go
type Strategy interface {
    Describe() Descriptor
    Start(ctx context.Context, env Environment) error
    OnBar(ctx context.Context, event BarEvent, view View) ([]DescribedIntent, []DescribedSignal, error)
}
```

`Describe` returns your strategy's name, version, and the
instrument/interval data it needs — carried to the host at Handshake,
so the host can answer its own `Describe()` with no further round
trip.

`Start` is called once, with an `Environment` carrying:

- `Clock` — reflects the *host's* own clock (real or, in a backtest,
  simulated), never your process's local wall clock. Always use
  `env.Clock.Now()`, never `time.Now()`, for anything that needs to
  agree with the host's own notion of "now."
- `RunID` — the host's run identifier, as a plain string.
- `Logger` — a ready-to-use `*slog.Logger`.

`OnBar` is called once per completed bar your strategy declared a
`DataRequirement` for. It returns the intents and signals you want
built — never real `order.Intent`/`journal.Record` values, which only
the host ever constructs (so a compromised or buggy guest can never
forge one). An empty slice for either is a valid, explicit "nothing
this bar" response.

### Describing an intent

```go
sdk.Enter(inst, order.Buy)
sdk.Exit(inst)
sdk.AdjustStop(inst, stopPrice)
sdk.EnterWithStop(inst, order.Buy, stopPrice)
sdk.TargetExposure(inst, order.Sell, quantity)
```

Each returns a `DescribedIntent`. To group several intents from one
`OnBar` call under the same correlation (for example, an exit paired
with a re-entry), chain `.WithCorrelation(token)` with the same
arbitrary string on each:

```go
[]sdk.DescribedIntent{
    sdk.Exit(inst).WithCorrelation("reversal"),
    sdk.Enter(inst, order.Sell).WithCorrelation("reversal"),
}
```

### Recording decision evidence

```go
sdk.Signal("mystrategy", map[string]string{
    "fast_sma": fastSMA.String(),
    "slow_sma": slowSMA.String(),
}).WithCorrelation(token)
```

The host journals this (when the run has a journal configured at all)
under the real `CorrelationID` it minted for the matching intent
group — matching `.WithCorrelation`'s own token to a
`DescribedIntent`'s.

### Reading history and the account

`View`, passed to `OnBar` (and `OnFill`, below), gives you:

```go
view.Account() AccountSnapshot
view.HistoryBars(inst, interval, n) (bars []marketdata.Bar, ok bool, err error)
```

`HistoryBars` is scoped to the exact requirements you declared in
`Describe`; asking for anything else returns `ok == false, err == nil`.
A non-nil `err` is a genuine transport/RPC failure talking to the
host, distinct from "not declared" — check `err` first.

**During `OnFill`, `HistoryBars` always returns `ok == false, err ==
nil`, never an RPC.** Strategy Protocol v1 scopes `GetHistoryBars` to
an in-flight `BarEvent` callback only (the same restriction the
in-process `strategy.FillHandler` boundary already has); there is no
separate fill-time history view.

### Handling fills (optional)

Implement `FillHandler` to receive fill notifications — `sdk`
detects this automatically and advertises the capability at Handshake:

```go
type FillHandler interface {
    OnFill(ctx context.Context, event FillEvent, view View) error
}
```

## What this SDK will never give you

By design, `sdk` never exposes a broker handle, a risk engine,
an execution pipeline, or Trader's own `strategy` package — the same
restrictions an in-process `strategy.Strategy` already has
(mechanically enforced by `sdk/boundary_test.go`). You
describe what you want; the host process decides whether, and how, it
actually happens.

## Testing your strategy

`sdk.ServeConn` is the lower-level entry point `Serve` itself
uses after dialing — it accepts any `grpc.ClientConnInterface`, so you
can drive your strategy against an in-process fake host (or the real
`adapters/strategy/external.Host`, over a `bufconn` listener) in your
own tests without a real socket. See `sdk`'s own
`serve_test.go` for the pattern.
