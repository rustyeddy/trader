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
fine when the flags or the environment supply everything.

| Option | Flag | Environment | Config key |
|---|---|---|---|
| data root | `--data-root` | `TRADER_DATA_ROOT` | `marketdata.root` |
| instruments | `--instrument` (repeat or comma-separated) | | `marketdata.instruments` |
| interval | `--interval` | | `marketdata.interval` |
| OANDA host | `--base-url` (download only) | | `marketdata.base_url` |
| OANDA token | none | `TRADER_OANDA_TOKEN` | none |

There is no default data root: with none configured the command stops and says so.
The **token is read only from `TRADER_OANDA_TOKEN`**; there is deliberately no flag
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

### Checking a live account

`make smoke` runs the opt-in live check: it downloads a small bounded range into a
temporary directory using your token from the environment, verifies what landed on
disk and prints only PASS or FAIL (details in `bin/smoke.log`). It is not part of
`make check`.

## Backtests

`trader` with no command runs the EMAX scenarios (one per `testdata/emax`
directory): a reversal exit, three stop cases (wick, cross, gap), a reversal and a
short stop. Each prints its bar count, orders and result. Scenario bars are
synthetic design data with a single price series, treated as the mid price. Running
a backtest on canonical bars is not available yet.
