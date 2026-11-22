# EMAX as an external strategy

The canonical example of a strategy that runs as its own process. It is the EMAX
strategy Trader has built in, written against the public Go SDK only (`strategy`,
`marketdata`, `num`), so it shows what an outside worker looks like: Trader sends
each group of completed bars over loopback TCP and JSON, the worker answers with the
orders it wants, and Trader sizes, admits, fills and journals them exactly as it
does for the built-in strategy.

| File | What it is |
|---|---|
| `main.go` | The program: flags, then `strategy.ListenAndServe`. |
| `emax/emax.go` | The strategy: `emax.New(cfg)` and `OnBars`, plus the EMA. |

## The rule

Long while the short EMA of the close is above the long one, short while it is
below. A change of side is a close on the crossover bar and the opposite open on the
next. Every open carries a stop a fixed fraction of the entry price away, rounded
down to the price tick (at least one tick). Each instrument has its own EMAs and
signal state. The strategy asks only; it never says how many units.

## Run it

Start the worker. It listens on a loopback address (a non-loopback one is refused)
and prints the address it bound:

```sh
go run ./examples/emax -addr 127.0.0.1:9100
```

| Flag | Default | Meaning |
|---|---|---|
| `-addr` | `127.0.0.1:0` | loopback address to listen on (`:0` picks a free port) |
| `-short`, `-long` | `2`, `4` | EMA windows (the built-in EMAX uses 2 and 4) |
| `-stop` | `0.01` | stop distance as a fraction of the price (1%) |
| `-tick` | `0.00001` | price tick of the instruments traded (EURUSD, GBPUSD) |

Then run a backtest that trades with it, in another terminal. The data is the
canonical bars in the store, as for any `trader backtest`:

```sh
trader backtest --worker 127.0.0.1:9100 \
    --instrument EURUSD,GBPUSD --interval h1 --from 2024-01-01 --to 2024-04-01
```

The worker serves one run and exits when Trader closes the connection. To compare
with the built-in strategy, run the same command without `--worker`: the summaries
are the same (see Parity).

Both are warmed up the same way. The built-in EMAX is prefilled with the month before
`--from`, when it is stored; the worker is sent those bars first, marked as warm-up, and
`Strategy.Warm` feeds its EMAs with them and nothing else (it takes no position, and the
worker answers a warm-up with no orders; one with an order ends the run). A worker that
does not register `strategy.WithWarmup` ignores them and starts cold.

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
```

A run whose worker disconnects or crashes is journaled too: `run.json` says
`failed` and why, and `events.jsonl` keeps what had happened until then.

## Parity

The worker must trade exactly as the built-in EMAX does. The root package's tests
hold it to that: the six EMAX scenarios, a 1,500-bar two-instrument walk, and a real
month built through the store (`service`) are each run with the built-in strategy and
with this worker over TCP from the same data, settings and account, and the
journals (every order transition, fill and protective exit, in order, to the price),
the summaries and the final positions must be identical. A run that disagrees is a
regression to explain, not a difference in protocol. The worker's EMA is a copy of
the built-in one (a worker cannot import Trader's private code), and a test checks the
two give the same value to the last bit.
