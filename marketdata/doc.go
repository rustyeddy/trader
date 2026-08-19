// Package marketdata defines Trader's bar-interval and trading-calendar
// vocabulary, as decided by issue #27 (M1-09) and ADR-012, and owns
// Manager, the sole application-service boundary through which consumers
// obtain historical market data (issue #71, M2-03; ADR-020). The
// acquisition, normalization, storage, resampling, and coverage machinery
// that Manager coordinates lands incrementally through M2 behind internal
// boundaries; see the architecture document for that larger scope.
//
// # Half-open ranges
//
// TimeRange represents every time span in this package as [start, end):
// start is included, end is excluded. A bar that ends at 17:00 and the
// bar that starts at 17:00 therefore never both claim that instant.
//
// # Intervals are typed, not parsed
//
// Interval is built from a typed Unit and a count — never from a
// provider-native string such as "H4" — so core code cannot misinterpret
// a malformed interval spelling. M1, H1, H4, D1, and W1 are provided as
// predefined values; Interval's String method exists only for display and
// is never parsed back.
//
// # Calendars align bars to sessions, not just the clock
//
// No bar interval, sub-day included, is assumed to align to the raw UTC
// clock: a trading day is not a UTC calendar day, and Calendar exists to
// make that alignment explicit and testable rather than assumed.
// FXCalendar is the one implementation this package provides, covering
// spot FX's continuous weekly session — open Sunday 17:00 New York time,
// closed Friday 17:00 through Sunday 17:00, with a daily rollover at
// 17:00 New York time on every other day. Every FXCalendar bar interval,
// including minute and hour bars, anchors to that same daily rollover
// (ADR-021 corrected an earlier, wrong UTC-midnight assumption for
// sub-day bars — see that ADR for why OANDA's real H4 candles disagreed
// with it). Exchange-traded asset classes will need their own Calendar
// implementations (holidays, early closes, futures session boundaries)
// in later milestones; Calendar is an interface for exactly that reason.
//
// FXCalendar blank-imports time/tzdata so its New York DST transitions
// resolve identically regardless of the host's installed time zone
// database — a deliberate binary-size tradeoff in favor of deterministic
// backtests.
//
// # No time.Now
//
// The interval and calendar methods are pure functions of an explicit
// time.Time argument. Manager receives a clock.Clock at construction and
// uses it wherever the current time matters — judging whether the current
// interval is still open, and answering as-of coverage without look-ahead.
// Nothing in this package calls time.Now, time.NewTimer, time.After, or
// time.Sleep directly; the composition root supplies the clock.
//
// # Manager: the historical-data gateway
//
// Manager is the only way to reach historical market data. It separates
// read-only queries, which never download or rebuild and report missing
// data explicitly, from explicit acquisition and build commands. Provider-
// and storage-native types never cross its boundary, and the internal
// provider, storage, normalization, and resampling components it
// coordinates depend on this package's types rather than the reverse. See
// Manager's own documentation for the full boundary and lifecycle contract.
// The M2-03 skeleton establishes construction, ownership, and dependency
// direction; the read and mutation operations report ErrNotImplemented
// until the M2-01 / ADR-020 query and canonical-persistence contracts land.
//
// # Bars and bar sets
//
// Bar is one canonical observed FX bar (issue #72, ADR-020): bid-basis
// OHLC, an average and maximum spread, a tick count, and an authoritative
// observed open Time stored verbatim rather than reconstructed from array
// position. BarSet is the homogeneous collection level, holding the
// instrument, interval, span, and price basis shared by all its bars so
// that metadata is not repeated on every Bar. Both are plain records
// validated with Validate at the normalization/store boundary; dataset
// revision and coverage/gap detail live at other levels (issues #73 and
// #79), not on BarSet. A missing interval is an absent Bar described by
// coverage — never a zero-filled dummy row.
//
// # Manifests identify a dataset without a path
//
// Manifest (issue #73, ADR-020) is the collection-level identity,
// provenance, and revision record for a canonical BarSet: provider,
// instrument, interval, span, price basis, schema/builder/validator/
// resampler/calendar versions, a raw-source fingerprint, a lightweight
// coverage summary, and optional parent lineage for a derived dataset.
// Dataset identity is never a filename or directory path.
// Manifest.Revision computes a deterministic fingerprint from every other
// field rather than storing a caller-supplied one, and Manifest.Matches
// checks that a Manifest and a BarSet describe the same data. Full
// Coverage/Gap detail remains a separate concern (issue #79).
//
// # Closures, gaps, and interval state
//
// FXCalendar's holiday support now includes partial closures
// (PartialClosure) alongside full-session ones, and StandardFXHolidays
// supplies Trader's own accepted M2 FX holiday rule set (issue #74,
// ADR-020) — New Year's Day, Christmas Day, and Boxing Day as full
// closures, Christmas Eve and New Year's Eve as partial closures from
// 13:00 New York time. FXCalendar.Status and FXCalendar.Session are
// computed together, so a truncated session (a partial closure, or the
// week not reopening because the Sunday it would open on is itself a
// holiday) can never make them disagree about which instants are open.
//
// IntervalState and ClassifyInterval give the vocabulary for why one
// expected bar interval does or does not have data — Present, Closed,
// Missing, Incomplete, InProgress, or Unexpected — replacing any dense,
// zero-filled representation: absence plus this state is how a closed
// market or a genuine gap is represented. A present Bar during a
// calendar closure is Unexpected, not Closed — the two are not
// equivalent, since Unexpected means the calendar and the data actively
// disagree, and DatasetComplete never accepts it as complete.
// ClassifyInterval also rejects, via ErrIntervalStraddlesBoundary, any
// span that isn't uniformly one Calendar Status throughout, rather than
// classifying it from its start alone. DatasetComplete formally defines
// completeness over an already-classified range. The engine that walks
// a real query range
// and produces these states is issue #79's coverage catalog, not this
// package.
//
// # Normalizing raw OANDA records
//
// normalizeOANDARecord and normalizeOANDASequence (issue #76, ADR-020)
// are this package's raw-to-canonical pipeline for OANDA: exact
// normalization (never through float64), then Bar.Validate, then
// classification into one of four recordOutcome values — accepted,
// incomplete (OANDA's own complete flag was false), suspicious (a
// crossed bid/ask, caught before a candidate Bar can even be built and
// distinguished from any other spread-computation failure, which is
// rejected instead), or rejected (an impossible Bar shape, a duplicate
// or out-of-order timestamp, or a timestamp that does not align to the
// partition's interval boundary). A Calendar failure while checking
// alignment is not itself a misaligned-timestamp verdict — it means
// alignment could not be evaluated — so normalizeOANDASequence aborts
// with that error rather than folding it into a per-record outcome.
// AvgSpread and MaxSpread follow the formula ADR-020 settled on: the
// mean and max of the four corner (ask - bid) spreads. Every type and
// function here is unexported — reachable only from within this
// package, the same boundary #75's archive inventory already
// established — since no oanda-native type is meant to escape it and
// there is no public consumer yet for a normalization result.
//
// # The canonical CSV store
//
// canonicalCSVStore (issue #77, ADR-020) persists a published
// (Manifest, BarSet) pair as one file — a schema comment, a JSON-encoded
// manifest header line, then canonical CSV rows — under
// root/provider/SYMBOL/YYYY/MM, ADR-020's derived-tree convention. It
// implements the barStore interface Manager's store field names, so a
// later implementation (a Parquet store, say) can be substituted without
// changing Manager, and a reusable, implementation-agnostic contract
// test exercises any barStore against that same interface. publish
// writes the file to a temporary name and renames it into place in one
// step: manifest and data were originally two separate files, but a
// design review found that let a cancelled or failed publish leave the
// prior revision merely detectably-inconsistent rather than genuinely
// intact, so they were merged into one atomically-renamed artifact — an
// option the issue's own scope explicitly allowed. Revision never
// appears in the path, per ADR-020; the file's own manifest header is
// where dataset identity and version information live, and load
// recomputes and cross-checks that header's revision against the
// decoded Manifest before trusting it. Cancellation is checked all the
// way to the actual commit point — immediately before the rename, and
// once per bar row while encoding, not only before the write begins —
// so cancelling a large publish part-way through never silently
// replaces the prior revision. Every type and function here is
// unexported, for the same reason as the normalizer above.
//
// # Historical Bar queries and the memory cache
//
// Manager.Bars (issue #78, ADR-020) is Manager's first operation: a
// strictly read-only historical query, taking a BarQuery (instrument,
// interval, half-open range) and returning a *BarReader over stable,
// chronologically ordered Bars. Bars resolves the query's instrument to
// this Manager's provider-native Listing through the configured
// instrument.Resolver (ADR-016), then loads every partition that could
// hold data for the query — through an unexported, Manager-owned
// barCache (cache.go) — before returning a reader; BarReader.Next
// therefore never itself does I/O. Loading is not by itself proof the
// query is satisfiable: coverageGap checks that the union of every
// loaded Manifest.Span, clipped to the query's range, actually covers
// it with no hole, and boundaryProbeKeys additionally loads the one
// calendar month on each side of the query's core months, tolerating a
// missing file there, so a session-aligned D1/W1 bar filed under an
// adjacent month (the canonical store's own overlap-not-containment
// rule permits this) is still found. Any gap reports a wrapped
// ErrDataUnavailable naming the uncovered sub-range and no reader at
// all, never a partial result; an invalid query reports a wrapped
// ErrInvalidQuery. Because core and probe partitions can genuinely
// overlap, the same bar Time can be encountered twice; barsEqual accepts
// a repeat only when it is byte-for-byte the same observation, and
// reports any disagreement as a wrapped ErrInconsistentData rather than
// silently keeping whichever partition loaded first. BarReader.Manifests
// discloses the provenance of every
// partition a result was assembled from, and — like barCache.get/put —
// returns manifests cloned via cloneManifest, so mutating one (through
// its Parent pointer) can never poison the cache or a later query.
// barCache evicts FIFO once its capacity (Config.CacheCapacity,
// defaulted if unset) is exceeded — no adaptive or LRU policy, matching
// the issue's own scope — exposes an invalidate hook for a future
// canonical-build/publish operation to evict a superseded revision, and
// is safe for concurrent use (every method locks its own mutex).
//
// New now builds its own canonicalCSVStore from Config.StoreRoot
// whenever no store is already wired in (the normal case for every
// caller outside this package), closing the gap left by the M2-03
// skeleton: previously nothing could construct a working Manager outside
// tests. Neither the store nor barCache is ever exposed through Config,
// an accessor, or a return value — Manager.Bars remains the only door in
// or out of published canonical Bar data, the same boundary the rest of
// this package doc already describes.
//
// # Coverage, gaps, and data-build planning
//
// Manager.Coverage and Manager.Plan (issue #79, ADR-020) are Manager's
// second read-only operation pair: given the same BarQuery shape Bars
// takes, Coverage reports what canonical data exists and why any of it
// is absent, and Plan derives the deterministic sequence of raw
// downloads and canonical builds that would close those gaps — without
// downloading, building, or publishing anything itself.
//
// PartitionCoverageStatus (Missing, Invalid, Stale, Current) classifies
// one UTC-calendar-month canonical partition file's own standing, and is
// deliberately orthogonal to IntervalState (#74), which classifies
// individual bar intervals within a partition that already exists and
// can be trusted. Staleness is judged two ways, both self-contained: a
// raw-built interval's canonical Manifest.RawFingerprint against the
// current raw partition's fingerprint (marketdata/internal/provider/
// oanda.Inspect), and the derived W1 interval's Manifest.Parent.Revision
// against the underlying D1 partition's Revision recomputed live — no
// speculative "current builder/validator/calendar code version" config
// was introduced, since no build operation exists yet to define one.
// Coverage.Gaps merges consecutive non-Present/non-Closed boundaries
// (Missing, Incomplete, InProgress, Unexpected) into one Gap each; a
// closed market is never a Gap. Canonical Bar carries no provider-
// completeness flag, so IntervalStateIncomplete is not currently
// producible from canonical data — the closest available signal is raw-
// side, PartitionCoverage.RawIncompleteCount from oanda.Partition.
// IncompleteCount.
//
// W1 cannot reuse ClassifyInterval directly: FXCalendar.Bar's W1 span is
// a full seven-day week that always contains the Friday-to-Sunday
// closure within it, so it is never uniformly one Calendar Status
// throughout, and ClassifyInterval's straddle check would reject every
// genuine W1 span. Coverage classifies W1 weeks separately (Present,
// Missing, or InProgress only — a week is never itself calendar-closed
// the way a single day or hour can be).
//
// Plan applies gated scheduling: an action for one stage is only
// produced once that stage's own input is already satisfiable — no
// ActionNormalizeCanonical for a month whose raw partition is missing or
// failed integrity checks, and no ActionDeriveCanonical for a W1 month
// whose underlying D1 range is not itself complete. This reproduces the
// legacy Inventory -> WantList -> Plan -> Download/Build planner's
// dependency-aware scheduling without transplanting legacy code.
// ActionDownloadRaw with reason "extend" additionally covers the most
// recently touched month when the calendar reports open intervals past
// the raw partition's last known record, using Manager's own clock — the
// same determinism contract Bars and Coverage already honor.
//
// Config gains two more optional fields for this: RawRoot (the raw
// archive location; required only when Coverage/Plan are actually
// called, unlike StoreRoot) and Calendar (defaults to
// NewFXCalendar(FXCalendarParams{}) when unset, the same "build a real
// internal default when not overridden" pattern the canonical store
// already uses).
//
// # Explicit OANDA synchronization
//
// Manager.Sync (issue #80, ADR-020) executes exactly the
// ActionDownloadRaw entries in a Plan a caller already obtained from
// Manager.Plan — never a caller-supplied instrument/interval/range, and
// never a plan Sync recomputes itself — acquiring missing or extending
// raw OANDA partitions through an internal oanda.Client while leaving
// canonical data completely untouched. ActionNormalizeCanonical and
// ActionDeriveCanonical entries are reported in SyncResult.Skipped, not
// executed: building canonical data from raw remains a separate, future
// Manager operation, keeping Sync's scope exactly "fill missing raw
// partitions and extend recent history."
//
// Sync decides missing-versus-extend operationally, by checking whether
// a raw partition file already exists, rather than trusting an Action's
// Reason text (diagnostic, not a dispatch key): no file means a brand-
// new, atomically-written partition (oanda.WritePartition's
// mustNotExist, which errors rather than silently overwriting anything
// that unexpectedly appeared — see WritePartition's own doc comment for
// why this is Link-based, not a Stat-then-Rename with a real race
// window); an existing, readable file means only the range since its
// last record is fetched and merged in by timestamp — through now or
// the calendar month's end, whichever is earlier, never a bulk
// redownload of already-preserved history, and re-fetched from that
// record's own Time rather than skipped past when it is itself still
// provider-incomplete, so a refreshed, finalized candle replaces a
// stale provisional one instead of being lost. A malformed existing raw
// file is not something Sync ever attempts to extend or repair: Plan
// reports that case as the distinct ActionRepairRaw (not
// ActionDownloadRaw), which Sync always reports in SkippedAction —
// repairing a corrupted raw artifact remains a separate, explicitly-
// authorized operation this issue does not build.
//
// Config gains OANDACredential (an oanda.CredentialProvider, never a
// bare token field — see its own doc comment) and OANDABaseURL, both
// optional but required together: New rejects one supplied without the
// other. Bars, Coverage, and Plan never need either; only Sync does, and
// it reports a clear configuration error if neither was supplied. The
// bearer token itself is never held anywhere in this package as a plain
// value, and never appears in a DownloadResult, SkippedAction, or error
// Sync returns — oanda.Client places it on exactly one outgoing request
// header and nowhere else.
//
// # Canonical build: normalization and resampling
//
// Manager.Build (issue #81, ADR-020) executes exactly the
// ActionNormalizeCanonical and ActionDeriveCanonical entries in a Plan a
// caller already obtained from Manager.Plan, mirroring Sync's own "only
// these actions" scope split: Build never executes ActionDownloadRaw or
// ActionRepairRaw (reported in BuildResult.Skipped, matching Sync's own
// SkippedAction pattern), and Sync never executes a canonical build
// action. ActionNormalizeCanonical converts one raw partition into
// canonical Bars at the same interval and publishes it; a Suspicious or
// Rejected record anywhere in the partition aborts the whole call before
// anything is published, while an Incomplete record (OANDA's own
// complete flag false) is silently excluded without aborting.
// ActionDeriveCanonical resamples canonical D1 into canonical W1,
// re-checking D1 completeness per calendar week against Manager's own
// Coverage rather than trusting whatever range originally produced the
// Action, and leaving any not-yet-ready week absent from the published
// result rather than aborting the whole month. W1's Manifest.Parent
// always names the D1 partition filed under the identical (year, month)
// key, and RawFingerprint propagates that parent's value verbatim, since
// W1 has no raw source of its own.
//
// Both paths publish through the same canonical store and cache
// invalidation Bars and Coverage already rely on (#77, #78) — a
// republished partition is immediately visible to the next Bars call,
// never served stale from cache.
//
// A build-tag-gated (corpus) operator test file exercises Build's
// derive path at corpus scale against a real, preserved OANDA archive,
// comparing derived H4/D1 against OANDA's own native H4/D1 partitions
// and, once, against the legacy candle-v2 canonical tree; see that
// file's own doc comment and ADR-020 for what a real run against it
// found.
package marketdata
