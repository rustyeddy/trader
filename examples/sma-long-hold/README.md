# sma-long-hold

A real, non-trivial out-of-tree strategy built on `strategysdk`
(issue #383, ADR-062) — the "prove Strategy Protocol v1 with something
more than hello-world" reference implementation, alongside
[`examples/strategysdk-minimal`](../strategysdk-minimal)'s
deliberately trivial flip-flop strategy.

It reproduces `strategy/smatrend`'s own default configuration — be
long only above a simple moving average, protected by a monotonic
trailing stop, entering only on a genuine cross from at-or-below the
SMA to strictly above it — never the full pluggable
ExitRule/ReEntryRule/InitialEntryRule surface `strategy/smatrend`
itself supports. See `main.go`'s own doc comment for exactly why the
default scenario needs none of that rule-selection machinery at all.

It never imports Trader's `strategy`, `backtest`, `service`, `cmd`,
`adapters`, `broker`, `execution`, `risk`, or `pipeline` packages —
mechanically enforced by `boundary_test.go` — so it can never place a
broker order, evaluate risk, or otherwise act outside describing
intents through `strategysdk`.

## Equivalence with the in-tree strategy

`cmd/trader/backtest/sma_long_hold_equivalence_test.go` runs this
binary (via `--strategy-exec`) and `strategy/smatrend` (in-process,
`ExitRuleName: "trailing-stop"`, `ReEntryRuleName: "fresh-cross"`,
`InitialEntryModeName: "fresh-cross"`) side by side over identical
canonical market data and asserts their trades and final account state
are identical — the concrete proof that an out-of-tree
`strategysdk.Strategy` produces the same trading decisions as its
in-tree `strategy.Strategy` counterpart when they implement the same
logic.

## Configuration

```sh
go build -o /tmp/sma-long-hold ./examples/sma-long-hold
```

With no `TRADER_STRATEGY_CONFIG` set, it defaults to EUR/USD H1 (the
same convention `strategysdk-minimal` uses), `sma_period: 20`,
`trailing_stop_percent: "0.10"`. Set `TRADER_STRATEGY_CONFIG` to a
JSON file to override any of these:

```json
{
  "base": "EUR",
  "quote": "USD",
  "interval_unit": "hour",
  "interval_count": 1,
  "sma_period": 20,
  "trailing_stop_percent": "0.10"
}
```

## Running it through "trader backtest run"

```sh
trader backtest run \
  --strategy-exec /tmp/sma-long-hold \
  --strategy-config sma-long-hold.json \
  --symbol EURUSD --interval H1 \
  --from 2024-01-01 --to 2024-06-01 \
  --adverse-distance 0.01000 \
  --data-raw-root /srv/trading/data/raw/oanda
```

As with every `--strategy-exec` run (issue #382), this binary's own
`Describe()` — not `--symbol` — determines the actual replay universe;
`--symbol`/`--interval` here only control what canonical data `run`
publishes beforehand, which must cover whatever `TRADER_STRATEGY_CONFIG`
(or the defaults) will actually request.
