package marketdata

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
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
// reports an error (the unexported errBarReaderClosed sentinel,
// unwrapped) if the reader has already been closed.
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
// data. Every returned Manifest is an independent clone (cloneManifest):
// the slice itself is a copy, and each element's Parent pointer, if any,
// is cloned too, so mutating a returned Manifest — including through
// Parent — can never affect r, another call to Manifests, or (since
// barCache clones on its own boundary) a later query's cached data.
func (r *BarReader) Manifests() []Manifest {
	if r == nil {
		return nil
	}
	out := make([]Manifest, len(r.manifests))
	for i, m := range r.manifests {
		out[i] = cloneManifest(m)
	}
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
// published. If the canonical store cannot prove complete coverage of
// query's full range — see "Proving coverage, not just loading files"
// below — Bars returns a wrapped ErrDataUnavailable and no reader — never
// a reader over a silently partial result.
//
// Bars resolves query.Instrument to this Manager's provider-native
// Listing (and its display symbol) through the configured Resolver
// before touching the store; an instrument with no tradable Listing
// under this Manager's ProviderName reports that resolution failure
// directly.
//
// # Proving coverage, not just loading files
//
// The canonical store partitions by UTC calendar month, but a
// partition's own Manifest.Span is not required to fall entirely within
// its filed month — only to overlap it (see checkKeyMatchesManifest in
// store_csv.go) — because a session-aligned D1 or W1 bar can legitimately
// open in the closing hours (D1) or days (W1) of the previous UTC
// month while still belonging, by the build step's own convention, to
// the month it was filed under. This means a bar whose Time falls inside
// query.Range can, in principle, live in a partition filed under a
// calendar month adjacent to every month query.Range itself touches, and
// loading only the months query.Range nominally overlaps
// (monthPartitionKeys) is not, by itself, enough to prove those bars
// were found.
//
// Bars therefore loads not just the core months query.Range touches, but
// also the one calendar month immediately before the first core month
// and the one immediately after the last (boundaryProbeKeys) — a bar
// filed under an adjacent month's partition, whether or not the "home"
// month's own file exists at all, must still be reachable. Every key,
// core or probe, is loaded the same tolerant way: a missing file
// (os.ErrNotExist) is not itself an error, since most queries and most
// month boundaries have nothing at an adjacent key. What actually
// decides success is coverageGap: the union of every Manifest.Span that
// was found, clipped to query.Range, must cover query.Range with no gap.
// A file that exists but fails to load (malformed, cancelled context)
// still fails the query immediately — silently ignoring a file that is
// known to exist but cannot be read would be worse than the coverage
// hole this design exists to close.
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

	provider, symbol := m.providerName, listing.Symbol()
	coreKeys := monthPartitionKeys(provider, symbol, query.Instrument, query.Interval, query.Range)
	keys := append(append([]partitionKey{}, coreKeys...), boundaryProbeKeys(coreKeys)...)

	var spans []TimeRange
	var bars []Bar
	seen := make(map[time.Time]bool)
	manifests := make([]Manifest, 0, len(keys))

	for _, key := range keys {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		man, bs, err := m.loadPartition(ctx, key)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue // nothing filed at this key; not an error by itself
			}
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, ctxErr
			}
			return nil, fmt.Errorf("marketdata: bars: %w", err)
		}
		manifests = append(manifests, man)
		spans = append(spans, man.Span)
		for _, b := range bs.Bars {
			if !query.Range.Contains(b.Time) || seen[b.Time] {
				continue
			}
			seen[b.Time] = true
			bars = append(bars, b)
		}
	}

	if gap, ok := coverageGap(query.Range, spans); !ok {
		return nil, fmt.Errorf("marketdata: bars: %w: no coverage for [%s, %s)",
			ErrDataUnavailable, gap.Start(), gap.End())
	}

	sort.Slice(bars, func(i, j int) bool { return bars[i].Time.Before(bars[j].Time) })
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

// boundaryProbeKeys returns the partitionKey immediately before core's
// first key and immediately after core's last key — the two adjacent
// calendar months a session-aligned D1/W1 bar could have spilled into or
// out of (see the "Proving coverage" section on Bars). It returns an
// empty slice for an empty core, which monthPartitionKeys never actually
// produces for a validated BarQuery, but boundaryProbeKeys stays total
// rather than assuming that.
func boundaryProbeKeys(core []partitionKey) []partitionKey {
	if len(core) == 0 {
		return nil
	}
	return []partitionKey{
		shiftPartitionKeyMonth(core[0], -1),
		shiftPartitionKeyMonth(core[len(core)-1], 1),
	}
}

// shiftPartitionKeyMonth returns a copy of key with its year/month
// shifted by delta calendar months (delta may be negative), correctly
// rolling over a year boundary.
func shiftPartitionKeyMonth(key partitionKey, delta int) partitionKey {
	t := time.Date(key.year, key.month, 1, 0, 0, 0, 0, time.UTC).AddDate(0, delta, 0)
	key.year = t.Year()
	key.month = t.Month()
	return key
}

// coverageGap reports whether the union of spans, each clipped to want,
// covers want's entire half-open range with no gap. It returns
// (TimeRange{}, true) when fully covered, or the first uncovered
// sub-range and false otherwise. spans need not be sorted, need not be
// disjoint, and may individually extend outside want — only their
// clipped overlap with want matters.
//
// This is deliberately not a general gap/coverage catalog (that is
// issue #79's scope): it answers exactly one question, whether the
// partitions Bars actually loaded prove complete coverage of one query's
// range, and is discarded once that question is answered.
func coverageGap(want TimeRange, spans []TimeRange) (TimeRange, bool) {
	type interval struct{ start, end time.Time }
	clipped := make([]interval, 0, len(spans))
	for _, s := range spans {
		start, end := s.Start(), s.End()
		if start.Before(want.Start()) {
			start = want.Start()
		}
		if end.After(want.End()) {
			end = want.End()
		}
		if end.After(start) {
			clipped = append(clipped, interval{start, end})
		}
	}
	sort.Slice(clipped, func(i, j int) bool { return clipped[i].start.Before(clipped[j].start) })

	cursor := want.Start()
	for _, iv := range clipped {
		if iv.start.After(cursor) {
			gap, err := NewTimeRange(cursor, iv.start)
			if err != nil {
				// Unreachable: iv.start.After(cursor) is exactly
				// NewTimeRange's success condition.
				panic(fmt.Sprintf("marketdata: coverageGap: %v", err))
			}
			return gap, false
		}
		if iv.end.After(cursor) {
			cursor = iv.end
		}
	}
	if cursor.Before(want.End()) {
		gap, err := NewTimeRange(cursor, want.End())
		if err != nil {
			panic(fmt.Sprintf("marketdata: coverageGap: %v", err))
		}
		return gap, false
	}
	return TimeRange{}, true
}
