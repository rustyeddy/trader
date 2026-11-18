package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/marketdata/internal/canonical"
)

// VerifyState is what Verify found overall for one partition.
type VerifyState string

// The verify states.
const (
	// Verified: the canonical file is what the raw partition says it should be.
	Verified VerifyState = "verified"
	// VerifyFailed: the file disagrees with the raw partition; see the findings.
	VerifyFailed VerifyState = "failed"
	// NoCanonical: there is no canonical file to check.
	NoCanonical VerifyState = "no canonical"
)

// FindingKind names what is wrong.
type FindingKind string

// The findings of Verify.
const (
	// FindUnreadable: the file is not valid canonical-v2 (this includes bars that
	// are duplicated, out of order, misaligned, invalid OHLC, or whose
	// availability is not the policy's).
	FindUnreadable FindingKind = "unreadable"
	// FindFingerprint: the file was built from raw bytes other than the ones now
	// on disk; the raw partition changed after the build (or the file is not from
	// it). The row findings say what that changed.
	FindFingerprint FindingKind = "fingerprint"
	// FindStale: the file is from another builder, validator or availability
	// policy than the one in force.
	FindStale FindingKind = "stale"
	// FindUnaccounted: a raw row is neither a bar nor a bad record in the file.
	FindUnaccounted FindingKind = "unaccounted"
	// FindUnexpected: a bar or bad record the raw partition has no row for.
	FindUnexpected FindingKind = "unexpected"
	// FindMismatch: a bar's bid, ask, volume or mid differs from the raw row it
	// came from, or a bad record points at a raw row of another time.
	FindMismatch FindingKind = "mismatch"
	// FindUnjustified: a bar was made from a row that cannot be one (still
	// forming), or a bad record's reason is not true of its raw row.
	FindUnjustified FindingKind = "unjustified"
)

// maxFindings bounds the findings kept for one partition; Omitted counts the rest.
const maxFindings = 100

// Finding is one disagreement between a canonical file and its raw partition.
type Finding struct {
	Kind FindingKind
	// Time is the opening time involved, zero when none.
	Time time.Time
	// RawRow is the raw row involved, 0 when none.
	RawRow int
	Detail string
}

func (f Finding) String() string {
	s := string(f.Kind)
	if !f.Time.IsZero() {
		s += " " + f.Time.UTC().Format("2006-01-02T15:04Z")
	}
	if f.RawRow > 0 {
		s += fmt.Sprintf(" row %d", f.RawRow)
	}
	return s + ": " + f.Detail
}

// Verification is what Verify found for one partition.
type Verification struct {
	Source     string
	Instrument string
	Interval   string
	Year       int
	Month      time.Month
	// Path is the canonical file's path relative to the data root.
	Path  string
	State VerifyState
	// RawRows, Bars and Bad count the raw rows and the file's bars and bad
	// records.
	RawRows, Bars, Bad int
	// Explained counts the bad records whose reason the raw row bears out, by
	// reason: the differences between raw and canonical that are expected.
	Explained map[string]int
	// Findings are the disagreements, in raw order, at most maxFindings of them;
	// Omitted counts the ones left out.
	Findings []Finding
	Omitted  int
}

func (v *Verification) add(f Finding) {
	if len(v.Findings) >= maxFindings {
		v.Omitted++
		return
	}
	v.Findings = append(v.Findings, f)
}

// Verify checks a canonical file against the raw partition it should have been
// built from. It re-derives nothing from Build's own code: it reads the file as it
// is and asks, row by row, whether the raw rows account for it.
//
//   - Every raw row is either the source of exactly one bar or the subject of a
//     bad record, and every bar and bad record has its raw row.
//   - A bar's bid, ask and volume equal its raw row's exactly (prices are exact
//     fixed-point decimals; there is no rounding to tolerate) and its mid is the
//     midpoint of them. A bar is never made from a row that was still forming.
//   - A bad record's reason is true of its raw row: still forming, off the
//     interval boundary, a repeat of a time already a bar, earlier than the row
//     before it, crossed, or invalid. These are the only differences between raw
//     and canonical that are expected, and Explained counts them by reason.
//   - The file was built from these raw bytes (the fingerprint) by the builder,
//     validator and availability policy in force.
//   - The file itself is valid canonical-v2: reading it checks, among other things,
//     that times are strictly ascending (so no duplicates), on the interval
//     boundary, in the month, with valid OHLC and the policy's availability.
//
// A missing file is NoCanonical, not an error; a file that is not readable as
// canonical-v2 is a failed partition with an unreadable finding. Only a failure
// to read that is not about the file's content is an error. Nothing is written.
func Verify(ctx context.Context, p marketdata.SourcePartition) (Verification, error) {
	if err := ctx.Err(); err != nil {
		return Verification{}, err
	}
	if err := p.Validate(); err != nil {
		return Verification{}, err
	}
	interval := canonical.Interval(p.Interval)
	if !interval.Valid() {
		return Verification{}, fmt.Errorf("%w: %q (h1 and d1 only)", ErrUnsupportedInterval, p.Interval)
	}
	key := canonical.Key{Source: p.Source, Symbol: p.Instrument, Interval: interval, Year: p.Year, Month: p.Month}
	path, err := key.Path()
	if err != nil {
		return Verification{}, err
	}
	v := Verification{Source: p.Source, Instrument: p.Instrument, Interval: p.Interval, Year: p.Year, Month: p.Month,
		Path: path, RawRows: len(p.Rows), Explained: map[string]int{}}

	f, err := canonical.Read(ctx, key)
	switch {
	case errors.Is(err, canonical.ErrNotFound):
		v.State = NoCanonical
		return v, nil
	case errors.Is(err, canonical.ErrMalformedData):
		v.State = VerifyFailed
		v.add(Finding{Kind: FindUnreadable, Detail: firstLine(err)})
		return v, nil
	case err != nil:
		return Verification{}, err
	}
	v.Bars, v.Bad = len(f.Bars), len(f.Bad)

	if f.Meta.RawFingerprint != p.Fingerprint {
		v.add(Finding{Kind: FindFingerprint, Detail: fmt.Sprintf("built from %s, the raw partition is %s", f.Meta.RawFingerprint, p.Fingerprint)})
	}
	if reason := staleReason(f.Meta); reason != "" {
		v.add(Finding{Kind: FindStale, Detail: reason})
	}
	if err := verifyRows(ctx, &v, p, f, interval); err != nil {
		return Verification{}, err
	}
	v.State = Verified
	if len(v.Findings) > 0 {
		v.State = VerifyFailed
	}
	return v, nil
}

