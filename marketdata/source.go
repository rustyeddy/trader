package marketdata

import (
	"errors"
	"fmt"
	"time"
)

// ErrInvalidSource reports a SourcePartition that does not describe a partition:
// a missing name or fingerprint, a month outside 1 to 12, or row numbers that are
// not positive and ascending.
var ErrInvalidSource = errors.New("marketdata: invalid source partition")

// SourceRow is one parseable row of a provider's raw partition, as the provider
// gave it. Bar is UNVALIDATED: its prices are exactly the provider's, Mid is not
// derived and AvailableAt is unset. Whether the row becomes a canonical bar or a
// bad record is the market-data service's decision, never the provider's, so a
// provider passes every row it can parse through, good or not.
type SourceRow struct {
	// Row is the row's position in the raw file: 1-based, counting data rows
	// only (the schema comment and column header are not counted).
	Row int
	// Bar holds Time, Volume, Bid and Ask as the provider gave them.
	Bar Bar
	// Complete is the provider's own flag that the candle had closed.
	Complete bool
}

// SourcePartition is one provider raw partition (one month, one instrument, one
// interval) handed to the market-data service for conversion to canonical bars.
// It carries what a bare []Bar would lose: the raw row numbers, the provider's
// complete flag and the fingerprint of the raw bytes the rows were read from.
//
// A provider builds it by parsing and mapping only. It sets no AvailableAt, does
// no validation and applies no domain rule; a row it cannot parse at all is an
// error from the provider, not a row here.
type SourcePartition struct {
	// Source names the provider, as in the path (for example "oanda").
	Source string
	// Instrument is the provider's symbol as spelled in the path (for example
	// "EURUSD").
	Instrument string
	// Interval is the lower-case interval token, such as "h1" or "d1".
	Interval string
	Year     int
	Month    time.Month
	// Fingerprint identifies the exact raw bytes the rows were read from
	// ("sha256:<hex>").
	Fingerprint string
	// Rows are the partition's rows in raw-file order.
	Rows []SourceRow
}

// Validate reports ErrInvalidSource unless p names a partition and its rows
// carry positive, strictly ascending row numbers. It looks at nothing about the
// prices or times: that is the market-data service's job.
func (p SourcePartition) Validate() error {
	switch {
	case p.Source == "":
		return fmt.Errorf("%w: no source", ErrInvalidSource)
	case p.Instrument == "":
		return fmt.Errorf("%w: no instrument", ErrInvalidSource)
	case p.Interval == "":
		return fmt.Errorf("%w: no interval", ErrInvalidSource)
	case p.Month < time.January || p.Month > time.December:
		return fmt.Errorf("%w: month %d is not 1 to 12", ErrInvalidSource, int(p.Month))
	case p.Fingerprint == "":
		return fmt.Errorf("%w: no fingerprint", ErrInvalidSource)
	}
	prev := 0
	for _, r := range p.Rows {
		if r.Row < 1 || r.Row <= prev {
			return fmt.Errorf("%w: row numbers must be positive and ascending, got %d after %d", ErrInvalidSource, r.Row, prev)
		}
		prev = r.Row
	}
	return nil
}
