# Equity and ETF data inventory (issue #728)

Status: discovery only. Nothing under `/srv/data` was modified, moved, downloaded
or ingested. Inspected 2026-11-24 on the development machine against Trader
`main` at `6f4c276`. Every number below was read from the files named here; what
could not be verified is marked **unknown**.

## Summary

- There is **no equity canonical data anywhere on disk**. `/srv/data/stooq/canonical`
  and `/srv/data/alpaca/canonical` do not exist; the only canonical-v2 data is
  OANDA FX (`/srv/data/oanda/canonical`).
- Existing equity/ETF data is raw only: the Stooq `d_us_txt.zip` / `h_us_txt.zip`
  bundles (all US symbols), one Stooq `unzipped` stage for SPY D1 only, three
  loose Stooq daily CSVs (SPY, QQQ, AAPL), and Alpaca IEX-feed D1 raw partitions
  for 15 symbols.
- **Trader `main` cannot read or ingest any of it.** The Stooq and Alpaca providers,
  `USEquityCalendar` and the equity ADRs' code live only in `_archive/`. The live
  build, read and backtest path is OANDA only (`service/bars.go` rejects any source
  but `oanda`), and canonical-v2 plus its D1 rule are FX-shaped (see
  [Compatibility](#compatibility-with-current-trader)).
- Practical source for the first verified SPY D1 test: **Stooq SPY D1**. It has
  5,418 bars from 2005-02-25 to 2026-09-10, consolidated volume, gaps consistent with
  market closures (not checked against an authoritative calendar), and it is already split into the layout's `unzipped` stage.
  Its prices are **dividend-adjusted** (total-return style), which must be recorded
  and never spliced with Alpaca. Alpaca SPY D1 is IEX-only (volume about 1.6% of
  consolidated) and has real data only from 2020-07-27 (plus one 2018-11-01 bar).

## What was inspected

| Path | What it is |
|---|---|
| `/srv/data/` | Data root: `alpaca`, `dukascopy`, `news_days`, `oanda`, `stooq` (31 GB total, FX dominates) |
| `/srv/data/stooq/raw/d_us_txt.zip` | Stooq daily US bundle, 539,192,683 bytes, mtime 2026-09-10, sha256 `962aec12...a52c` |
| `/srv/data/stooq/raw/h_us_txt.zip` | Stooq hourly US bundle, 456,122,237 bytes, mtime 2026-09-10, sha256 `c5bcdf71...2a1` |
| `/srv/data/stooq/raw/{spy,qqq,aapl}_us_d.csv` | Loose native Stooq daily downloads (Sep 5-6) |
| `/srv/data/stooq/raw/movers_clean.csv` | 21-row screener list (not OHLCV; not market data for backtests) |
| `/srv/data/stooq/unzipped/SPY/YYYY/MM/SPY-YYYY-MM-d1.csv` | 260 raw-v1 monthly files, SPY D1 only |
| `/srv/data/alpaca/raw/<SYM>/YYYY/MM/<SYM>-YYYY-MM-d1.csv` | 262 raw-v1 monthly files per symbol, 15 symbols |
| `/srv/data/oanda/{raw,canonical}`, `/srv/data/dukascopy/raw` | FX, out of scope; used only to see what canonical-v2 looks like |
| `/srv/trading/bin/download_alpaca_history.sh` | Records how the Alpaca set was produced (symbol, exchange, kind) |
| `_archive/internal/marketdata/...`, `docs/arch/adr-047..053,072,073` | Old equity code and decisions, read only |

Other places searched for Stooq/Alpaca/`.us.txt`/`spy_us*` files (whole filesystem,
same device): nothing else holds equity data. `/srv/trading/data` does not exist
(older scripts such as `bin/stooqs2canonical.sh` point at it). `~/trader_test` is a
14 MB file, not a data tree; `~/trader-data-store` is empty. I did not look inside
`~/.config/alpaca` (credentials) and recorded nothing from it.

Sources of truth for classification: Stooq's bundle folders (`nyse etfs`,
`nasdaq stocks`, ...) and, for Alpaca, the symbol/kind list in
`download_alpaca_history.sh` (Alpaca's files carry no asset class).

## Reproducible read-only inventory

All commands read only. Run Python with `-I`; write outputs to a scratch dir, never
under `/srv/data`.

```sh
# layout and sizes
find /srv/data -maxdepth 3 -not -path '*/oanda/*' -not -path '*/dukascopy/*' | sort
du -sh /srv/data/stooq/* /srv/data/alpaca/*
ls -la /srv/data/stooq/raw
sha256sum /srv/data/stooq/raw/*

# alpaca: file and symbol counts, headers
find /srv/data/alpaca -type f | wc -l                      # 3930
find /srv/data/alpaca/raw/SPY -type f | sort | sed -n '1p;$p'
head -2 /srv/data/alpaca/raw/SPY/2024/03/SPY-2024-03-d1.csv
find /srv/data/alpaca/raw -type f -exec head -1 {} + | grep -v '^==>' | sed 's/ year=.*feed/ ... feed/' | sort | uniq -c

# stooq bundles: member lists and groups (no extraction)
unzip -Z1 /srv/data/stooq/raw/d_us_txt.zip | awk -F/ '{print $3" "$4}' | sort | uniq -c
unzip -p /srv/data/stooq/raw/d_us_txt.zip 'data/daily/us/nyse etfs/2/spy.us.txt' | { head -3; tail -1; }
unzip -p /srv/data/stooq/raw/h_us_txt.zip 'data/hourly/us/nyse etfs/2/spy.us.txt' | { head -3; tail -1; }
```

Per-symbol rows, first/last date, duplicates and OHLC checks were computed with a
throwaway read-only Python script (`zipfile` streaming for the bundles, `csv`-free
line splitting for the partitions); its logic is: for each `*.txt`/`*.csv`, count data
rows, take first and last date, count repeated timestamps, count rows where
`high < max(open,close,low)` or `low > min(open,close,high)`, and list weekdays with
no bar between first and last date. It is not committed (the commands above plus the
definitions here reproduce it; the Go tests for the real reader will be the
permanent version).

## Coverage table

Row counts are data rows (headers and `#` comment lines excluded). "Dup" is repeated
timestamps; "OHLC bad" is rows violating the high/low containment. Size is bytes on
disk (raw text) or uncompressed member size (zip).

### Alpaca raw (provider `alpaca`, feed `iex`, interval D1, canonical: none)

Raw partitions are one file per month, Jan 2005 to Oct 2026 (262 files per symbol,
about 100 KB per symbol in total). Most early files are header-only: no symbol has
data before 2020-07-27 except one SPY bar. Downloaded 2026-10-06 (file mtime).

| Symbol | Class | Rows | First | Last | Non-empty months | Dup | Size |
|---|---|---|---|---|---|---|---|
| SPY | ETF | 1,558 | 2018-11-01 (then 2020-07-27) | 2026-10-06 | 77 | 0 | 101,940 |
| QQQ | ETF | 1,557 | 2020-07-27 | 2026-10-06 | 76 | 0 | 100,883 |
| IWM | ETF | 1,557 | 2020-07-27 | 2026-10-06 | 76 | 0 | 101,335 |
| DIA | ETF | 1,557 | 2020-07-27 | 2026-10-06 | 76 | 0 | 99,297 |
| GLD | ETF | 1,557 | 2020-07-27 | 2026-10-06 | 76 | 0 | 100,622 |
| TLT | ETF | 1,557 | 2020-07-27 | 2026-10-06 | 76 | 0 | 98,975 |
| XLE | ETF | 1,557 | 2020-07-27 | 2026-10-06 | 76 | 0 | 94,842 |
| XLF | ETF | 1,557 | 2020-07-27 | 2026-10-06 | 76 | 0 | 97,518 |
| XLK | ETF | 1,557 | 2020-07-27 | 2026-10-06 | 76 | 0 | 96,597 |
| AAPL | equity | 1,557 | 2020-07-27 | 2026-10-06 | 76 | 0 | 102,206 |
| MSFT | equity | 1,557 | 2020-07-27 | 2026-10-06 | 76 | 0 | 101,484 |
| NVDA | equity | 1,557 | 2020-07-27 | 2026-10-06 | 76 | 0 | 98,511 |
| TSLA | equity | 1,557 | 2020-07-27 | 2026-10-06 | 76 | 0 | 101,449 |
| JPM | equity | 1,554 | 2020-07-27 | 2026-10-06 | 76 | 0 | 100,839 |
| XOM | equity | 1,554 | 2020-07-27 | 2026-10-06 | 76 | 0 | 98,897 |

All 15 files sorted ascending, five-column rows after a 2-line header. Alpaca SPY
OHLC violations: 0 (others not checked). JPM and XOM have 3 fewer rows than the rest;
**unknown** why (not investigated).

### Stooq `unzipped` (provider `stooq`, raw-v1, canonical: none)

| Symbol | Class | Interval | Files | Rows | First | Last | Notes |
|---|---|---|---|---|---|---|---|
| SPY | ETF | D1 | 260 (2005-02 to 2026-09) | 5,418 | 2005-02-25 | 2026-09-10 | Identical to the `d_us_txt.zip` member on every row (0 differences) |

No other symbol and no H1 exists in `unzipped`.

### Stooq loose daily downloads (native `Date,Open,High,Low,Close,Volume`)

| File | Rows | First | Last | Comparison |
|---|---|---|---|---|
| `spy_us_d.csv` | 5,415 | 2005-02-25 | 2026-09-04 | 0 differing rows vs bundle; bundle has 3 later days (09-08 to 09-10) |
| `qqq_us_d.csv` | 6,915 | 1999-03-10 | 2026-09-04 | same: 0 differences, 3 later days in bundle |
| `aapl_us_d.csv` | 10,580 | 1984-09-07 | 2026-09-04 | same: 0 differences, 3 later days in bundle |

### Stooq bundles: whole-archive totals (raw; canonical none)

| Bundle | Group (asset class) | Members | Non-empty | Rows |
|---|---|---|---|---|
| daily | nasdaq etfs (ETF) | 946 | 944 | 1,388,778 |
| daily | nyse etfs (ETF) | 2,792 | 2,772 | 5,979,592 |
| daily | nasdaq stocks (equity) | 4,728 | 4,706 | 9,100,577 |
| daily | nyse stocks (equity) | 4,524 | 4,519 | 10,584,633 |
| daily | nysemkt stocks (equity) | 327 | 327 | 837,093 |
| hourly | nasdaq etfs | 946 | 944 | 2,151,075 |
| hourly | nyse etfs | 2,792 | 2,769 | 6,937,091 |
| hourly | nasdaq stocks | 4,728 | 4,705 | 11,192,945 |
| hourly | nyse stocks | 4,524 | 4,512 | 10,114,152 |
| hourly | nysemkt stocks | 327 | 327 | 844,623 |

The zip listing shows 13,331 entries per bundle (13,317 `.txt` members plus directory
entries); 49 daily and 60 hourly `.txt` members are empty (0 bytes). Daily rows total 27,890,673
over 13,268 non-empty members; first date 1970-01-02, last 2026-09-10 (nearly all
symbols end 2026-09). Hourly first timestamp 2024-08-23 for every symbol, last
2026-09-10. Whole-archive daily checks: 0 duplicate timestamps, 0 out-of-order rows,
68 OHLC-containment violations in 29 files (for example PCG 6, USB 6, BEN 5), 20,605
zero-volume rows.

### Stooq bundle members for the starter symbols (daily D and hourly H)

| Symbol | Class | D rows | D first | D last | H rows | H first | H last |
|---|---|---|---|---|---|---|---|
| SPY | ETF | 5,418 | 2005-02-25 | 2026-09-10 | 3,579 | 2024-08-23 | 2026-09-10 |
| QQQ | ETF | 6,918 | 1999-03-10 | 2026-09-10 | 3,579 | 2024-08-23 | 2026-09-10 |
| IWM | ETF | 5,418 | 2005-02-25 | 2026-09-10 | 3,579 | 2024-08-23 | 2026-09-10 |
| DIA | ETF | 5,418 | 2005-02-25 | 2026-09-10 | 3,579 | 2024-08-23 | 2026-09-10 |
| GLD | ETF | 5,418 | 2005-02-25 | 2026-09-10 | 3,579 | 2024-08-23 | 2026-09-10 |
| TLT | ETF | 5,418 | 2005-02-25 | 2026-09-10 | 3,579 | 2024-08-23 | 2026-09-10 |
| XLE | ETF | 5,418 | 2005-02-25 | 2026-09-10 | 3,579 | 2024-08-23 | 2026-09-10 |
| XLF | ETF | 5,418 | 2005-02-25 | 2026-09-10 | 3,579 | 2024-08-23 | 2026-09-10 |
| XLK | ETF | 5,418 | 2005-02-25 | 2026-09-10 | 3,579 | 2024-08-23 | 2026-09-10 |
| AAPL | equity | 10,583 | 1984-09-07 | 2026-09-10 | 3,579 | 2024-08-23 | 2026-09-10 |
| MSFT | equity | 10,200 | 1986-03-13 | 2026-09-10 | 3,579 | 2024-08-23 | 2026-09-10 |
| NVDA | equity | 6,950 | 1999-01-22 | 2026-09-10 | 3,579 | 2024-08-23 | 2026-09-10 |
| TSLA | equity | 4,075 | 2010-06-28 | 2026-09-10 | 3,579 | 2024-08-23 | 2026-09-10 |
| JPM | equity | 14,291 | 1970-01-02 | 2026-09-10 | 3,579 | 2024-08-23 | 2026-09-10 |
| XOM | equity | 14,291 | 1970-01-02 | 2026-09-10 | 3,579 | 2024-08-23 | 2026-09-10 |

The complete per-member list (13,268 daily and 13,257 hourly non-empty members) is
reproducible with the zip commands above; it is not committed because it is bulk
inventory, not a decision input. Note the D1 start of 2005-02-25 shared by the
ETFs: it looks like the bundle's own start for those series, not a gap
(**unknown** whether Stooq has earlier SPY history elsewhere).

### Not equity data, listed for completeness

`/srv/data/oanda/{raw,canonical}` (26 FX pairs), `/srv/data/dukascopy/raw` (10 FX
pairs), `/srv/data/news_days/*.txt` (calendar lists). Not inspected beyond one
OANDA canonical file header.

## Source formats and verified semantics

### Stooq daily member (bundle)

```
<TICKER>,<PER>,<DATE>,<TIME>,<OPEN>,<HIGH>,<LOW>,<CLOSE>,<VOL>,<OPENINT>
SPY.US,D,20050225,000000,92.7949,93.8716,92.7097,93.6948,79289004,0
```

Verified for SPY D1 (5,418 rows): `PER` is always `D`, `TIME` is always `000000`,
`OPENINT` is always 0, no weekend bars, no duplicates, sorted, volume never zero
and always an integer. Header is identical in all 13,268 non-empty daily members.
`.txt` members are CSV; paths contain spaces (`nyse etfs`) and a numeric shard
folder (`1`, `2`, `3`) for the larger groups. The loose `*_us_d.csv` files use
`Date,Open,High,Low,Close,Volume` and carry the same numbers.

The date has no time zone or session information. Bar-open versus bar-close
meaning of a date-only daily bar is by convention (the trading day); **no source
document was found**, so treat it as the New York trading date. Daily prices have
up to 4 decimals for SPY and up to 7 for AAPL (SPY: 4,078 of 21,672 price values
have 4 decimals, none more; AAPL has 1,216 with 7).

### Stooq hourly member

Same header, `PER` is `60`. SPY H1: 3,579 rows, 513 trading days
(2024-08-23 to 2026-09-10), the same days as the daily file (0 days missing, 0
extra). Observed `TIME` labels:

| Labels (HHMMSS) | Days | When |
|---|---|---|
| 160000..220000 (7 bars) | 467 | most days |
| 150000..210000 (7 bars) | 40 | 2024-10-28..11-01, 2025-03-10..03-28, 2025-10-27..10-31, 2026-03-09..03-27 (the weeks the US and Europe differ on DST) |
| 160000..200000 (5 bars) | 5 | for example 2025-11-28, 2025-12-24 |
| 180000..220000 (5 bars) | 1 | 2025-07-11 |

Inference, marked as such: the one-hour shift occurs only in the weeks where the US
and Europe are on different daylight-saving rules, so the clock is a **central
European local time, not UTC and not New York** (Europe/Warsaw or equivalent: the
zone name is **unknown**, nothing in the file states it). 15:00/16:00 to
21:00/22:00 European time corresponds to 10:00 to 16:00 New York. The first label
is therefore not 09:30; whether a label is the bar's start or end, and how the
09:30 to 10:00 half hour is bucketed, is **unknown**. Extended-hours presence is
**unknown** (no label falls outside the 10:00 to 16:00 New York window). Hourly
volume has fractional values in 997 bars up to 2025-03-20 (27.9% of rows) and
hourly volume sums to a median 65% of the daily volume (range 42% to 82%): hourly
volume is not the consolidated daily total. Per day, the hourly open and close are within 0.3% of the daily ones and the
hourly high and low within 0.8%. H1 covers only about two years, so
it does not serve a 2005-onward backtest; it is out of the first test.

### Alpaca raw partition

```
# schema=raw-v1 source=alpaca instrument=SPY tf=d1 year=2024 month=03 feed=iex
date,open,high,low,close,volume
2024-03-01,508.975,513.27,508.6,512.93,1239371
```

Header-only files exist for months with no data. The date is a plain date (no time
zone). `feed=iex` is in the header of all 262 SPY files and of the other files read; the
command above tallies the header of every file, but I did not store its full output.
The `adjustment` parameter used on download is **not recorded** in the files; the
price evidence below is the only basis.

The 2018-11-01 SPY bar is `271.6,271.6,271.6,271.6` with volume 200: a single print,
not a day's range. Treat it as an artifact; Alpaca SPY effectively starts 2020-07-27.

### Adjustment basis (verified by comparison, not by documentation)

- Stooq SPY close vs Alpaca SPY close on the same days (1,540 common days): ratio
  0.93 to 0.94 in 2020, 0.95 in 2021, 0.96 in 2022, 0.98 in 2023, 0.99 in 2024,
  1.00 from 2025. Stooq is lower in the past by about the dividends paid since, and
  converges to the Alpaca price at the present. SPY paid dividends and did not
  split in this window, so this is **dividend adjustment in Stooq**, matching
  ADR-073 (which records `AdjustmentTotalReturn`). On 2024-03-01 Stooq closes
  504.838 and Alpaca 512.93; the 2018-11-01 bar is 246.675 (Stooq) against 271.6.
- AAPL split on 2020-08-31 (4:1). Alpaca AAPL 2020-07-27 close is 94.86 and Stooq's
  91.8827, both on the post-split basis, and neither shows a jump across
  2020-08-28/31. So **both are split-adjusted**. Alpaca's `adjustment` setting is
  unknown, but its levels behave as split-adjusted without dividends.
- Non-dividend payers agree closely (TSLA 2020-07-27 Alpaca 102.84 vs Stooq 102.64;
  differences are presumably IEX-only versus consolidated prints, not investigated).
- Stooq daily history is rewritten by every ex-dividend date, so an old download is
  stale for price levels after the next ex-dividend (ADR-073). The bundle dates from
  2026-09-10.
- No split or dividend amounts are recorded in any of these files. Dividend and split
  **events are unknown** from the data; only their effect on levels can be seen.

### Volume

Stooq D1 volume is consolidated (SPY median daily volume tens of millions, e.g.
79,289,004 on 2005-02-25 and 42,642,514 on 2026-09-10). Alpaca IEX volume is
exchange-specific: median Alpaca/Stooq ratio 0.0158 (range 0.0000018 to 0.040). Do
not use Alpaca IEX volume as market volume. Early Stooq volumes for some symbols are
fractional (IWM 2005, MSFT 1986), reflecting a split-style scaling; SPY D1 is
integral.

### Calendar, gaps and holidays (SPY D1)

- Stooq SPY has 202 weekdays without a bar between 2005-02-25 and 2026-09-10 (6 in
  2005, 8 in 2026, otherwise 9 to 11 per year) and no weekend bars. Largest holes are
  `2006-12-29 -> 2007-01-03` and `2012-10-26 -> 2012-10-31`. Unscheduled closures
  I recognise appear as missing days: 2007-01-02, 2012-10-29 and 2012-10-30,
  2018-12-05, 2025-01-09. I did not check the full list against an authoritative
  NYSE calendar; it is consistent with one, not proven.
- Stooq and Alpaca SPY have the same trading days over 2020-07-27 to 2026-09-10
  (1,539 days each, 0 on one side only), which supports the missing days being
  non-trading days for both providers. SPY and IWM bundle dates are identical.
- Alpaca runs to 2026-10-06, 18 trading days past the Stooq bundle (2026-09-10).
- The daily file does not flag early-close days; the hourly file shows 5-bar days on
  2025-11-28 and 2025-12-24.
- Equities have a 09:30 to 16:00 New York regular session, holidays and early
  closes (DST moves the UTC time of the open and close by an hour). None of this is
  modelled by the live canonical code (below).

### Data-quality notes

- Stooq SPY D1: 0 duplicates, 0 OHLC violations, 0 zero volumes, sorted.
- Stooq whole daily archive: 68 OHLC violations in 29 files, 20,605 zero-volume rows,
  49 empty members. A converter must reject, not repair, such rows (ADR-090).
- Alpaca: no duplicates; mostly header-only months before 2020-07; IEX volume.
- SPY close-to-close returns from Stooq and Alpaca differ by 0.026% on average over
  1,538 steps; the largest is 1.17% on 2025-04-09 (cause **unknown**, likely
  exchange-feed or closing-price differences, not investigated). Over July to
  September 2026 (no intervening dividend), closes differ by -0.02% to +0.03%.
- No synthetic or carried-forward bars were created or assumed anywhere.

## Compatibility with current Trader

### What the current code does with a trade-price bar

- `marketdata.Bar` (`marketdata/bar.go`) has `Bid`, `Ask`, `Mid` and `Trade` OHLC
  series; `Trade` is described as "for sources that give trades". `Validate`
  accepts a bar whose only series is `Trade`, checks its own shape, and applies no
  bid/ask relationship to it. `Bar.OHLC()` prefers Mid, then Trade, then Bid, then
  Ask. `Volume` is an int64 described as "the provider's activity (tick) count, not a
  tradable quantity".
- **canonical-v2 refuses it.** `marketdata/internal/canonical/file.go:191` returns
  `ErrMalformedData` for any bar with a non-zero `Trade` series; the file format
  (`codec.go`) has only `bid_*`, `ask_*`, `mid_*` columns and `volume`. The package
  doc says so: "canonical-v2 stores bid, ask and mid only". So an equity trade bar
  has no persisted form today.
- Mapping a Stooq or Alpaca close into `Mid` would let it persist but would
  mislabel trade prices as a midpoint. Do not do this; the Bar doc says nothing
  infers a missing side and the issue forbids synthesising bid/ask or spreads.
  A trade series in canonical needs a schema decision (a new canonical version or
  trade columns), which is an architecture change for the owner.
- Prices are fixed at 5 decimals (mid 6) with `FormatFixed`, which returns
  `ErrPrecision` rather than round. Stooq SPY D1 (max 4 decimals) fits; AAPL (7
  decimals) and some hourly SPY values (up to 6) do not.
- Availability is `PolicyScheduledCloseV1`: D1 opens at **17:00 America/New_York**
  and is available at the next 17:00 (`schedule.go`; ADR-090). `Aligned` rejects any
  D1 time not at 17:00 New York. A daily equity bar has no such time, so a Stooq or
  Alpaca D1 needs an equity D1 alignment and availability rule (for example, the
  New York session close). Per AGENTS.md a bar is usable only from `AvailableAt`; do
  not invent one. This is a design question, not a conversion detail.
- The gap calendar (`calendar.go`) is FX only: weekly close Friday 17:00 to Sunday
  17:00 and year-end windows. ADR-090 states an equity provider "brings its own
  calendar rule". `USEquityCalendar` (ADR-049) exists only in `_archive/`.
- The old representation (`_archive/internal/marketdata/normalize_stooq.go`) stored
  trade OHLC as `Open/High/Low/Close` with no bid/ask and a zero spread, `Ticks` =
  volume, a midnight-UTC D1 label and an adjustment policy
  (`AdjustmentSplitAdjusted`, amended to `AdjustmentTotalReturn` for Stooq by
  ADR-073). It is a different `Bar` type and does not compile against the current
  one.

### Current read path

- `service.ReadBars` (`service/bars.go`) and the REST bars endpoint accept source
  `oanda` only (`only oanda`), `cmd/trader` build/backtest/data are OANDA only, and
  `provider/` contains only `oanda`. The `store` layout and `layout.Partition` are
  provider-neutral and already accept `stooq` and `alpaca`, plus the `unzipped`
  stage; the on-disk equity data already follows that layout.
- Build seam: `marketdata.SourcePartition` (provider-built, UNVALIDATED)
  and `service.Build` (domain rules, availability, mid) are the intended seam, but
  `Build` derives `Mid` from bid and ask and the alignment is 17:00 NY, so it cannot
  yet build equity bars.

### #401 assumptions versus current code

#401 (closed 2026-09-20) assumed a Stooq D1/H1 parser, canonical CSV with
`timestamp,open,high,low,close,volume`, provider-specific canonical paths and
verified adjustment metadata. Its later review comment pointed at
`internal/marketdata/internal/provider/stooq`, `stooq.Import`,
`normalizeStooqSequence`, `USEquityCalendar`, a Manifest with `AdjustmentSplitAdjusted`
and full-archive SPY tests. Against `main` today:

| #401 assumption | Current state |
|---|---|
| Stooq import and normalize code exists | Only in `_archive/` (`_archive/internal/marketdata/internal/provider/stooq/import.go`, `normalize_stooq.go`); not in the live tree |
| `USEquityCalendar`, equity D1 anchored at midnight UTC | Archived; live calendar is FX |
| Manifest / `Revision` / adjustment policy field | Replaced by the canonical-v2 `# meta` line (fingerprint, builder/validator version, availability policy, counts); there is no adjustment or price-basis field |
| Canonical columns `timestamp,open,high,low,close,volume` | Now `time,available_at,bid_*,ask_*,mid_*,volume`, no trade columns |
| Layout `provider/symbol/interval/year/month` with `stooq` in the path | Still true in spirit: `<source>/<stage>/<instrument>/<YYYY>/<MM>/...`, `stooq` and `alpaca` valid sources |
| Provider kept in metadata | Header has `provider=`; no asset class, currency, timezone or adjustment |
| Stooq timezone/session semantics documented | Not for H1 (see above); D1 is date-only, with no documented convention |
| Stooq adjustment unknown/verified | Verified split and dividend adjusted here and in ADR-073 |

The conclusion is that nothing from #401's code can be reused by importing it; the
design of ADR-047 to ADR-053 and ADR-072/073 (calendar, adjustment labelling, raw
fingerprint, incremental import) is reusable by moving and adapting one layer at a
time as AGENTS.md prescribes.

## Gaps and issues to raise

1. canonical-v2 has no trade series, no adjustment/price-basis label and a 5-decimal
   price width; an equity D1 file cannot be written. Needs a schema decision.
2. D1 alignment and availability are FX 17:00 New York; an equity D1 rule and a
   US equity calendar (holidays, early closes) are missing in the live tree.
3. No Stooq or Alpaca provider package, no read path for either source, no
   canonical output for any equity.
4. Adjustment must be an explicit, recorded property: Stooq D1 is dividend-adjusted
   and rewritten at every ex-dividend; Alpaca is split-adjusted (basis not recorded
   in its files) and IEX-only. Never splice them.
5. The Alpaca raw set is thin (about 6 years, IEX volume, 15 symbols) and its header
   does not record the adjustment setting.
6. Stooq H1 has an undocumented European clock and bar-label convention and only
   two years of history; do not ingest it until the convention is verified.

## Ranked small plan

Each item is one small issue and PR; none changes data under `/srv/data` except
writing new canonical files after an explicit approval of the target.

1. **Decide the equity canonical shape (design issue, owner approval).** Trade series
   persistence, price decimals, D1 alignment and availability rule for a daily US
   equity bar (and where the adjustment label lives). Smallest answer that fits SPY
   D1; no code before approval (AGENTS.md: needs-design). Output: an ADR or an
   amendment to ADR-090.
2. **SPY D1 read/ingest (Stooq).** A `provider/stooq` that parses the `unzipped` SPY
   monthly raw-v1 files (or the bundle member) into `marketdata.SourcePartition`,
   an equity D1 rule in the market-data service, and a canonical `stooq/SPY/d1` build
   verified against the raw fingerprint, with `service.ReadBars` accepting source
   `stooq`. Acceptance: 5,418 bars, 0 bad records, the missing days classified by an
   equity calendar, adjustment recorded as dividend-adjusted. SPY only; no batch
   discovery or download.
3. **Deterministic SPY D1 backtest.** Run one fixed strategy over the canonical
   Stooq SPY D1 range through the public API (`trader backtest` with the stooq
   source), with golden output. Trade-price bars carry no bid/ask, so no spread is
   invented; the result states the execution-price assumption the engine applies
   (to be checked in this step, not assumed here). Keep Stooq prices as one dataset, never merged with
   Alpaca.
4. **Alpaca SPY D1 cross-check (optional, small).** Add the Alpaca provider for the
   same month files to compare coverage and returns (not levels) on 2020-07 to now.
   Use as a check on Stooq, not as the primary series, because of IEX volume.
5. **Expand equities/ETFs.** QQQ, IWM, DIA and the other starter symbols from the
   bundle (D1 only), handling up-to-7-decimal prices (needs the decimals decision of
   step 1), the empty and OHLC-violating members and the stale-after-ex-dividend rule.
   Then the `unzipped` splitter for more symbols. H1 only after its clock is
   verified. Crypto later; futures out of scope.

## Open questions

- Stooq daily-bar convention for the equity D1 `Time` and `AvailableAt` (session close
  or other)?
- Is dividend-adjusted (total-return style) Stooq acceptable as the first verified SPY
  series for strategies that use price levels, or must the first test use returns
  only?
- Stooq H1 time zone name and whether labels are bar start or end: ask Stooq or
  derive from a known day with a known first print.
- What `adjustment` parameter did the Alpaca download use? Not recorded in the files.
- Why do JPM and XOM have 3 fewer Alpaca rows than the other symbols?
- Why does the Stooq ETF D1 history start at 2005-02-25?
- Should the `unzipped` stage and the raw bundles be refreshed (the bundle is dated
  2026-09-10; Alpaca runs to 2026-10-06)? No download was run here.

## Scope and limits of this inventory

Read-only. No files under `/srv/data` or any archive were modified; no downloads,
network calls, ingestion or Go code. Not inspected: contents of the 13,000 other
Stooq members beyond the totals above (per-member OHLC and dedup were scanned, but
adjustments, splits and quality were examined only for SPY, QQQ, IWM, AAPL, MSFT,
TSLA, NVDA and the Alpaca comparison); the Alpaca per-symbol OHLC sanity beyond SPY;
OANDA/Dukascopy beyond one header; the news-day files; `~/.config/alpaca`.
