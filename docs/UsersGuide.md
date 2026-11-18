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
| `trader data build` | Builds canonical bars from the raw store. Needs no token and makes no network call. |
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

- Rows already on file are never changed; only missing rows are added, and a file
  that would gain nothing is left alone. If the provider now reports different
  values for a stored row, the stored row is kept and the difference is counted and
  logged.
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
`closure` and is fine. Any other missing bar, even one hour, is an `unexplained` gap.
The ends of the range are not judged, since the newest candle may still be forming.

By default the command fails if any month is stale, missing or bad, or if the series
has an unexplained gap. `--allow-bad` accepts months with bad records (their good
bars load, and the month is still reported as bad); `--allow-missing` accepts a month
with no file and unexplained gaps (an allowed gap is still shown). Output: one line
per month (`<instrument> <year>-<month>: loaded, good N`, `bad, good N, bad N`,
`missing`, `stale: <reason>`), one line per gap (`<instrument>: gap closure|unexplained
<start> <end>, N bars`, UTC, the end exclusive), one line per instrument (`N bars` or
`not loadable`) and `total: loaded N, bad N, missing N, stale N; bars N; gaps: N
unexplained, N known closures`.

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
positions).

## Backtests

`trader` with no command runs the EMAX scenarios (one per `testdata/emax`
directory): a reversal exit, three stop cases (wick, cross, gap), a reversal and a
short stop. Each prints its bar count, orders and result. Scenario bars are
synthetic design data with a single price series, treated as the mid price. Running
a backtest on canonical bars is not available yet.
