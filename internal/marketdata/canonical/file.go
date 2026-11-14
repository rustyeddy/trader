package canonical

import (
	"errors"
	"fmt"
	"github.com/rustyeddy/trader/marketdata"
	"time"

	"github.com/rustyeddy/trader/internal/marketdata/layout"
)

// ErrMalformedData reports a canonical file, or a File value, that breaks the
// canonical-v2 rules: a bad header, a row that does not parse, bars out of order
// or outside the partition's month, counts that disagree with the metadata, an
// availability time the policy does not give.
var ErrMalformedData = errors.New("canonical: malformed data")

// Schema is the canonical file schema this package reads and writes.
const Schema = "canonical-v2"

// Key identifies one monthly canonical partition. Source is the provider
// (for example "oanda") and Symbol the instrument as spelled in the path.
type Key struct {
	Source   string
	Symbol   string
	Interval Interval
	Year     int
	Month    time.Month
}

func (k Key) layout() layout.Partition {
	return layout.Partition{Source: k.Source, Stage: layout.Canonical, Instrument: k.Symbol,
		Year: k.Year, Month: k.Month, Interval: string(k.Interval)}
}

// Validate reports ErrUnsupportedInterval for an interval other than H1 or D1,
// and the layout's error (layout.ErrInvalidComponent) for any part that cannot
// be a path component.
func (k Key) Validate() error {
	if !k.Interval.Valid() {
		return fmt.Errorf("%w: %q", ErrUnsupportedInterval, string(k.Interval))
	}
	if err := k.layout().Validate(); err != nil {
		return fmt.Errorf("canonical: %w", err)
	}
	return nil
}

// Path returns the partition file's path under root, through the layout
// resolver: root/<source>/canonical/SYMBOL/YYYY/MM/SYMBOL-YYYY-MM-<interval>.csv.
func (k Key) Path(root string) (string, error) {
	if err := k.Validate(); err != nil {
		return "", err
	}
	p, err := k.layout().Path(root)
	if err != nil {
		return "", fmt.Errorf("canonical: %w", err)
	}
	return p, nil
}

// Meta is what a canonical file records about how it was built. Together the
// first four fields decide whether a rebuild would change anything (SameBuild).
type Meta struct {
	// RawFingerprint identifies the raw bytes the file was built from.
	RawFingerprint string
	// BuilderVersion is bumped when the conversion's output changes.
	BuilderVersion string
	// ValidatorVersion is bumped when the accept and reject rules change, so a
	// file validated under old rules is rebuilt.
	ValidatorVersion string
	// AvailabilityPolicy names the rule that produced every AvailableAt, such
	// as PolicyScheduledCloseV1.
	AvailabilityPolicy string
	// BuiltAt is when the file was built. It is provenance and is not part of
	// SameBuild.
	BuiltAt time.Time
}

// SameBuild reports whether m and o describe the same build: the same raw
// bytes, builder, validator and availability policy. BuiltAt is ignored.
func (m Meta) SameBuild(o Meta) bool {
	return m.RawFingerprint == o.RawFingerprint && m.BuilderVersion == o.BuilderVersion &&
		m.ValidatorVersion == o.ValidatorVersion && m.AvailabilityPolicy == o.AvailabilityPolicy
}

// Reason says why a raw record did not become a bar.
type Reason string

// The reasons a record is kept as a bad record rather than a bar.
const (
	// ReasonIncomplete: the provider had not closed the candle.
	ReasonIncomplete Reason = "incomplete"
	// ReasonCrossed: an ask below its bid at some corner.
	ReasonCrossed Reason = "crossed"
	// ReasonInvalid: an impossible shape (high or low not containing open and
	// close, a non-positive price, a negative volume).
	ReasonInvalid Reason = "invalid"
	// ReasonDuplicate: the same time as an earlier record.
	ReasonDuplicate Reason = "duplicate"
	// ReasonOutOfOrder: earlier than the record before it.
	ReasonOutOfOrder Reason = "out-of-order"
	// ReasonMisaligned: not on the interval's boundary.
	ReasonMisaligned Reason = "misaligned"
)

// Valid reports whether r is one of the defined reasons.
func (r Reason) Valid() bool {
	switch r {
	case ReasonIncomplete, ReasonCrossed, ReasonInvalid, ReasonDuplicate, ReasonOutOfOrder, ReasonMisaligned:
		return true
	}
	return false
}

