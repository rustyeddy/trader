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
// 13:00 New York time. IntervalState and ClassifyInterval give the
// vocabulary for why one expected bar interval does or does not have
// data — Present, Closed, Missing, Incomplete, or InProgress —
// replacing any dense, zero-filled representation: absence plus this
// state is how a closed market or a genuine gap is represented.
// DatasetComplete formally defines completeness over an
// already-classified range. The engine that walks a real query range
// and produces these states is issue #79's coverage catalog, not this
// package.
package marketdata
