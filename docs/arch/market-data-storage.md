# Market data storage layout and source provenance

**Status:** Accepted storage-layout convention (2026-10-18). This document specifies the directory contract; existing files and callers still require migration.

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

Example:

```text
/srv/trading/data/oanda/canonical/EURUSD/2024/03/EURUSD-2024-03-H1.csv
<repo>/testdata/data/oanda/canonical/EURUSD/2024/03/EURUSD-2024-03-H1.csv
```

The prefix can change; the subtree under `data/` must not. A package-local test fixture may similarly use `<package>/testdata/data/...`.

## Raw, canonical, and intermediates

**Raw** means the original provider-downloaded representation, unmodified. Preserve the original bytes, format, extension, and source identity. Provider filenames need not match canonical CSV naming. If provider files span multiple months, ingestion must define their partitioning explicitly; do not alter original payloads just to fit a filename rule.

**Canonical** means Trader's normalized **schema/representation**, not provider-independent OHLC prices. The same EURUSD H1 period from OANDA and Dukascopy may have different valid OHLC values. Never merge or silently substitute providers on the assumption that canonical prices are identical.

Intermediate forms produced during raw-to-canonical conversion are optional. Their retention, layout, and cleanup may be decided per ingestion pipeline; they are not part of the required `raw`/`canonical` contract.

## Metadata and assumptions

A brief `<source>/assumptions.json` or `<source>/canonical/assumptions.json` is permitted to document source-level or canonicalization defaults (for example, timestamp semantics, timezone, price basis, session/calendar, adjustment policy, or transformation version). These files are optional; do not require them for ordinary path resolution.

Instrument-, interval-, or dataset-specific exceptions belong in scoped metadata or existing CSV headers rather than being inaccurately asserted as provider-wide defaults. Define precedence explicitly if multiple metadata levels are implemented. Preserve existing CSV header/provenance conventions and avoid contradictory duplicate truths. Do not introduce a mandatory metadata schema without a separate design decision.

## Lookup and partitioning

- Resolution of canonical instrument prices must identify **data root, source, instrument, interval, and requested time range**; the provider is not optional.
- A single shared path resolver should own layout construction and discovery. CLI, services, ingestion adapters, and tests must not each invent their own paths.
- Use UTC as the partition reference for intraday data; place bars by their opening timestamp. Session-based daily/weekly/monthly partition semantics require explicit documented treatment so that provider trading calendars are not silently changed.
- Resolve and validate identifiers as path components; reject traversal and ambiguous identifiers.
- Report missing data and source mismatches explicitly; never silently fall back to another provider or the former directory layout.

## Migration

This replaces the old source/stage ordering:

```text
OLD: data/raw/oanda/...        NEW: data/oanda/raw/...
OLD: data/canonical/oanda/...  NEW: data/oanda/canonical/...
```

Implementation must inventory existing path assumptions, migrate existing data safely, and update config, ingestion, readers, tests, and documentation. Prefer a reversible migration plan with verification before removal of old paths. No implicit legacy fallback.

This layout refines the storage organization described in historical-data ADR-020; its acquisition, provenance, and bar-normalization decisions remain applicable unless separately amended.

See [testing architecture](testing.md) for fixture ownership and regression expectations.
