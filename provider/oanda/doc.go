// Package oanda is the raw OANDA store: the provider-native record, the
// audited instrument scope, a reader, an atomic writer and an inventory of the
// raw-v1 CSV corpus, and the HTTP client that downloads candles (ADR-020,
// ADR-089). It reads no environment and never uses slog's global logger.
//
// # Layout
//
// Every path comes from internal/marketdata/layout, so a raw partition lives at
//
//	<root>/oanda/raw/PAIR/YYYY/MM/PAIR-YYYY-MM-<tf>.csv
//
// with <tf> one of m1, h1, h4, d1. root is the caller's data root; nothing here
// hard-codes one. There is no fallback to the retired <stage>/<source> order:
// the existing archive is migrated separately, by renaming, before this package
// can read it.
//
// # File format
//
// Each file is self-describing and byte-compatible with the archive:
//
//	# schema=raw-v1 source=oanda instrument=EURUSD tf=h1 year=2020 month=05
//	time,bid_o,bid_h,bid_l,bid_c,ask_o,ask_h,ask_l,ask_c,volume,complete
//	2020-05-01T00:00:00Z,1.09439,1.09548,...,2235,true
//
// Prices are exact num.Price values written with five decimals, as the archive
// has them; a price with more precision is an error, never rounded. The daily
// file names its interval d1 in the path but writes tf=d in the comment; both
// resolve to RawD1. The path is authoritative: a schema comment that disagrees
// with it is ErrMalformedData, as is any malformed row. Timestamps are the
// provider's, verbatim, never rebuilt from row position.
//
// # Scope
//
// Scope is the audited set of 26 FX pairs, not a currency-combination rule. A
// symbol outside it (XAUUSD, or a valid-looking absent pair such as USDEUR)
// reports ErrInstrumentOutOfScope. W1 has no raw partition and reports
// ErrUnsupportedInterval.
//
// # Writing
//
// WritePartition is atomic: a temporary file beside the destination, synced,
// then committed. Create-only links it into place, so an existing authoritative
// file is never silently replaced (ErrPartitionAlreadyExists). UpdatePartition
// is the safe extend: it holds a process-local per-partition lock across the
// read, the caller's merge and the replace, so overlapping updates cannot lose
// records. Separate processes are not coordinated. A failure or cancellation
// before the commit leaves any existing file untouched; one after it does not
// undo the commit.
//
// # Client
//
// Client downloads candles for one symbol, interval and half-open range,
// paginating (5000 per response) and retrying transient failures (429, 5xx,
// network errors) with a capped exponential backoff. It writes nothing: the
// caller stores the records through the raw store. The base URL and a
// CredentialProvider are injected; the base must be https (plain http only to a
// loopback host). The credential is asked for before every request and its token
// is placed only on the Authorization header: it is never logged, never in an
// error, and an error whose text would contain it is scrubbed. Logging goes
// through an injected *slog.Logger (nil is silent), and waits go through an
// injected Sleeper, so tests never wait. Daily requests pin dailyAlignment 17
// and America/New_York (ADR-020). A page that is out of order or repeats a
// candle is ErrMalformedData, not accepted.
//
// # Dependencies
//
// The package imports num, instrument, layout, internal/logging and the
// standard library, and no other provider, the root engine or cmd.
package oanda
