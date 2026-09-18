# strategysdk-minimal

The smallest useful out-of-tree Go strategy, built on `strategysdk`
(issue #381, ADR-062) — the guest-side counterpart to
`adapters/strategy/external.Host` (issue #379/#380).

It never imports Trader's `strategy`, `backtest`, `adapters`, or any
runtime/application package — only `strategysdk` itself, plus the
small shared value-type packages (`instrument`, `marketdata`, `num`,
`order`) `strategysdk.DescribedIntent` and friends are built from. See
`strategysdk/boundary_test.go` for the mechanically enforced version
of that same rule.

## What it does

`flipFlop` is a deliberately trivial strategy: flat on its first bar,
it enters long; once long, the next bar exits. It exists to show the
complete shape of a `strategysdk.Strategy` — `Describe`, `Start`,
`OnBar` — and the one-line `Serve()` call a real strategy author's own
`main()` needs. It is not a trading strategy anyone should run for
real.

## Author experience

```go
func main() {
    if err := strategysdk.Serve(newFlipFlop()); err != nil {
        log.Fatal(err)
    }
}
```

`Serve` reads the Unix-domain socket path from the
`TRADER_STRATEGY_SOCKET` environment variable — the exact contract
`adapters/strategy/external.Process` sets on a child process it
launches (ADR-063) — dials it, performs the Strategy Protocol v1
Handshake, and drives the strategy until the host ends the session or
this process receives SIGINT/SIGTERM.

## Building and running it directly

A Trader host normally launches this binary itself (via
`adapters/strategy/external.Launch`), setting `TRADER_STRATEGY_SOCKET`
automatically. To drive it manually against any `StrategyHostService`
implementation instead:

```sh
go build -o /tmp/flipflop ./examples/strategysdk-minimal

# with some process listening on /tmp/trader-strategy.sock and
# implementing trader.strategy.v1.StrategyHostService:
TRADER_STRATEGY_SOCKET=/tmp/trader-strategy.sock /tmp/flipflop
```

Without `TRADER_STRATEGY_SOCKET` set, or with nothing listening at
that path, it exits immediately with a clear error rather than
hanging — see `strategysdk.SocketPathEnv`'s own doc comment.
