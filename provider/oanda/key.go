package oanda

import (
	"fmt"
	"time"

	"github.com/rustyeddy/trader/internal/marketdata/layout"
)

// source is OANDA's name in the layout.
const source = "oanda"

// PartitionKey identifies one raw monthly partition: the provider symbol (for
// example "EURUSD"), the raw interval, and the calendar month the file covers.
// It is the domain value; a path is only its rendering under a data root, so
// paths stay strings at the operating-system boundary and the key carries the
// validation and the methods.
type PartitionKey struct {
	Symbol   string
	Interval RawInterval
	Year     int
	Month    time.Month
}

// Validate reports whether the key names a partition in the corpus: the symbol
// is one of the audited in-scope pairs (ErrInstrumentOutOfScope or
// ErrMalformedData otherwise), the interval is one of the raw tokens
// (ErrUnsupportedInterval), and the year and month satisfy the layout rules
// (layout.ErrInvalidComponent). A key that validates cannot reach a path
// outside the corpus.
func (k PartitionKey) Validate() error {
	if _, err := resolveSymbol(k.Symbol); err != nil {
		return err
	}
	if got, err := resolveInterval(string(k.Interval)); err != nil || got != k.Interval {
		return fmt.Errorf("%w: %q", ErrUnsupportedInterval, string(k.Interval))
	}
	if err := k.layout().Validate(); err != nil {
		return fmt.Errorf("oanda: %w", err)
	}
	return nil
}

// Path returns the partition file's absolute path under the data root, through
// the one layout resolver (ADR-089): root/oanda/raw/SYMBOL/YYYY/MM/SYMBOL-YYYY-MM-tf.csv.
// The path is also the partition's lock key, so it is the same for every
// spelling of one root.
func (k PartitionKey) Path(root string) (string, error) {
	if err := k.Validate(); err != nil {
		return "", err
	}
	path, err := k.layout().Path(root)
	if err != nil {
		return "", fmt.Errorf("oanda: %w", err)
	}
	return path, nil
}

// Key returns the key of the partition m describes.
func (m Meta) Key() PartitionKey {
	return PartitionKey{Symbol: m.Symbol, Interval: m.Interval, Year: m.Year, Month: m.Month}
}

func (k PartitionKey) layout() layout.Partition {
	return layout.Partition{Source: source, Stage: layout.Raw, Instrument: k.Symbol, Year: k.Year, Month: k.Month, Interval: string(k.Interval)}
}
