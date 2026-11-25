# EMAX as an external strategy

The canonical example of a strategy that runs as its own process. It is the EMAX
strategy Trader has built in, written against the public Go SDK only (`strategy`,
`marketdata`, `num`, `indicators`), so it shows what an outside strategy looks like:
it connects to a running Trader, asks for one backtest, and Trader sends each group of
completed bars over loopback TCP and JSON, the strategy answers with the orders it
wants, and Trader sizes, admits, fills and journals them exactly as it does for the
built-in strategy. At the end the strategy receives the result.

| File | What it is |
|---|---|
| `main.go` | The program: flags, then `strategy.Run`. |
| `emax/emax.go` | The strategy: `emax.New(cfg)`, `OnBars` and `Warm`, using `indicators.EMA`. |

## The rule

Long while the short EMA of the close is above the long one, short while it is
below. A change of side is a close on the crossover bar and the opposite open on the
next. Every open carries a stop a fixed fraction of the entry price away, rounded
down to the price tick (at least one tick). Each instrument has its own EMAs and
signal state. The strategy asks only; it never says how many units.

## Run it

Start Trader. It accepts strategy clients on `127.0.0.1:9100` by default (loopback
only) and stays up between runs:

```sh
trader serve
```

Then, in another terminal, run the strategy. It asks for the backtest, trades it and
prints the result. The data is the canonical bars in Trader's store, as for any
`trader backtest`:

```sh
go run ./examples/emax -instrument EURUSD,GBPUSD -interval h1 \
    -from 2024-01-01 -to 2024-04-01
```

| Flag | Default | Meaning |
|---|---|---|
| `-trader` | `127.0.0.1:9100` | Trader's strategy address (`trader serve --strategy-listen`), loopback only |
| `-instrument`, `-interval` | `EURUSD`, `h1` | what to trade; h1 or d1 |
| `-from`, `-to` | required | the range, UTC, `to` exclusive: `YYYY-MM-DD` or RFC 3339 |
| `-short`, `-long` | `2`, `4` | EMA windows (the built-in EMAX uses 2 and 4) |
| `-stop` | `0.01` | stop distance as a fraction of the price (1%) |
| `-tick` | `0.00001` | price tick of the instruments traded (EURUSD, GBPUSD) |
| `-wait` | `30s` | how long to wait for Trader to accept the run when it is busy with another |

Trader runs one backtest at a time; a second client waits (up to its `-wait`) until the
first is done. The strategy exits when the run does. To compare with the built-in
strategy, run `trader backtest` with the same instruments and range: the summaries are
the same (see Parity).

Both are warmed up the same way. The built-in EMAX is prefilled with the month before
the range, when it is stored; the strategy is sent those bars first, marked as warm-up,
and `Strategy.Warm` feeds its EMAs with them and nothing else (it takes no position,
and the strategy answers a warm-up with no orders; one with an order ends the run). A
strategy that does not register `strategy.WithWarmup` ignores them and starts cold.

## Where the results are

Every run is journaled under the results root (`--results-root`, `TRADER_RESULTS_ROOT`,
`results.root`, default `/srv/trading/backtest/results`), in a directory named by its
RunID. The command prints it at the end:

```text
run run_01KB5Q3V7T2N4XG9YB6HFA1V7Z: /srv/trading/backtest/results/run_01KB5Q3V7T2N4XG9YB6HFA1V7Z
```

```sh
dir=/srv/trading/backtest/results/run_01KB5Q3V7T2N4XG9YB6HFA1V7Z
jq '{status, error, strategy, result: .result.orders}' $dir/run.json   # transport tcp, status completed
jq -c 'select(.state=="filled")' $dir/events.jsonl                      # every fill, stops included
jq '.result | {max_drawdown, max_percent_drawdown}' $dir/run.json       # largest falls, with times
jq -c '[.time, .equity.amount]' $dir/account.jsonl | head               # equity after each frame
```

A run whose client disconnects or crashes is journaled too: `run.json` says
`failed` and why, and `events.jsonl` keeps what had happened until then.

## Parity

The strategy must trade exactly as the built-in EMAX does. The root package's tests
hold it to that: the six EMAX scenarios, a 1,500-bar two-instrument walk, and a real
month built through the store (`service`) are each run with the built-in strategy and
with this strategy over TCP from the same data, settings and account, and the
journals (every order transition, fill and protective exit, in order, to the price),
the summaries and the final positions must be identical. A run that disagrees is a
regression to explain, not a difference in protocol. Both use the one
EMA in the public `indicators` package, so the indicator is not a place they can differ;
the rule itself (signal, flip, stop) is written twice, and these tests keep the copies
the same.
