// Package canonical is Trader's canonical bar store: the canonical-v2 file that
// holds one month of marketdata.Bar values, and the alignment and availability
// rule for the H1 and D1 intervals (ADR-090). The Bar itself, with its optional
// bid, ask, mid and trade series and the validation of what a possible bar is,
// is the public marketdata.Bar (ADR-091); canonical stores a Bar's bid, ask and
// mid and refuses a bar that carries a trade series, which this schema does not
// persist. It imports marketdata, num and the layout resolver and never a
// provider; a provider package maps its raw records into marketdata.Bar.
//
// # File format
//
//	# schema=canonical-v2 provider=oanda symbol=EURUSD interval=h1 year=2024 month=01
//	# meta {"raw_fingerprint":"sha256:...","builder_version":"...","validator_version":"...","availability_policy":"scheduled-close-v1","built_at":"...","bar_count":530,"bad_count":1,"first_bar":"...","last_bar":"..."}
//	# bad time=2024-01-05T14:00:00Z reason=crossed raw_row=1203
//	time,available_at,bid_o,bid_h,bid_l,bid_c,ask_o,ask_h,ask_l,ask_c,mid_o,mid_h,mid_l,mid_c,volume
//	2024-01-01T22:00:00Z,2024-01-01T23:00:00Z,1.10416,...,1.104270,...,188
//
// Bid and ask are exact decimals with five places and mid with six; an absent
// series is empty fields. Nothing goes through a float. Times are
// UTC. Good bars are rows in ascending time; every rejected raw record is a
// "# bad" line, so a bad record is never silently lost. A File that is not Clean
// may be missing data and must not be treated as complete.
//
// The path is authoritative (a header naming another partition is an error), and
// writes are atomic. Nothing here reads the environment or the clock; the build
// time is the caller's.
package canonical
