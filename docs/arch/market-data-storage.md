# Market data storage layout and source provenance

**Status:** Accepted storage-layout convention (2026-10-18). Implemented for new data by `internal/marketdata/layout` (the one path resolver) and `internal/store` (the only data-file I/O), ADR-089 and ADR-092; the existing archive on disk still uses the old order and is migrated separately (#642).

## Invariant

All persisted instrument price data, including production, research, and synthetic or copied test data, follows the **same layout beneath a configurable directory named `data`**:

```text
<parent>/data/<source>/<stage>/<instrument>/<YYYY>/<MM>/<filename>
```

- `source` is the originating provider (e.g., `oanda`, `alpaca`, `stooq`, `dukascopy`), including for canonical data.
- `stage` is `raw` or `canonical`.
- `instrument` is a stable, filesystem-safe identifier; do not assume every instrument is a six-character FX pair.
- `YYYY` and `MM` identify the partition; `MM` is zero-padded.
- Canonical files **must** be named `<instrument>-<YYYY>-<MM>-<interval>.csv`. Interval tokens must be consistent with Trader's supported interval identifiers.

Example (the data root is the `data` directory itself. `trader` takes it from `--data-root`, then `TRADER_DATA_ROOT`, then `marketdata.root`, then the `store` package's default, `/srv/data`):

```text
/srv/data/oanda/canonical/EURUSD/2024/03/EURUSD-2024-03-h1.csv
<repo>/testdata/data/oanda/canonical/EURUSD/2024/03/EURUSD-2024-03-h1.csv
```

The prefix can change; the subtree under the data root must not. Code builds paths relative to the root (`layout`) and the `store` package resolves them under its root, so a test points the root at a temporary directory. A package-local test fixture may similarly use `<package>/testdata/data/...`.

## Raw, canonical, and intermediates

**Raw** means the original provider-downloaded representation, unmodified. Preserve the original bytes, format, extension, and source identity. Provider filenames need not match canonical CSV naming. If provider files span multiple months, ingestion must define their partitioning explicitly; do not alter original payloads just to fit a filename rule.

**Canonical** means Trader's normalized **schema/representation**, not provider-independent OHLC prices. The same EURUSD H1 period from OANDA and Dukascopy may have different valid OHLC values. Never merge or silently substitute providers on the assumption that canonical prices are identical.

Intermediate forms produced during raw-to-canonical conversion are optional. Their retention, layout, and cleanup may be decided per ingestion pipeline; they are not part of the required `raw`/`canonical` contract.

## Metadata and assumptions

A brief `<source>/assumptions.json` or `<source>/canonical/assumptions.json` is permitted to document source-level or canonicalization defaults (for example, timestamp semantics, timezone, price basis, session/calendar, adjustment policy, or transformation version). These files are optional; do not require them for ordinary path resolution.

Instrument-, interval-, or dataset-specific exceptions belong in scoped metadata or existing CSV headers rather than being inaccurately asserted as provider-wide defaults. Define precedence explicitly if multiple metadata levels are implemented. Preserve existing CSV header/provenance conventions and avoid contradictory duplicate truths. Do not introduce a mandatory metadata schema without a separate design decision.

## Lookup and partitioning

- Resolution of canonical instrument prices must identify **data root, source, instrument, interval, and requested time range**; the provider is not optional.
- A single shared path resolver owns layout construction (`internal/marketdata/layout`), and a single package owns reading and writing the files (`store`). CLI, services, providers, and tests must not each invent their own paths or file operations.
- Use UTC as the partition reference for intraday data; place bars by their opening timestamp. Session-based daily/weekly/monthly partition semantics require explicit documented treatment so that provider trading calendars are not silently changed.
- Resolve and validate identifiers as path components; reject traversal and ambiguous identifiers.
- Report missing data and source mismatches explicitly; never silently fall back to another provider or the former directory layout.

## Migration

This replaces the old source/stage ordering:

```text
OLD: data/raw/oanda/...        NEW: data/oanda/raw/...
OLD: data/canonical/oanda/...  NEW: data/oanda/canonical/...
```

`trader data migrate` does the move for raw (ADR-092): each `raw/<source>` directory is
renamed to `<source>/raw`, as a dry run unless `--apply` is given, verified file by file
(`--checksum` for the bytes) and reversible with `--reverse`. Old `canonical/` files
are canonical-v1: they are not migrated but rebuilt with `trader data build`, then
deleted by the owner once `trader data audit` passes. Operator steps:

1. Stop every download and build.
2. `trader data migrate` (dry run) and read the plan; it refuses if a destination
   exists, a link or a temporary file is found.
3. `trader data migrate --apply --checksum`.
4. `trader data build` for the months wanted, then `trader data audit`.
5. Delete the old `canonical/` tree and any stale `.part` files by hand.

Original statement of the work:

Implementation must inventory existing path assumptions, migrate existing data safely, and update config, ingestion, readers, tests, and documentation. Prefer a reversible migration plan with verification before removal of old paths. No implicit legacy fallback.

This layout refines the storage organization described in historical-data ADR-020; its acquisition, provenance, and bar-normalization decisions remain applicable unless separately amended.

See [testing architecture](testing.md) for fixture ownership and regression expectations.
