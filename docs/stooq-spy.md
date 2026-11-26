# Stooq SPY daily bars

Trader builds one equity series: SPY daily trade-price bars from Stooq's daily history
(ADR-095, #735). It can be built and read. It cannot be backtested yet (#736).

## Input

Stooq's own daily format, either the bulk bundle with the symbol's file named inside it,
or that file extracted, unchanged:

```
<TICKER>,<PER>,<DATE>,<TIME>,<OPEN>,<HIGH>,<LOW>,<CLOSE>,<VOL>,<OPENINT>
SPY.US,D,20050225,000000,92.7949,93.8716,92.7097,93.6948,79289004,0
```

The monthly raw-v1 files under `stooq/unzipped/` and the single-symbol
`Date,Open,High,Low,Close,Volume` download are **not** inputs: a series is one whole
history from one set of bytes, so its adjusted prices are consistent. The input is only
read. Nothing is downloaded.

## Build

Writing to the shared data root (`/srv/data`) is the owner's decision. Try it in a
scratch root first; the input path is relative to the root, so link the bundle in:

```sh
root=$(mktemp -d)
mkdir -p "$root/stooq/raw"
ln -s /srv/data/stooq/raw/d_us_txt.zip "$root/stooq/raw/d_us_txt.zip"

trader data build --data-root "$root" --source stooq --instrument SPY \
  --input stooq/raw/d_us_txt.zip \
  --member 'data/daily/us/nyse etfs/2/spy.us.txt' \
  --snapshot-as-of 2026-09-10
```

For the shared archive, run the same command with `--data-root /srv/data` and no link.

`--snapshot-as-of` is the date you obtained the file. It is recorded the first time those
bytes are built and reused afterwards, whatever is passed later.

## Output

```
<root>/stooq/snapshots/SPY/<sha256>.raw          the input bytes, unchanged
<root>/stooq/snapshots/SPY/<sha256>.json         date, file name, member, ZIP hash
<root>/stooq/canonical/SPY/YYYY/MM/SPY-YYYY-MM-d1.csv   canonical-trade-v1
```

For the bundle inventoried on 2026-11-24 (ZIP sha256 `962aec12...a52c`):

| | |
|---|---|
| Snapshot | `sha256:54b64bba890d89495bc2108824ba378a3f7f92349d5021595499bbc47696ee6b` |
| Bars | 5,418 in 260 months, 2005-02-25 to 2026-09-10 |
| Bad records | 0 |
| Unexplained gaps | 1: 2011-02-17 |

Running the command again leaves every month `unchanged`.

- **Bad rows** (a weekend or listed closure, an impossible or unrepresentable price, a
  fractional volume, a duplicate, a row out of order) are kept in their month's file as
  bad records, the command fails after publishing, and strict reads refuse those months.
- **A file that cannot be read** (wrong format, ticker or period, a malformed date or
  number) and **a date outside the calendar** (2005 to 2026) fail the build with nothing
  written.
- **A new download** replaces the whole series. Old snapshots stay archived. A snapshot
  that lacks a month already on disk is refused. A rebuild that stops part way leaves two
  snapshots, which reads refuse until it is run again.
- **Rebuilding from the archive** is the same command with
  `--input stooq/snapshots/SPY/<sha256>.raw` and no `--member`.

## Read

```sh
trader serve --data-root "$root"
curl -s 'http://127.0.0.1:8080/v1/marketdata/bars?source=stooq&instrument=SPY&interval=d1&from=2024-03-01&to=2024-04-01'
```

See [rest-api.md](rest-api.md#trade-price-data) for the `data` object. A strict read
(`partial=false`) of a range that covers 2011-02-17 fails: SPY has no row for that
session, and it is not on a known-holes list until it is confirmed (ADR-095).

`trader data status`, `verify` and `audit` cover OANDA data only.

## Check against your own copy

An opt-in test builds from a local bundle into a temporary root and compares every bar
read back with the bundle's rows:

```sh
TRADER_STOOQ_BUNDLE=/srv/data/stooq/raw/d_us_txt.zip \
  go test -tags stooqsmoke ./cmd/trader -run TestSmokeStooq -v -count=1
```
