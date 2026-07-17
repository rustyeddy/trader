# signalreplay: backtesting scanner signals

`signalreplay` answers, empirically: does a "tradeable" classification from
`trader review` have edge? It replays a review sweep CSV through the
existing `backtest` runner using a deliberately naive mechanical entry (next
bar after the signal date), then joins each closed trade back to the sweep
row that produced it. The output is a per-trade dataset (R-multiple, close
cause, sweep features) suitable for downstream grading/threshold work.

This is analysis tooling, not a live trading strategy. See
[docs/signalreplay-spec.org](signalreplay-spec.org) for the full design.

## Workflow

```bash
# 1. Produce a sweep CSV (see docs/Review.org)
trader review --from 2019-01-01 --to 2024-12-31 --output csv > tradeables.csv

# 2. Generate a backtest config: one run per distinct instrument in the sweep
trader signalreplay gen \
  --signals tradeables.csv \
  --exit chandelier --exit-params atr_period=14,multiplier=2.0 \
  --timeframe D1 --source oanda \
  --risk-pct 0.5 \
  --out replay-chandelier.yml

# 3. Run it (writes JSON+org reports, same path as `trader backtest run`)
trader signalreplay run replay-chandelier.yml --report-dir ./reports

# 4. Join closed trades back to the sweep CSV into an outcome dataset
trader signalreplay report \
  --reports ./reports \
  --signals tradeables.csv \
  --out outcome.csv
```

`gen` and `run` are separate steps (a generated config is a committable,
re-runnable artifact — check it in) but `signalreplay run` can also generate
and execute in one step by passing `gen`'s flags directly instead of a config
path:

```bash
trader signalreplay run \
  --signals tradeables.csv --exit chandelier \
  --report-dir ./reports
```

## Episode semantics

Sort a sweep CSV's `tradeable`-bucket rows by date, per instrument.
Consecutive rows with the same `BIAS` and a gap of `episode-gap` calendar
days or less collapse into one episode: `(instrument, bias, first-date,
last-date)`. The first date is the signal date; entry is the open of the
first bar strictly after it.

Example (`episode-gap: 5`, EURUSD):

| Date       | Bias  | Episode                        |
|------------|-------|---------------------------------|
| 2024-01-02 | long  | E1 (first=01-02)                |
| 2024-01-03 | long  | E1 (gap 1d, merges)             |
| 2024-01-08 | long  | E1 (gap 5d, merges — boundary)  |
| 2024-01-15 | long  | E2 (gap 7d, exceeds threshold)  |
| 2024-01-22 | short | E3 (bias flip always splits)    |

Only one entry fires per episode by default (`one-per-episode: true`): once
an episode has opened a trade, it never re-enters, even if the position
later closes (stop-out) while the episode's window is technically still
open. `close-on-flip` closes the current position when a new episode with
the opposite bias activates, opening the new side in the same signal.

## signalreplay strategy params

| param              | type   | default     | meaning                                            |
|---------------------|--------|-------------|-----------------------------------------------------|
| `signals`           | string | (required)  | path to review sweep CSV                            |
| `entry`              | string | `next-open` | entry mode; v1 supports only `next-open`             |
| `episode-gap`        | int    | 5           | max calendar-day gap merging rows into one episode   |
| `max-hold-days`      | int    | 0           | 0 = unlimited; else time-stop after N bars           |
| `close-on-flip`      | bool   | true        | emit CloseAll when a new episode has opposite bias   |
| `one-per-episode`    | bool   | true        | at most one entry per episode (no re-entry)          |

The strategy never sets an initial stop itself — configure an `exit:` kind
in the generated config (e.g. `chandelier`); `signalreplay gen` refuses an
empty `--exit`.

## `gen` flags

| flag                | default    | meaning                                             |
|----------------------|------------|-------------------------------------------------------|
| `--signals`           | (required) | path to the sweep CSV                                 |
| `--exit`              | (required) | exit strategy kind                                     |
| `--exit-params`       |            | `key=value[,key=value...]`, values type-inferred        |
| `--timeframe`         | `D1`       | candle timeframe for every generated run                |
| `--source`            | `oanda`    | candle data source                                       |
| `--risk-pct`          | 0.5        | risk percent per trade                                   |
| `--starting-balance`  | 10000      | starting account balance                                  |
| `--warmup-days`       | 90         | candle history before the earliest signal date             |
| `--runout-days`       | 120        | candle history after the latest signal date, so late entries can resolve |

One `RunConfig` is emitted per distinct instrument in the sweep CSV, ordered
alphabetically for deterministic output. `Data.From`/`Data.To` span that
instrument's own earliest-to-latest signal date plus the warmup/runout
buffers — not the full CSV's date range.

## Outcome CSV columns

`signal_date, instrument, bias, entry_time, entry_price, initial_stop,
exit_time, exit_price, close_cause, pnl, r_multiple, hold_bars`, followed by
the sweep's feature columns (`ADX, CI, EMA SEP, EMA DIST, H4 ADX, H4 CI, H4
EMA DIST, Squeeze, W1 Bias, WEEK%, H1 Align, H1 EMA DIST`), joined on
`(instrument, signal_date)`.

`r_multiple` is signed and side-adjusted:
`(exit-entry)/(entry-initial_stop)` for longs, mirrored for shorts; 0 when
the stop distance is zero (stop-at-entry edge case).

`report` only emits rows for trades opened by the `signalreplay` strategy —
identified by the `signalreplay:<date>` marker in the closed trade's
`Reason` field — so a `--reports` directory containing other strategies'
runs is filtered down correctly.
