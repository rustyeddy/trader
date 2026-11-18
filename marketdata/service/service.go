// Package service is the market-data domain service (ADR-091). Its capabilities
// are Build, Load and Verify. Build turns a provider's raw partition, handed over as
// a marketdata.SourcePartition, into a canonical-v2 file and publishes it under
// the store's data root (ADR-090); Load reads canonical files back as a series
// and checks it against the market calendar; Verify checks a canonical file against
// the raw partition it should have been built from.
//
// It owns every domain rule the provider must not: which rows are bad and why,
// the alignment and availability of a bar, the mid price, the build versions, and
// the decision to build, leave alone or replace a file. It imports the public
// marketdata package and the internal canonical implementation, never a provider
// and never another domain service; an application service in the module's
// service package coordinates it with a provider.
package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/marketdata/internal/canonical"
)

// Versions of the conversion. BuilderVersion changes when the output of a
// conversion changes; ValidatorVersion when the accept and reject rules change.
// Either one makes every file built under the old value stale, so the next build
// replaces it.
const (
	BuilderVersion   = "builder-v1"
	ValidatorVersion = "validator-v1"
)

// ErrUnsupportedInterval reports an interval other than h1 or d1.
var ErrUnsupportedInterval = errors.New("service: unsupported interval")

// Action says what a build did to a partition's canonical file.
type Action string

// The actions of Build.
const (
	// Built: there was no canonical file, and one was written.
	Built Action = "built"
	// Unchanged: a canonical file from the same build was already there.
	Unchanged Action = "unchanged"
	// Replaced: the file was from another build, or unreadable, and was replaced.
	Replaced Action = "replaced"
)

// Result is what one Build came to.
type Result struct {
	Source     string
	Instrument string
	Interval   string
	Year       int
	Month      time.Month
	// Path is the canonical file's path relative to the data root.
	Path   string
	Action Action
	// Good is the number of bars in the file and Bad the number of rejected
	// records kept in it. A partition with Bad > 0 is not complete data.
	Good, Bad int
}

// Build converts p to a canonical file and publishes it (ADR-090). Every row is
// classified: a complete, aligned, well-formed, new, in-order row becomes a bar
// (with its availability time and its mid price set here, not by the provider);
// any other row is kept in the file as a bad record with its reason and raw row
// number, so a bad partition is never mistaken for a quiet one. A row that is
// not even inside the partition's month means the raw file itself is damaged:
// the build fails and writes nothing.
//
// builtAt is recorded as provenance only; it never decides whether a file is
// rebuilt. The file is written atomically, and not at all when the build on disk
// is the same.
func Build(ctx context.Context, p marketdata.SourcePartition, builtAt time.Time) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if err := p.Validate(); err != nil {
		return Result{}, err
	}
	interval := canonical.Interval(p.Interval)
	if !interval.Valid() {
		return Result{}, fmt.Errorf("%w: %q (h1 and d1 only)", ErrUnsupportedInterval, p.Interval)
	}
	key := canonical.Key{Source: p.Source, Symbol: p.Instrument, Interval: interval, Year: p.Year, Month: p.Month}
	bars, bad, err := convert(ctx, key, p)
	if err != nil {
		return Result{}, err
	}
	f := canonical.File{
		Key: key,
		Meta: canonical.Meta{
			RawFingerprint: p.Fingerprint, BuilderVersion: BuilderVersion, ValidatorVersion: ValidatorVersion,
			AvailabilityPolicy: canonical.PolicyScheduledCloseV1, BuiltAt: builtAt.UTC(),
		},
		Bars: bars,
		Bad:  bad,
	}
	outcome, err := canonical.Publish(ctx, f)
	if err != nil {
		return Result{}, err
	}
	path, err := key.Path()
	if err != nil {
		return Result{}, err
	}
	return Result{
		Source: p.Source, Instrument: p.Instrument, Interval: p.Interval, Year: p.Year, Month: p.Month,
		Path: path, Action: Action(outcome), Good: len(bars), Bad: len(bad),
	}, nil
}

// convert classifies p's rows, in raw order, into bars and bad records.
func convert(ctx context.Context, key canonical.Key, p marketdata.SourcePartition) ([]marketdata.Bar, []canonical.BadRecord, error) {
	var (
		bars    []marketdata.Bar
		bad     []canonical.BadRecord
		seen    = make(map[int64]bool, len(p.Rows))
		prev    time.Time
		hasPrev bool
	)
	for _, r := range p.Rows {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		t := r.Bar.Time.UTC()
		if t.Year() != p.Year || t.Month() != p.Month {
			return nil, nil, fmt.Errorf("%w: row %d opens at %s, outside %04d-%02d",
				marketdata.ErrInvalidSource, r.Row, t.Format(time.RFC3339), p.Year, int(p.Month))
		}
		bar, reason, err := classify(key, r, t)
		if err != nil {
			return nil, nil, err
		}
		if reason == "" {
			switch {
			case seen[t.UnixNano()]:
				reason = canonical.ReasonDuplicate
			case hasPrev && t.Before(prev):
				reason = canonical.ReasonOutOfOrder
			}
		}
		prev, hasPrev = t, true
		if reason != "" {
			bad = append(bad, canonical.BadRecord{Time: t, Reason: reason, RawRow: r.Row})
			continue
		}
		seen[t.UnixNano()] = true
		bars = append(bars, bar)
	}
	return bars, bad, nil
}

// classify decides one row on its own merits, before any comparison with its
// neighbours. It returns the finished bar and no reason, or a reason. The first
// reason that applies wins, in this order: incomplete, misaligned, invalid
// (a missing side, an overflow computing the mid, or a bar that fails
// marketdata.Bar.Validate), crossed.
func classify(key canonical.Key, r marketdata.SourceRow, t time.Time) (marketdata.Bar, canonical.Reason, error) {
	if !r.Complete {
		return marketdata.Bar{}, canonical.ReasonIncomplete, nil
	}
	ok, err := canonical.Aligned(key.Interval, t)
	if err != nil {
		return marketdata.Bar{}, "", err
	}
	if !ok {
		return marketdata.Bar{}, canonical.ReasonMisaligned, nil
	}
	avail, err := canonical.AvailableAt(key.Interval, t)
	if err != nil {
		return marketdata.Bar{}, "", err
	}
	b := r.Bar
	b.Instrument, b.Time, b.AvailableAt = key.Symbol, t, avail
	if b.Bid.IsZero() || b.Ask.IsZero() {
		return marketdata.Bar{}, canonical.ReasonInvalid, nil
	}
	mid, err := marketdata.MidOf(b.Bid, b.Ask)
	if err != nil {
		return marketdata.Bar{}, canonical.ReasonInvalid, nil
	}
	b.Mid = mid
	switch err := b.Validate(); {
	case errors.Is(err, marketdata.ErrCrossed):
		return marketdata.Bar{}, canonical.ReasonCrossed, nil
	case err != nil:
		return marketdata.Bar{}, canonical.ReasonInvalid, nil
	}
	return b, "", nil
}
