// Package oanda reads Trader's preserved raw OANDA historical CSV corpus
// (issue #70, M2-02). It is an internal market-data provider: it lives under
// marketdata/internal/, which Go restricts to importers within the
// marketdata/ subtree, so nothing outside that subsystem can reach it — all
// access is through marketdata.Manager, per ADR-020.
//
// # What it does, and does not, do
//
// This package parses the existing raw archive; it does not download data,
// choose Trader's canonical storage format, mutate any dataset, or resample.
// A raw file's rows become provider-native Record values that a later
// normalization stage converts into canonical marketdata.Bar values. Record
// is deliberately close to the file: bid/ask OHLC and OANDA's own tick/
// activity count and complete flag, preserved rather than interpreted.
//
// To keep the dependency direction clean — and to let marketdata.Manager own
// and wire this provider without an import cycle — the package does not import
// the root marketdata package. It carries a provider-native RawInterval token
// rather than marketdata.Interval; mapping a RawInterval to the canonical
// Interval is a normalization concern, not this reader's.
//
// # Archive shape
//
// The corpus is laid out as
//
//	oanda/PAIR/YYYY/MM/PAIR-YYYY-MM-<tf>.csv
//
// with <tf> one of m1, h1, h4, d1. Each file is self-describing:
//
//	# schema=raw-v1 source=oanda instrument=EURUSD tf=h1 year=2020 month=05
//	time,bid_o,bid_h,bid_l,bid_c,ask_o,ask_h,ask_l,ask_c,volume,complete
//	2020-05-01T00:00:00Z,1.09439,1.09548,...,2235,true
//
// The file path is authoritative for a partition's instrument, interval,
// year, and month; the schema comment, when present, is cross-checked
// against it and a disagreement is an error. The daily files name the
// interval d1 in the path but tf=d in the comment; both map to D1.
//
// # Scope
//
// Scope is an exact, audited set of the 24 FX pairs preserved in the archive,
// not a currency-combination rule: two in-scope currencies do not by
// themselves make an in-scope pair (there is no USDEUR or CADNZD partition).
// A symbol outside that set — XAUUSD (gold is not an FX leg) or a valid-looking
// but absent pair — reports ErrInstrumentOutOfScope rather than being treated
// as an FX pair. W1 is not present in the raw corpus and is not invented: a w1
// partition reports ErrUnsupportedInterval.
//
// # Exactness and provenance
//
// Prices parse to exact num.Price values; they are never routed through
// float64. Timestamps are read from each row and never reconstructed from row
// position. Malformed rows — wrong field count, unparseable price, time,
// volume, or complete flag — are reported as errors rather than silently
// skipped or coerced.
//
// # Live synchronization
//
// Client (issue #80, ADR-020) is this package's live acquisition path:
// a minimal OANDA v20 REST client for historical candle download,
// authenticated through a CredentialProvider rather than any bare token
// field or environment read (this package never calls os.Getenv; see
// CredentialProvider's own doc comment). It pins dailyAlignment=17 and
// alignmentTimezone=America/New_York explicitly on every request rather
// than relying on OANDA's undocumented account defaults (issue #74,
// ADR-020) — the archive's own D1 opens already match a 17:00
// America/New_York rollover, which is only true because the legacy
// client happened to inherit those same defaults, not because it
// requested them. A pinned request survives a provider default changing
// without Trader silently mis-aligning new data against the old
// archive. There is no OANDA-native weekly partition in the preserved
// archive (see RawInterval); W1 is a derived interval in M2, built by
// resampling canonical D1, not synchronized from OANDA directly, so no
// weeklyAlignment parameter is ever requested.
//
// FetchCandles paginates automatically (OANDA caps one response at
// 5000 candles), retries a transient failure (a network error, HTTP
// 429, or 5xx) with backoff up to a configured attempt limit, and
// classifies a permanent failure (401/403, 400/404) as a distinct,
// non-retried sentinel error rather than retrying blindly against
// something that cannot succeed. Every dependency — the HTTP transport,
// the clock backoff and rate-limit pacing run against, the page size —
// is exported or injected explicitly (HTTPDoer, ClientConfig), so no
// test in this package makes a real network call or a real sleep.
//
// WritePartition (writer.go) atomically writes a raw partition file —
// temp file, then rename — the same discipline
// marketdata.canonicalCSVStore.publish uses for canonical data (#77),
// applied here to raw data; the legacy client wrote raw files with a
// direct os.Create and no atomicity at all. Its mustNotExist parameter
// is the mechanism behind ADR-020's "prevent accidental replacement of
// an existing authoritative raw artifact" requirement: a genuinely new
// partition is written only when nothing already exists at that path,
// and an "extend" (new data appended to an existing partition) is the
// only case that replaces an existing file — always with a
// caller-merged superset of records, never a blind overwrite. Repairing
// a corrupted existing partition remains a distinct, explicitly-
// authorized future operation, not something an ordinary sync performs.
//
// marketdata.Manager.Sync (sync.go's counterpart in the parent package)
// is the only caller of Client/WritePartition; nothing outside the
// marketdata tree ever reaches either directly, the same boundary every
// other internal provider component in this package already keeps.
//
// # Archive inventory and integrity inspection
//
// Inspect (issue #75, ADR-020) walks a raw archive rooted at a caller-
// supplied directory and returns a deterministic Inventory: every
// discovered partition file's identity, PartitionStatus (OK, Unreadable,
// or Malformed), row/duplicate/incomplete-row counts, observed first and
// last record time, and a "sha256:<hex>" content fingerprint in the form
// marketdata.Manifest's RawFingerprint expects; every file it
// deliberately did not inventory, and why (SkippedEntry); and every hole
// in an otherwise-populated month sequence (MonthGap). Inspect never
// writes, renames, or deletes anything, and never searches for its root
// — a sibling tree sharing the same PAIR/YYYY/MM shape (a candles backup
// or save tree, for example) is only ever seen if the caller mistakenly
// points Inspect at it directly. Nor can such a tree, or a misplaced
// file, be mistaken for the archive by being nested inside root:
// verifyPathLayout requires every candidate file's path relative to root
// to be exactly SYMBOL/YYYY/MM/<filename>, agreeing with what the file
// name itself resolves to, or the file is recorded as skipped rather
// than inventoried.
//
// Inspect does not judge whether a bar missing from a present partition
// reflects a routine market closure or genuine data loss: that needs
// trading-calendar knowledge, and this package does not import the root
// marketdata package (see above) precisely so Manager can wire it
// without a cycle. MonthGap is calendar-blind by design; calendar-aware,
// row-level completeness belongs to issue #79's coverage engine,
// operating on canonical data where that knowledge is available.
//
// Full-archive inspection against a real archive is operator-run, not
// part of normal CI: it lives behind the fullarchive build tag
// (inventory_fullarchive_test.go), and its target is a local constant an
// operator edits by hand rather than an environment variable — domain
// code under marketdata/ must not read the environment or flags
// (config/arch_test.go enforces this mechanically), and this package's
// own internal/ visibility means the test cannot be relocated to the
// exempt test/ tree either.
package oanda
