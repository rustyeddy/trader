package marketdata

import (
	"context"
	"fmt"
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
	// that fails to load; First and Last come from the partitions that
	// load. Gaps within the span are available from Coverage over
	// [First, Last].
	Canonical *DataSpan
}

// Inventory is a pure read: it never downloads, builds, or writes. It
// fails only for an invalid request, an unresolvable instrument, a
// provider that cannot hold the interval, or an I/O error reading
// either tree.
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
	lookup, err := m.rawInventoryLookup(ctx, interval)
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
		if err != nil {
			if ctx.Err() != nil {
				return marketdata.BarSet{}, false, ctx.Err()
			}
			// Invalid, or removed since listing; Coverage reports it.
			return marketdata.BarSet{}, false, nil
		}
		return bs, len(bs.Bars) > 0, nil
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
	return &span, nil
}
