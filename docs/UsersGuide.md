# Trader User's Guide

This guide lists what `trader` (the command in `cmd/trader`) does today, checked
against its `--help` output. Trader is under active development
([Project Status](../README.md#project-status)); commands and flags will change as
the milestones land. The guide for the earlier, larger command line is kept for
reference in [`_archive/docs/UsersGuide.md`](../_archive/docs/UsersGuide.md); it
does not describe this program.

## Building

```sh
make build            # writes bin/trader
go run ./cmd/trader   # or run without building
```

## Commands

| Command | What it does |
|---|---|
| `trader` | Backtests the six EMAX scenarios in `testdata/emax` and prints a summary of each. Deterministic: the output is the same every run. |
| `trader data download` | Downloads OANDA bid/ask candles into the raw store. Needs a token and the network. |
| `trader data build` | Builds canonical bars from the raw store, or SPY and QQQ daily bars from a Stooq file. Needs no token and makes no network call. |
| `trader data status` | Shows which canonical months a range can load (loaded, bad, missing, stale) without changing anything. Needs no token and makes no network call. |
| `trader backtest` | Backtests canonical bars from the data store: EMAX on each instrument, one shared account. Needs no token and makes no network call. |

Global flags, accepted by every command: `--log-level` (DEBUG, INFO, WARN, ERROR;
default INFO), `--log-format` (text or json) and `--log-output` (stderr, stdout or a
file to append to).

## Market data

Data files live under one root, the **data root**. For one instrument, interval
and month:

```
<data root>/oanda/raw/EURUSD/2024/01/EURUSD-2024-01-h1.csv        raw: OANDA's candles
<data root>/oanda/canonical/EURUSD/2024/01/EURUSD-2024-01-h1.csv  canonical: built from raw
```

Months are UTC months of the bar's opening time. Raw is the archive: it is only
ever added to. Canonical is derived from raw and can always be rebuilt.

### Configuration

Each option comes from, in order: the flag, the environment, then the config file
(`etc/trader.yml` by default, `--config` to name another; copy
[`etc/trader.example.yml`](../etc/trader.example.yml)). A missing default file is
fine when the flags or the environment supply everything. The data root has one
more step: `--data-root`, then `TRADER_DATA_ROOT`, then `marketdata.root`, then the
default `/srv/data`.

| Option | Flag | Environment | Config key |
|---|---|---|---|
| data root | `--data-root` | `TRADER_DATA_ROOT` | `marketdata.root` (default `/srv/data`) |
| results root | `--results-root` (backtest) | `TRADER_RESULTS_ROOT` | `results.root` (default `/srv/trading/backtest/results`) |
| instruments | `--instrument` (repeat or comma-separated) | | `marketdata.instruments` |
| interval | `--interval` | | `marketdata.interval` |
| OANDA host | `--base-url` (download only) | | `marketdata.base_url` |
| OANDA token | none | `TRADER_OANDA_TOKEN` | none |

The data root defaults to `/srv/data` (the `store` package's default), so a bare
`trader data download` writes into that directory: give `--data-root` or
`marketdata.root` to use another. The **token is read only from `TRADER_OANDA_TOKEN`**; there is deliberately no flag
and no config key for it, and a config file containing a secret-looking key is
rejected. The host defaults to OANDA's practice host, so a run with no `base_url`
can only read demo data; name the live host (`https://api-fxtrade.oanda.com`)
explicitly to download from a live account.

Set the token in your own shell without putting it on a command line, and unset it
afterwards:

```sh
read -rs TRADER_OANDA_TOKEN && export TRADER_OANDA_TOKEN
# ... run trader data download ...
unset TRADER_OANDA_TOKEN
```

### trader data download

```sh
trader data download --data-root /srv/data --instrument EURUSD,GBPUSD \
    --interval h1 --from 2024-01-01 --to 2024-04-01
```

`--from` and `--to` are UTC dates or RFC 3339 instants; the range is half-open
(`--to` is not included). Intervals: `m1`, `h1`, `h4`, `d1`. The scope is the 26
audited currency pairs.

- A complete row already on file is never changed: missing rows are added, and a
  file that would gain and change nothing is left alone. If the provider now reports
  different values for a stored complete row, the stored row is kept and the
  difference is counted and logged. A stored row that is still marked forming (an
  older download stored a few) is not a fact yet: once the provider reports the
  candle closed, the download replaces it with the closed candle, in place, and
  counts and logs it (`replaced N`, with the old and new values). A candle still
  forming at the provider is never stored.
- A candle the provider still reports as forming is never stored.
- Requests are paced, and a transient failure (429, 5xx, network) is retried with
  backoff. A rejected token (401) is retried only within the same attempt budget and
  then reported; a 403 is not retried.
- Output: one line per month, `<path>: fetched N, added N, kept N, not stored
  (incomplete) N`, then a total.

### trader data build

```sh
trader data build --data-root /srv/data --instrument EURUSD --interval h1 \
    --from 2024-01-01 --to 2024-04-01
```

Builds canonical-v2 files for `h1` or `d1` (ADR-090). For each month:

- a good bar gets its **mid** price (the exact midpoint of bid and ask) and a
  **modeled availability time**: the next interval boundary, for `d1` 17:00
  America/New_York (so a day is 23 or 25 hours across a daylight-saving change). A
  consumer may use a bar only from that time;
- a row that is not a good bar (still forming, off the interval boundary, crossed
  bid/ask, impossible shape, a duplicate or out of order) is **kept in the file as a
  bad record** with its reason and raw row number. A partition with bad records is
  not complete data;
- a month with no raw file is reported as missing and skipped; a month that cannot
  be built (for example a damaged raw file) is reported as failed and the rest carry
  on, with a non-zero exit at the end;
- a file already built from the same raw bytes is left alone; one from a different
  build, or in an old format, is replaced.

Output: one line per month (`<path>: built|unchanged|replaced, good N, bad N`, or
`no raw partition` / `failed: ...`), then `total: built N, unchanged N, replaced N,
missing raw N, failed N; good bars N, bad records N`.

`--source stooq` builds SPY or QQQ daily trade bars from a Stooq file instead, with its own
options and no date range: see [Stooq daily bars](stooq-daily.md).

### trader data status

```sh
trader data status --instrument EURUSD --interval h1 --from 2024-01-01 --to 2024-04-01
```

A read-only check of the canonical bars a range would load, with the same options as
`build` plus `--allow-bad` and `--allow-missing`. For each instrument it reads every
UTC month the range touches and reports:

- `loaded`: a valid file from the build in force with no bad records;
- `bad`: the file has bad records, so it is not complete data;
- `missing`: there is no canonical file for the month;
- `stale`: the file is not from the build in force (another availability policy,
  builder or validator version) or is not canonical-v2. A stale month is **never
  used**: rebuild it with `trader data build`.

It also checks the inside of the series: between its first and last bar every bar the
FX calendar expects must be there. A weekend is not a gap and is not reported. A gap
inside a year-end holiday (around December 25 and January 1) is reported as a
`closure` and is fine. A run on the reviewed list of bars OANDA does not have (it was
checked, and downloaded again, and the candles are not there) is reported as a
`known_hole` and is fine too. Any other missing bar, even one hour, is an
`unexplained` gap.
The ends of the range are not judged, since the newest candle may still be forming.

By default the command fails if any month is stale, missing or bad, or if the series
has an unexplained gap. `--allow-bad` accepts months with bad records (their good
bars load, and the month is still reported as bad); `--allow-missing` accepts a month
with no file and unexplained gaps (an allowed gap is still shown). Output: one line
per month (`<instrument> <year>-<month>: loaded, good N`, `bad, good N, bad N`,
`missing`, `stale: <reason>`), one line per gap (`<instrument>: gap closure|unexplained
<start> <end>, N bars`, UTC, the end exclusive), one line per instrument (`N bars` or
`not loadable`) and `total: loaded N, bad N, missing N, stale N; bars N; gaps: N
unexplained, N known closures, N known holes`.

`status` does not compare a file with the raw it was built from.

### Verifying canonical against raw

```sh
trader data verify --instrument EURUSD --interval h1 --from 2013-01-01 --to 2026-01-01
trader data verify --instrument EURUSD --interval h1 --from 2013-01-01 --to 2026-01-01 --sample 6
```

Read-only. Checks each canonical month against the raw partition it was built from,
row by row: every raw row is a bar or a bad record and every bar and bad record has
its raw row; a bar's bid, ask and volume equal the raw row's exactly (there is no
rounding to tolerate) and its mid is their midpoint; each bad record's reason is true
of its row; the file was built from the raw bytes now on disk by the builder in
force and is valid canonical-v2. Bad records the raw bears out (a forming candle, a
crossed quote and so on) are the expected differences: they are counted by reason,
not failed. A month with no raw, or raw but no canonical file, is reported, not
failed. The command exits non-zero if any month disagrees, printing each finding
(`fingerprint`, `unaccounted`, `unexpected`, `mismatch`, `unjustified`, `stale`,
`unreadable`) with its time and raw row. `--sample N` checks N months per instrument
spread from the first to the last of the range, each in full. A raw file edited or
extended after a build shows as a fingerprint finding plus the rows it changed:
rebuild with `trader data build`.

### Migrating the raw archive

```sh
trader data migrate                       # dry run: plan and checks, nothing moves
trader data migrate --apply --checksum    # move, then prove every file unchanged
trader data migrate --apply --reverse     # move everything back
```

The raw archive used to live in `<data root>/raw/<source>/...`; trader now reads and
writes `<data root>/<source>/raw/...`. `migrate` renames each source's directory whole
(no copying: instant, and every file keeps its inode and bytes). It is a **dry run**
unless you pass `--apply`. It refuses, and moves nothing at all, if a destination
already exists, a source holds a symbolic link, or it finds a temporary file (a
download or build is running or was interrupted: stop it first). After each move it
checks the files are the files that were there; `--checksum` also compares the SHA-256
of every file before and after (it reads all the data twice). If verification fails,
nothing is moved back for you: use `--reverse`.

Only raw moves. The old `canonical/<source>` files are canonical-v1; they are listed,
never touched, and rebuilt with `trader data build`. Delete them yourself once
`trader data audit` passes on the rebuilt data. The old layout is not read after the
move: there is no fallback.

### Serving the bars over HTTP

```sh
trader serve                      # read-only REST API on 127.0.0.1:8080
curl -s 'http://127.0.0.1:8080/v1/marketdata/bars?instrument=EURUSD&interval=h1&from=2024-05-20&to=2024-05-21'
```

`trader serve` exposes the canonical bars to other programs over `GET
/v1/marketdata/bars`, paged, with the coverage of what is missing in every response. It
reads only the canonical files and needs no provider credentials. It listens on
loopback only unless `TRADER_API_TOKEN` is set. The full contract, errors,
authentication and an integration example are in [the REST API guide](rest-api.md).

### Auditing a range

```sh
trader data audit --instrument EURUSD,GBPUSD --interval h1 --from 2013-01-01 --to 2026-01-01
trader data audit --instrument EURUSD --interval h1 --from 2013-01-01 --to 2026-01-01 --allow-missing --format json
```

One report for the whole range, read-only, no token or network. For each instrument
it shows every month (`loaded`, `bad`, `missing`, `stale`), the gaps inside the
series (`unexplained`, or a year-end `closure`; weekends are not reported) and the
cross-check of each canonical month against its raw (what `trader data verify`
does). The command exits non-zero when the data is not sound: a stale month, a bad
or missing month or an unexplained gap (unless `--allow-bad` / `--allow-missing`
accept them, as for `status`), or a file that disagrees with its raw. `--sample N`
limits the raw cross-check to N months per instrument; months and gaps always cover
the whole range, and the output says so next to the verdict (`note: sampled: the raw
cross-check covered N of M months`; JSON `sample`, `raw_checked`), because an OK then
certifies the raw cross-check only for those months. A month that has a canonical
file but no raw partition is `UNVERIFIABLE` and fails the audit: the audit does not
certify what it cannot check (`trader data verify` only reports it). `--format json` prints the entire report as one JSON object (also
when the audit fails, so a tool can read why); the text output ends with
`audit: OK` or `audit: FAILED`.

Real EURUSD h1 2013-2025 audits as: 156 months loaded, 23 known closures, 10
unexplained gaps (one or two hours each), all 156 months verified against raw; it
passes with `--allow-missing`.

### Checking a live account

`make smoke` runs the opt-in live check: it downloads a small bounded range into a
temporary directory using your token from the environment, verifies what landed on
disk and prints only PASS or FAIL (details in `bin/smoke.log`). It is not part of
`make check`.

## trader backtest

```sh
trader backtest --instrument EURUSD,GBPUSD --interval h1 --from 2024-01-01 --to 2024-04-01
```

Backtests the canonical bars of one or more instruments over a UTC range on one
shared account. A run has one source (`--source`, only `oanda`) and one interval.
Bars are loaded as `trader data status` describes, with the same options and the same
`--allow-bad` and `--allow-missing`; by default a stale, missing or bad month, or an
unexplained gap inside the series, fails the run, and nothing is replayed unless every instrument loaded. Every bar available
at the same instant reaches the strategy together; an instrument with no bar at that
instant is absent, never carried forward.

The strategy and account are fixed, the ones the EMAX scenarios use: EMAX (windows 2
and 4, a 1% stop) on each instrument and a 10,000 USD account. The engine trades the
instruments it has a market for (EURUSD and GBPUSD today); another instrument is
refused with an error naming it.

Indicators are prefilled: the bars of the UTC month before `--from`, when they can be
loaded, only warm the strategy up (they are not traded and not counted). A missing
month is simply not there; if the earlier months are stale, or bad and `--allow-bad`
is not given, the run goes without a warm-up (the log says so). The engine rounds
prices to the nearest tick of the market (a mid between two ticks rounds up); the
stored data is not changed.

Output: the engine's summary for the run (bars, orders, trades, money, open
positions), then `run <RunID>: <directory>`.

### An external strategy

`trader serve` also accepts strategy clients, on by default at `127.0.0.1:9100`
(`--strategy-listen`; an empty value turns it off; loopback only). A strategy is a
separate process that connects, asks for one backtest (instruments, interval, range),
trades it and receives the result, and Trader stays up for the next client.

```sh
trader serve                                  # terminal 1: Trader, staying up
go run ./examples/emax -instrument EURUSD,GBPUSD -interval h1 \
    -from 2024-01-01 -to 2024-04-01           # terminal 2: a strategy client
```

Trader loads the bars, sends them over TCP and JSON group by group, and the strategy
answers with the orders it wants (it never says how many units); Trader sizes, admits,
fills and journals them as it does for the built-in strategy, under the results root
(`--results-root` on `trader serve`, like `trader backtest`). The strategy is warmed up
like the built-in one: the month before the range is sent first, marked as warm-up, for
it to prefill its indicators; nothing is traded on it, and a client that answers it with
an order fails the run. The client gets the RunID as soon as the run exists and, at the
end, the result: RunID, status and the summary. Trader runs one backtest at a time: a
second client waits until the first is done and gives up after its own timeout. A client
that disconnects, times out or sends something invalid ends only its own run, journaled
as `failed` with the events recorded until then, and the server goes on. Interrupting
`trader serve` cancels the run in progress, journals it as `cancelled`, and exits.
[`examples/emax`](../examples/emax/README.md) is the canonical client (the same EMAX,
written against the public SDK); its README says how to run it and read the journal.

### Run settings

`run.json` has a `settings` object from the moment the run exists: the risk and account
settings the engine sizes and admits the run's orders under. It stays however the run
ends, so a failed or cancelled run says what it ran under too (one that stopped before
trading says what it would have traded under). The summary of a completed run, both the
`summary` a strategy client receives and `result` in `run.json`, repeats it as
`settings`. They are read-only facts, which no client or flag sets. The starting balance
and currency are the summary's `initial_balance`.

```json
"settings": {
  "risk_per_trade": "0.01",
  "max_order_notional": {"amount": "50000000", "currency": "USD"},
  "margin_policy": "default",
  "margin_closeout_level": "0.5",
  "instruments": [
    {"instrument": "EURUSD", "margin_rate": "0.02", "min_units": "1", "unit_step": "1"},
    {"instrument": "GBPUSD", "margin_rate": "0.05", "min_units": "1", "unit_step": "1"}
  ]
}
```

| Field | Meaning |
|---|---|
| `risk_per_trade` | fraction of equity an open is sized to lose at its stop; planned risk, not a cap on the loss |
| `max_order_notional` | largest notional of one opening order |
| `margin_policy` | `default`: the engine's liquidation rules (below the closeout level, one position at a time, worst loser first); `custom`: any of those replaced. Every run Trader makes is `default`: no option replaces them, only the engine's own code and tests |
| `margin_closeout_level` | equity as a fraction of margin in use below which positions are liquidated; absent if the broker decides that by a custom policy |
| `instruments` | every market the engine could size an order for, which can be more than the run asked for |
| `instruments[].margin_rate` | fraction of notional held as margin |
| `instruments[].min_units`, `unit_step` | smallest size of an open, and the step a size is rounded down to |
| `instruments[].max_units` | largest size of one order; absent when there is no limit |

There is no aggregate (portfolio) limit and no risk policy version or settings hash:
Trader has neither. A strategy's own parameters, such as its stop distance, are not
settings of the run. Runs made before the field existed have none: treat an absent
`settings` as unknown.

### Results

Every run is journaled, whether it completes, fails or is cancelled. It gets a RunID
(`run_` and a sortable ULID that carries the time it began) and a directory of that
name under the results root (`--results-root`, then `TRADER_RESULTS_ROOT`, then
`results.root`, then `/srv/trading/backtest/results`; the root is created if it is
missing). Three files are enough to read a run:

- `run.json`: the RunID, kind (`backtest`), strategy, source, interval, instruments,
  range, [`settings`](#run-settings), wall-clock `started_at` and `ended_at`, `status` (`running` while it runs,
  then `completed`, `failed` or `cancelled`), `error` and, for a completed run, the
  engine's `result` (orders, trades, balance, equity, open positions, and the
  metrics below). A run that was cut off, say by a crash, still says `running`.
- `events.jsonl`: one JSON line for every order transition, in the order it
  happened: `requested` (what was asked: instrument, action, direction, type, price,
  stop, target), `sized` (the account's size; for a market open whose `price` differs
  from the decision bar's close, which it is sized from, that close as `price`), `submitted` (the broker's id), `filled`
  (units, fill price, and what the order was), `rejected` or `cancelled` (with the
  reason). The broker's own orders, protective stops, targets and margin closeouts,
  are in it with their `cause`. Lines carry the simulated time only, so two runs that
  trade the same way write the same lines. A failed run keeps the events it had.
- `account.jsonl`: the account's equity over time, one JSON line per observation, in
  order. Copy it unchanged to keep the evidence; it needs nothing of Trader to read.

```json
{"seq":1,"time":"2024-01-01T22:00:00Z","kind":"initial","account":"sim123","balance":{"amount":"10000","currency":"USD"},"unrealized":{"amount":"0","currency":"USD"},"equity":{"amount":"10000","currency":"USD"},"open":0}
{"seq":3,"time":"2024-01-02T00:00:00Z","kind":"frame","account":"sim123","balance":{"amount":"10000","currency":"USD"},"unrealized":{"amount":"7.06446","currency":"USD"},"equity":{"amount":"10007.06446","currency":"USD"},"open":1}
```

  The first line (`kind` `initial`) is the account before anything trades, at the
  opening time of the first traded frame (warm-up bars are not traded and not
  observed). Every other line (`frame`) is the account after one replayed frame, at
  that frame's availability time, once its fills, marks and margin rules have settled;
  with several instruments there is one coherent line per frame, never one per
  instrument. `balance` is realized cash, `unrealized` the open positions' P&L at their
  latest mark, `equity` their sum; all are exact amounts in the account's currency,
  and `open` counts the open positions. A frame holds only the instruments that have a
  bar then. An open position whose instrument has none is not marked again, and its
  instrument is listed in `stale` (omitted when empty), so an unchanged value is told
  from a fresh one; no mark is made up.

The summary in `result` keeps its earlier fields and adds [`settings`](#run-settings)
and, all computed from the
account observations and the run's trade counts (nothing else is measured):

```json
"net_return_pct": "1.5045263",
"win_rate": "0.32608696",
"max_drawdown": {
  "amount": {"amount": "218.501", "currency": "USD"},
  "pct": "2.15839646",
  "peak_equity": {"amount": "10123.30236", "currency": "USD"},
  "trough_equity": {"amount": "9904.80136", "currency": "USD"},
  "peak_time": "2024-01-05T16:00:00Z",
  "trough_time": "2024-01-23T03:00:00Z"
},
"max_percent_drawdown": {
  "amount": {"amount": "218.501", "currency": "USD"},
  "pct": "2.15839646",
  "peak_equity": {"amount": "10123.30236", "currency": "USD"},
  "trough_equity": {"amount": "9904.80136", "currency": "USD"},
  "peak_time": "2024-01-05T16:00:00Z",
  "trough_time": "2024-01-23T03:00:00Z"
}
```

- `net_return_pct`: (final equity - initial equity) / initial equity, in percent
  (`1.5` is 1.5%). Equity includes open positions at their latest mark.
- `win_rate`: `wins / trades` as a fraction, `null` when no trade closed. A trade is
  a closed position; break-even trades count in `trades`, not in `wins`.
- `max_drawdown` and `max_percent_drawdown`: mark-to-market equity drawdowns, not
  balance drawdowns, so open-position losses count. Replaying `account.jsonl`: the
  peak is the highest `equity` so far (the first line takes part, and a later equal
  equity does not move the peak time), the depth is peak minus `equity`, and the
  percentage is the depth over that peak, to eight places.
  - `max_drawdown` is the fall with the largest `amount`. Its `pct` belongs to that
    fall and is not necessarily the run's largest percentage.
  - `max_percent_drawdown` is the fall with the largest `pct`: use it for the maximum
    percentage drawdown. Its `amount` belongs to that fall.

  For equity `100, 80, 200, 170`, `max_drawdown` is 30 (15%, from 200) and
  `max_percent_drawdown` is 20% (20, from 100). The two are often the same fall, as
  above. Each takes the first of equals and has that fall's peak and trough `time`
  and equity. A run that never falls (no trades, or only rising equity) reports zero,
  and the peak and trough are the first line. A fall that never recovers still
  counts. Equity is sampled at frame close, not intrabar: an excursion inside a bar
  is not seen. Missing marks are never replaced, and an observation that cannot be
  valued fails the run rather than giving a statistic. The last line equals the
  summary's final `balance`, `unrealized` and `equity`; the run fails if it does not.

```sh
ls /srv/trading/backtest/results                       # one directory per run
jq . /srv/trading/backtest/results/run_*/run.json      # status, error, settings, result
jq -c 'select(.state=="filled")' /srv/trading/backtest/results/run_*/events.jsonl
```

## Backtests

`trader` with no command runs the EMAX scenarios (one per `testdata/emax`
directory): a reversal exit, three stop cases (wick, cross, gap), a reversal and a
short stop. Each prints its bar count, orders and result. Scenario bars are
synthetic design data with a single price series, treated as the mid price. Running
a backtest on canonical bars is not available yet.
