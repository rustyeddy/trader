// Package canonical is Trader's canonical bar store: the files that hold one
// month of marketdata.Bar values, and the alignment and availability rules of
// those bars (ADR-090, ADR-095). The Bar itself, with its optional bid, ask, mid
// and trade series and the validation of what a possible bar is, is the public
// marketdata.Bar (ADR-091). There are two on-disk schemas and one bar:
// canonical-v2 stores bid, ask and mid for the H1 and D1 intervals of the FX
// session; canonical-trade-v1 stores the trade series of US equity daily bars.
// Neither holds both quotes and trades, and a file is refused rather than lose a
// series. The package imports marketdata, num, the layout resolver and the store
// and never a provider; a provider package maps its raw records into
// marketdata.Bar.
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
// A canonical-trade-v1 file has the same shape, with its own metadata (TradeMeta)
// and columns, and prices with eight decimals:
//
//	# schema=canonical-trade-v1 provider=stooq symbol=SPY interval=d1 year=2024 month=03
//	# meta {"raw_fingerprint":"sha256:...",...,"calendar":"us-equity-calendar-v1",...,"price_basis":"trade","currency":"USD",...}
//	time,available_at,trade_o,trade_h,trade_l,trade_c,volume
//	2024-03-01T14:30:00Z,2024-03-01T21:00:00Z,501.02800000,505.27000000,500.61400000,504.83800000,78064486
//
// Its bars open at 09:30 New York and are available at the scheduled close
// (PolicyUSEquityCloseV1), on the dates of the embedded CalendarUSEquityV1.
//
// The path is authoritative (a header naming another partition is an error), and
// writes are atomic. Nothing here reads the environment or the clock; the build
// time is the caller's.
package canonical