// BadRecord notes a raw record that was rejected, so the distinction between a
// bad record, a missing bar and a closed market is never lost.
type BadRecord struct {
	// Time is the record's opening time as the raw file gave it.
	Time   time.Time
	Reason Reason
	// RawRow is the record's position in the raw file: 1-based, counting data
	// rows only (the schema comment and column header are not counted).
	RawRow int
}

// File is one canonical partition: its key, how it was built, the good bars in
// ascending time and the rejected records in raw-row order.
type File struct {
	Key  Key
	Meta Meta
	Bars []marketdata.Bar
	Bad  []BadRecord
}

// Clean reports whether the partition has no bad records. A partition that is
// not clean may be missing data and must not be treated as complete (ADR-090).
func (f File) Clean() bool { return len(f.Bad) == 0 }

// Validate reports ErrMalformedData (or the key's error) unless the file obeys
// the canonical-v2 rules: a valid key; build metadata present; bars valid,
// aligned, strictly ascending and inside the partition's UTC month; availability
// times as the named policy gives when it is PolicyScheduledCloseV1; bad records
// with valid reasons, times inside the month and strictly ascending raw rows.
func (f File) Validate() error {
	if err := f.Key.Validate(); err != nil {
		return err
	}
	m := f.Meta
	if m.RawFingerprint == "" || m.BuilderVersion == "" || m.ValidatorVersion == "" || m.AvailabilityPolicy == "" {
		return fmt.Errorf("%w: metadata needs a raw fingerprint, builder, validator and availability policy", ErrMalformedData)
	}
	if m.BuiltAt.IsZero() {
		return fmt.Errorf("%w: metadata has no build time", ErrMalformedData)
	}
	for i, b := range f.Bars {
		if err := f.checkBar(i, b); err != nil {
			return err
		}
	}
	prevRow := 0
	for _, bad := range f.Bad {
		switch {
		case !bad.Reason.Valid():
			return fmt.Errorf("%w: bad record at %s has reason %q", ErrMalformedData, bad.Time.UTC().Format(time.RFC3339), string(bad.Reason))
		case bad.RawRow < 1 || bad.RawRow <= prevRow:
			return fmt.Errorf("%w: bad record raw rows must be positive and ascending, got %d after %d", ErrMalformedData, bad.RawRow, prevRow)
		case !f.inMonth(bad.Time):
			return fmt.Errorf("%w: bad record at %s is not in %04d-%02d", ErrMalformedData, bad.Time.UTC().Format(time.RFC3339), f.Key.Year, int(f.Key.Month))
		}
		prevRow = bad.RawRow
	}
	return nil
}

func (f File) inMonth(t time.Time) bool {
	u := t.UTC()
	return u.Year() == f.Key.Year && u.Month() == f.Key.Month
}

func (f File) checkBar(i int, b marketdata.Bar) error {
	at := b.Time.UTC().Format(time.RFC3339)
	if err := b.Validate(); err != nil {
		return fmt.Errorf("%w: bar %s: %v", ErrMalformedData, at, err)
	}
	if !b.Trade.IsZero() {
		return fmt.Errorf("%w: bar %s has a trade series, which canonical-v2 does not store", ErrMalformedData, at)
	}
	if !f.inMonth(b.Time) {
		return fmt.Errorf("%w: bar %s is not in %04d-%02d", ErrMalformedData, at, f.Key.Year, int(f.Key.Month))
	}
	if i > 0 && !b.Time.After(f.Bars[i-1].Time) {
		return fmt.Errorf("%w: bar %s is not after %s", ErrMalformedData, at, f.Bars[i-1].Time.UTC().Format(time.RFC3339))
	}
	ok, err := Aligned(f.Key.Interval, b.Time)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("%w: bar %s is not on a %s boundary", ErrMalformedData, at, string(f.Key.Interval))
	}
	if f.Meta.AvailabilityPolicy == PolicyScheduledCloseV1 {
		want, err := AvailableAt(f.Key.Interval, b.Time)
		if err != nil {
			return err
		}
		if !b.AvailableAt.Equal(want) {
			return fmt.Errorf("%w: bar %s is available at %s, but %s gives %s", ErrMalformedData, at,
				b.AvailableAt.UTC().Format(time.RFC3339), PolicyScheduledCloseV1, want.Format(time.RFC3339))
		}
	}
	return nil
}
