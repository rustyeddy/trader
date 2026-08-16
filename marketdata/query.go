package marketdata

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/rustyeddy/trader/instrument"
)

// Sentinel errors returned (wrapped) by Manager.Bars and BarReader.
var (
	// ErrInvalidQuery marks a BarQuery that is missing a required field or
	// whose range is not well-formed.
	ErrInvalidQuery = errors.New("marketdata: invalid bar query")
	// ErrDataUnavailable marks a query that cannot be satisfied because
	// canonical data is missing for some part of the requested range. It
	// is never returned alongside a partial result: Bars either returns a
	// reader over accurate, complete data for the requested range, or it
	// returns this error and no reader at all.
	ErrDataUnavailable = errors.New("marketdata: requested data unavailable")
	// errBarReaderClosed marks a BarReader that Close has already been
	// called on.
	errBarReaderClosed = errors.New("marketdata: bar reader is closed")
)

// BarQuery describes the canonical Bar data a caller wants — an
// instrument, an interval, and a half-open time range — not how to
// acquire or store it. Query.go's Manager.Bars is the only way to
// exchange a BarQuery for data; nothing else in this package accepts one.
type BarQuery struct {
	// Instrument is the canonical instrument identity to query. Required.
	Instrument instrument.ID
	// Interval is the canonical bar interval to query. Required.
	Interval Interval
	// Range is the half-open [Start, End) time span to query, in the same
	// convention TimeRange documents throughout this package. Required.
	Range TimeRange
}

// validate reports whether q is well-formed enough to attempt, returning
// a wrapped ErrInvalidQuery for the first problem found.
func (q BarQuery) validate() error {
	if q.Instrument.IsZero() {
		return fmt.Errorf("%w: instrument is zero", ErrInvalidQuery)
	}
	if !q.Interval.Valid() {
		return fmt.Errorf("%w: interval is invalid", ErrInvalidQuery)
	}
	if q.Range.start.IsZero() || q.Range.end.IsZero() || !q.Range.end.After(q.Range.start) {
		return fmt.Errorf("%w: range is invalid", ErrInvalidQuery)
	}
	return nil
}

// BarReader iterates the Bars a successful Manager.Bars call resolved,
// in stable chronological order. A BarReader is fully materialized at
// construction — Manager.Bars only returns one once every touched
// partition has been loaded and validated — so Next never itself does
// I/O or blocks on anything but ctx; Manager.Bars is the operation that
// can be slow or fail on missing data, not iteration.
//
// A BarReader is not safe for concurrent use. It starts no goroutines and
// owns no resource beyond its own in-memory slices, so a caller that
// forgets to call Close leaks nothing beyond the reader itself; Close and
// the io.Closer-shaped contract exist for symmetry with other Trader
// readers and so a future implementation backed by something less inert
// than a slice does not need a different method set.
type BarReader struct {
	bars      []Bar
	manifests []Manifest
	pos       int
	closed    bool
}

// Next returns the next Bar in chronological order, or io.EOF once the
// reader is exhausted. It reports ctx's error if ctx is already done, and
// reports a wrapped error if the reader has been closed.
func (r *BarReader) Next(ctx context.Context) (Bar, error) {
	if r == nil || r.closed {
		return Bar{}, errBarReaderClosed
	}
	if err := ctx.Err(); err != nil {
		return Bar{}, err
	}
	if r.pos >= len(r.bars) {
		return Bar{}, io.EOF
	}
	b := r.bars[r.pos]
	r.pos++
	return b, nil
}

// Manifests returns the Manifest for every canonical partition this
// query's result was assembled from, in the order they were loaded. It
// discloses provenance and quality information a caller may need to
// judge whether the returned Bars are fit for a particular use — for
// example, comparing RawFingerprint or CalendarVersion across a run's
// data. The returned slice is a copy; mutating it does not affect r.
func (r *BarReader) Manifests() []Manifest {
	if r == nil {
		return nil
	}
	out := make([]Manifest, len(r.manifests))
	copy(out, r.manifests)
	return out
}

