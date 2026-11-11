package marketdata

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"time"

	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/marketdata"
)

// DataSpan summarizes the data held for one (instrument, interval) in
// one place, the raw archive or the canonical store.
type DataSpan struct {
	// First and Last are the open times of the earliest and latest
	// records found. Both are zero when no partition could be read.
	First time.Time
	Last  time.Time
	// End is the exclusive end of the bar opening at Last, from the
	// Manager's calendar, so [First, End) is a half-open range covering
	// every record found. When the calendar cannot place Last (a raw
	// record off the calendar's grid), End is Last plus the interval's
	// nominal length. Zero when Last is zero.
	End time.Time
	// Partitions counts the monthly partitions found.
	Partitions int
}

// Inventory reports, without a caller-supplied range, what raw and
// canonical data exist for one (instrument, interval) (issue #435).
type Inventory struct {
	Instrument instrument.ID
	Interval   marketdata.Interval
	// Raw is nil when the raw archive holds no readable partition for
	// the instrument and interval, and always nil for a derived interval
	// (W1), which has no raw data of its own. Malformed or unreadable
	// raw partitions are not counted; Coverage and Plan report them.
	Raw *DataSpan
	// Canonical is nil when no canonical partition has been published.
	// Partitions counts every published partition file, including one
	// whose content is malformed or invalid; First and Last come from the
	// partitions that load. Gaps within the span are available from Coverage over
	// [First, Last].
	Canonical *DataSpan
}

// Inventory is a pure read: it never downloads, builds, or writes. It
// fails only for an invalid request, an unresolvable instrument, a
// provider that cannot hold the interval, or an I/O error reading
// either tree. A canonical partition that fails with an I/O error (for
// example permission denied) fails Inventory rather than being skipped,
// so an unreadable newest partition never reports an earlier end.
func (m *Manager) Inventory(ctx context.Context, id instrument.ID, interval marketdata.Interval) (Inventory, error) {
	if !m.configured() {
		return Inventory{}, fmt.Errorf("marketdata: inventory: %w: manager is not configured", ErrInvalidConfig)
	}
	if err := ctx.Err(); err != nil {
		return Inventory{}, err
	}
	if id.IsZero() {
		return Inventory{}, fmt.Errorf("marketdata: inventory: %w: zero instrument", ErrInvalidQuery)
	}
	if !interval.Valid() {
		return Inventory{}, fmt.Errorf("marketdata: inventory: %w: invalid interval", ErrInvalidQuery)
	}
	listing, err := m.resolver.ResolveInstrument(id, m.providerName, "")
	if err != nil {
		return Inventory{}, fmt.Errorf("marketdata: inventory: resolve listing: %w", err)
	}
	symbol := listing.Symbol()

	inv := Inventory{Instrument: id, Interval: interval}
	if inv.Raw, err = m.rawSpan(ctx, symbol, interval); err != nil {
		return Inventory{}, fmt.Errorf("marketdata: inventory: %w", err)
	}
	if inv.Canonical, err = m.canonicalSpan(ctx, symbol, id, interval); err != nil {
		return Inventory{}, fmt.Errorf("marketdata: inventory: %w", err)
	}
	return inv, nil
}

// rawSpan summarizes the readable raw partitions for symbol and interval.
func (m *Manager) rawSpan(ctx context.Context, symbol string, interval marketdata.Interval) (*DataSpan, error) {
	rawInterval, ok := intervalToRawInterval(interval)
	if !ok {
		return nil, nil
	}
	lookup, err := m.rawInventoryLookup(ctx, symbol, interval)
	if err != nil {
		return nil, err
	}
	var span DataSpan
	for key, p := range lookup {
		if key.symbol != symbol || key.interval != rawInterval || p.status != rawPartitionOK || p.rowCount == 0 {
			continue
		}
		span.Partitions++
		if span.First.IsZero() || p.firstTime.Before(span.First) {
			span.First = p.firstTime
		}
		if p.lastTime.After(span.Last) {
			span.Last = p.lastTime
		}
	}
	if span.Partitions == 0 {
		return nil, nil
	}
	span.End = m.barEnd(span.Last, interval)
	return &span, nil
}

// canonicalSpan summarizes the published canonical partitions for
// symbol and interval. Only the earliest and latest loadable partitions
// are read.
func (m *Manager) canonicalSpan(ctx context.Context, symbol string, id instrument.ID, interval marketdata.Interval) (*DataSpan, error) {
	months, err := m.store.months(ctx, m.providerName, symbol, interval)
	if err != nil {
		return nil, err
	}
	if len(months) == 0 {
		return nil, nil
	}
	span := DataSpan{Partitions: len(months)}
	load := func(ym yearMonth) (marketdata.BarSet, bool, error) {
		key := partitionKey{provider: m.providerName, symbol: symbol, instrument: id, interval: interval, year: ym.year, month: ym.month}
		_, bs, err := m.loadPartition(ctx, key)
		switch {
		case err == nil:
			return bs, len(bs.Bars) > 0, nil
		case ctx.Err() != nil:
			return marketdata.BarSet{}, false, ctx.Err()
		case skippableLoadError(err):
			return marketdata.BarSet{}, false, nil
		default:
			return marketdata.BarSet{}, false, err
		}
	}
	for _, ym := range months {
		bs, ok, err := load(ym)
		if err != nil {
			return nil, err
		}
		if ok {
			span.First = bs.Bars[0].Time
			break
		}
	}
	for i := len(months) - 1; i >= 0; i-- {
		bs, ok, err := load(months[i])
		if err != nil {
			return nil, err
		}
		if ok {
			span.Last = bs.Bars[len(bs.Bars)-1].Time
			break
		}
	}
	span.End = m.barEnd(span.Last, interval)
	return &span, nil
}

// barEnd returns the exclusive end of the bar opening at last; see
// DataSpan.End.
func (m *Manager) barEnd(last time.Time, interval marketdata.Interval) time.Time {
	if last.IsZero() {
		return time.Time{}
	}
	if bar, err := m.calendar.Bar(last, interval); err == nil && bar.End().After(last) {
		return bar.End()
	}
	return last.Add(nominalLength(interval))
}

// nominalLength is interval's length ignoring calendar adjustments.
func nominalLength(interval marketdata.Interval) time.Duration {
	unit := time.Minute
	switch interval.Unit() {
	case marketdata.UnitHour:
		unit = time.Hour
	case marketdata.UnitDay:
		unit = 24 * time.Hour
	case marketdata.UnitWeek:
		unit = 7 * 24 * time.Hour
	}
	return time.Duration(interval.Count()) * unit
}

// skippableLoadError reports whether a canonical partition that failed to
// load can be skipped when finding the canonical span: a file removed
// since it was listed, or one whose content is malformed or invalid
// (Coverage reports those as Invalid). Any other filesystem error —
// permission denied, a failed read — is an operational failure, not a
// shorter dataset, and must not be skipped.
func skippableLoadError(err error) bool {
	if errors.Is(err, fs.ErrNotExist) {
		return true
	}
	var pathErr *fs.PathError
	return !errors.As(err, &pathErr)
}