func verifyRows(ctx context.Context, v *Verification, p marketdata.SourcePartition, f canonical.File, interval canonical.Interval) error {
	bars := make(map[int64]marketdata.Bar, len(f.Bars))
	for _, b := range f.Bars {
		bars[b.Time.UnixNano()] = b
	}
	bad := make(map[int]canonical.BadRecord, len(f.Bad))
	for _, b := range f.Bad {
		bad[b.RawRow] = b
	}
	claimed := make(map[int64]bool, len(f.Bars))
	var prev time.Time

	for i, r := range p.Rows {
		if err := ctx.Err(); err != nil {
			return err
		}
		t := r.Bar.Time.UTC()
		key := t.UnixNano()
		if rec, ok := bad[r.Row]; ok {
			delete(bad, r.Row)
			switch {
			case !rec.Time.Equal(t):
				v.add(Finding{Kind: FindMismatch, Time: t, RawRow: r.Row, Detail: fmt.Sprintf("the bad record is at %s", rec.Time.UTC().Format(time.RFC3339))})
			case !reasonHolds(rec.Reason, interval, r, t, i > 0 && prev.After(t), claimed[key]):
				v.add(Finding{Kind: FindUnjustified, Time: t, RawRow: r.Row, Detail: fmt.Sprintf("recorded as %s, which the raw row does not bear out", rec.Reason)})
			default:
				v.Explained[string(rec.Reason)]++
			}
			prev = t
			continue
		}
		prev = t
		bar, ok := bars[key]
		switch {
		case !ok:
			v.add(Finding{Kind: FindUnaccounted, Time: t, RawRow: r.Row, Detail: "the raw row is neither a bar nor a bad record"})
			continue
		case claimed[key]:
			v.add(Finding{Kind: FindUnaccounted, Time: t, RawRow: r.Row, Detail: "a second raw row at a time already a bar, not recorded as a duplicate"})
			continue
		}
		claimed[key] = true
		if !r.Complete {
			v.add(Finding{Kind: FindUnjustified, Time: t, RawRow: r.Row, Detail: "a bar was made from a candle that was still forming"})
		}
		if bar.Bid != r.Bar.Bid || bar.Ask != r.Bar.Ask || bar.Volume != r.Bar.Volume {
			v.add(Finding{Kind: FindMismatch, Time: t, RawRow: r.Row, Detail: "bid, ask or volume differs from the raw row"})
		}
		if mid, err := marketdata.MidOf(r.Bar.Bid, r.Bar.Ask); err != nil || bar.Mid != mid {
			v.add(Finding{Kind: FindMismatch, Time: t, RawRow: r.Row, Detail: "mid is not the midpoint of the raw bid and ask"})
		}
	}
	for _, rec := range f.Bad {
		if _, left := bad[rec.RawRow]; left {
			v.add(Finding{Kind: FindUnexpected, Time: rec.Time, RawRow: rec.RawRow, Detail: "a bad record for a raw row the partition does not have"})
		}
	}
	for _, b := range f.Bars {
		if !claimed[b.Time.UnixNano()] {
			v.add(Finding{Kind: FindUnexpected, Time: b.Time, Detail: "a bar no raw row accounts for"})
		}
	}
	return nil
}

// reasonHolds reports whether the raw row bears out a bad record's reason.
// earlierIsLater: the row before it opened later; repeat: its time is already a
// bar from an earlier row.
func reasonHolds(reason canonical.Reason, interval canonical.Interval, r marketdata.SourceRow, t time.Time, earlierIsLater, repeat bool) bool {
	switch reason {
	case canonical.ReasonIncomplete:
		return !r.Complete
	case canonical.ReasonMisaligned:
		ok, err := canonical.Aligned(interval, t)
		return err == nil && !ok
	case canonical.ReasonDuplicate:
		return repeat
	case canonical.ReasonOutOfOrder:
		return earlierIsLater
	case canonical.ReasonCrossed, canonical.ReasonInvalid:
		avail, err := canonical.AvailableAt(interval, t)
		if err != nil {
			return false // a misaligned row is not crossed or invalid, it is misaligned
		}
		b := r.Bar
		b.Time, b.AvailableAt = t, avail
		mid, merr := marketdata.MidOf(b.Bid, b.Ask)
		if merr != nil || b.Bid.IsZero() || b.Ask.IsZero() {
			return reason == canonical.ReasonInvalid
		}
		b.Mid = mid
		verr := b.Validate()
		if reason == canonical.ReasonCrossed {
			return errors.Is(verr, marketdata.ErrCrossed)
		}
		return verr != nil && !errors.Is(verr, marketdata.ErrCrossed)
	}
	return false
}
