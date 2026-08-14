// Package marketdata defines Trader's bar-interval and trading-calendar
// vocabulary, as decided by issue #27 (M1-09) and ADR-012. It does not yet
// acquire, normalize, store, or resample market data — see the
// architecture document for that larger scope, which lands in M2.
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
// Every method in this package is a pure function of an explicit
// time.Time argument. Nothing here calls time.Now; callers supply
// whatever time they want evaluated, including from a clock.Clock at a
// composition root.
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
package marketdata
