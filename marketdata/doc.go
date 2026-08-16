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
// Minute and hour bars align to the UTC clock. Day and week bars do not:
// a trading day is not a UTC calendar day, and Calendar exists to make
// that alignment explicit and testable rather than assumed. FXCalendar is
// the one implementation this package provides, covering spot FX's
// continuous weekly session — open Sunday 17:00 New York time, closed
// Friday 17:00 through Sunday 17:00, with a daily rollover at 17:00 New
// York time on every other day. Exchange-traded asset classes will need
// their own Calendar implementations (holidays, early closes, futures
// session boundaries) in later milestones; Calendar is an interface for
// exactly that reason.
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
// ErrInvalidQuery. BarReader.Manifests discloses the provenance of every
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
package marketdata