// Close marks r exhausted. It is idempotent and always returns nil: a
// BarReader owns no external resource that could fail to release.
func (r *BarReader) Close() error {
	if r == nil {
		return nil
	}
	r.closed = true
	return nil
}

// Bars resolves query against the canonical store, through Manager's own
// cache, and returns a BarReader over the result. Bars is strictly
// read-only: it never downloads raw data, never rebuilds or resamples a
// canonical dataset, and never changes which revision is currently
// published. If the canonical store does not have complete data for
// query's full range, Bars returns a wrapped ErrDataUnavailable and no
// reader — never a reader over a silently partial result.
//
// Bars resolves query.Instrument to this Manager's provider-native
// Listing (and its display symbol) through the configured Resolver
// before touching the store; an instrument with no tradable Listing
// under this Manager's ProviderName reports that resolution failure
// directly.
func (m *Manager) Bars(ctx context.Context, query BarQuery) (*BarReader, error) {
	if !m.configured() {
		return nil, fmt.Errorf("marketdata: bars: %w: manager is not configured", ErrInvalidConfig)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := query.validate(); err != nil {
		return nil, fmt.Errorf("marketdata: bars: %w", err)
	}

	listing, err := m.resolver.ResolveInstrument(query.Instrument, m.providerName, "")
	if err != nil {
		return nil, fmt.Errorf("marketdata: bars: resolve listing: %w", err)
	}

	keys := monthPartitionKeys(m.providerName, listing.Symbol(), query.Instrument, query.Interval, query.Range)

	var bars []Bar
	manifests := make([]Manifest, 0, len(keys))
	for _, key := range keys {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		man, bs, err := m.loadPartition(ctx, key)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, ctxErr
			}
			if errors.Is(err, os.ErrNotExist) {
				return nil, fmt.Errorf("marketdata: bars: %w: no canonical data for %s/%s %s %04d-%02d",
					ErrDataUnavailable, key.provider, key.symbol, key.interval, key.year, int(key.month))
			}
			return nil, fmt.Errorf("marketdata: bars: %w", err)
		}
		manifests = append(manifests, man)
		for _, b := range bs.Bars {
			if query.Range.Contains(b.Time) {
				bars = append(bars, b)
			}
		}
	}

	return &BarReader{bars: bars, manifests: manifests}, nil
}

// loadPartition returns the (Manifest, BarSet) for key, serving it from
// m.cache when present and populating the cache on a miss. It is the
// only path Bars uses to reach the store, so caching is transparent to
// every caller of Bars.
func (m *Manager) loadPartition(ctx context.Context, key partitionKey) (Manifest, BarSet, error) {
	if man, bs, ok := m.cache.get(key); ok {
		return man, bs, nil
	}
	man, bs, err := m.store.load(ctx, key)
	if err != nil {
		return Manifest{}, BarSet{}, err
	}
	m.cache.put(key, man, bs)
	return man, bs, nil
}

// monthPartitionKeys returns one partitionKey for every UTC calendar
// month that r overlaps, in chronological order. The canonical store
// partitions by UTC calendar month (ADR-020); a query range that spans
// more than one month, or that starts or ends mid-month, therefore
// touches more than one partition file, and every one of them must agree
// before Bars can return a complete result.
func monthPartitionKeys(provider, symbol string, id instrument.ID, interval Interval, r TimeRange) []partitionKey {
	start := r.Start().UTC()
	end := r.End().UTC()

	cur := time.Date(start.Year(), start.Month(), 1, 0, 0, 0, 0, time.UTC)
	var keys []partitionKey
	for cur.Before(end) {
		keys = append(keys, partitionKey{
			provider:   provider,
			symbol:     symbol,
			instrument: id,
			interval:   interval,
			year:       cur.Year(),
			month:      cur.Month(),
		})
		cur = cur.AddDate(0, 1, 0)
	}
	return keys
}
