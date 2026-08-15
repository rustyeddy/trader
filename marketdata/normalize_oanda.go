package marketdata

import (
	"errors"
	"fmt"
	"time"

	"github.com/rustyeddy/trader/marketdata/internal/provider/oanda"
	"github.com/rustyeddy/trader/num"
)

// recordOutcome classifies what happened when one raw OANDA record was
// normalized and validated (issue #76, ADR-020).
//
// recordOutcome, normalizedRecord, and the record-error sentinels below
// are all unexported. normalizeOANDARecord and normalizeOANDASequence
// are themselves unexported entry points with no public consumer yet;
// exporting their result types now would expand and stabilize the
// public marketdata API for values external callers cannot obtain.
// Should Manager later need an operator-facing normalization report,
// that is the right time to design a deliberate, provider-neutral
// public result contract — not this one, promoted as-is.
type recordOutcome uint8

const (
	// recordOutcomeUnknown is recordOutcome's zero value; no function in
	// this file returns it for a record it actually processed.
	recordOutcomeUnknown recordOutcome = iota
	// recordOutcomeAccepted means the record produced a valid,
	// provider-complete, correctly time-ordered and interval-aligned
	// Bar.
	recordOutcomeAccepted
	// recordOutcomeIncomplete means the record produced an otherwise
	// valid Bar, but OANDA's own complete flag was false: the bar had
	// not closed yet as of when the provider recorded it. It is never
	// treated as an accepted, final observation, and — per ADR-020 — is
	// not persisted as a canonical Bar; it is reported as its own
	// outcome instead.
	recordOutcomeIncomplete
	// recordOutcomeSuspicious means the record's own bid/ask values
	// contradict each other — an ask below the corresponding bid at
	// some corner — before a candidate Bar could even be built. This is
	// a data anomaly the raw CSV format cannot rule out on its own,
	// distinct from an otherwise-well-formed Bar shape that Validate
	// rejects, and distinct from an arithmetic failure while summing
	// spreads (which is recordOutcomeRejected: not a market anomaly,
	// but a computation this package could not carry out at all).
	recordOutcomeSuspicious
	// recordOutcomeRejected means the record could not become a valid
	// Bar: an impossible Bar shape (Bar.Validate failed), a spread
	// computation that failed for a reason other than a crossed market,
	// a timestamp that duplicates or precedes an earlier record in the
	// same sequence, or a timestamp that does not align to the
	// partition's interval boundary.
	recordOutcomeRejected
)

// String returns a human-readable recordOutcome name.
func (o recordOutcome) String() string {
	switch o {
	case recordOutcomeUnknown:
		return "unknown"
	case recordOutcomeAccepted:
		return "accepted"
	case recordOutcomeIncomplete:
		return "incomplete"
	case recordOutcomeSuspicious:
		return "suspicious"
	case recordOutcomeRejected:
		return "rejected"
	default:
		return fmt.Sprintf("recordOutcome(%d)", uint8(o))
	}
}

// normalizedRecord is the result of normalizing and validating one raw
// OANDA record. No oanda-native type appears here: time and the fields
// inside bar are Trader's own canonical types.
type normalizedRecord struct {
	outcome recordOutcome
	// bar is populated when outcome is recordOutcomeAccepted or
	// recordOutcomeIncomplete, and is the zero Bar otherwise.
	bar Bar
	// time is the record's source open time, preserved verbatim,
	// regardless of outcome — even a rejected record's timestamp is
	// reported, since ordering/duplicate detection depends on it and a
	// caller diagnosing a rejection needs to know where in the file it
	// happened.
	time time.Time
	// err is non-nil exactly when outcome is recordOutcomeSuspicious or
	// recordOutcomeRejected.
	err error
}

// Sentinel errors returned (wrapped) in normalizedRecord.err.
var (
	// errRecordDuplicate marks a record whose Time repeats any earlier
	// record's Time in the same sequence, not only the immediately
	// preceding one.
	errRecordDuplicate = errors.New("marketdata: duplicate record time")
	// errRecordOutOfOrder marks a record whose Time is not strictly
	// after the previous record's Time in the same sequence.
	errRecordOutOfOrder = errors.New("marketdata: record out of chronological order")
	// errRecordMisaligned marks a record whose Time does not fall on
	// the partition interval's calendar-aligned boundary. It is
	// reported only once Calendar.Bar has successfully computed that
	// boundary and disagrees with the record's Time — never for a
	// Calendar.Bar failure itself, which normalizeOANDASequence
	// propagates as its own sequence-level error instead (a calendar
	// unable to evaluate a boundary is not evidence the record is
	// misaligned).
	errRecordMisaligned = errors.New("marketdata: record time is not interval-aligned")
	// errUnsupportedRawInterval marks a oanda.RawInterval this package
	// cannot map to a canonical Interval.
	errUnsupportedRawInterval = errors.New("marketdata: unsupported raw interval")
)

// canonicalInterval maps raw's raw OANDA interval token to Trader's
// canonical Interval. There is no W1 case: the preserved raw archive has
// no native weekly partition (see oanda.RawInterval), and W1 is a
// derived interval built by resampling canonical D1 (ADR-020), never
// normalized directly from a raw record.
func canonicalInterval(raw oanda.RawInterval) (Interval, error) {
	switch raw {
	case oanda.RawM1:
		return M1, nil
	case oanda.RawH1:
		return H1, nil
	case oanda.RawH4:
		return H4, nil
	case oanda.RawD1:
		return D1, nil
	default:
		return Interval{}, fmt.Errorf("marketdata: normalize: %w: %q", errUnsupportedRawInterval, raw)
	}
}

// normalizeOANDARecord converts one raw OANDA record into a
// normalizedRecord. It never touches float64: every value moves through
// exact num.Price arithmetic, from the already-exact fields oanda.Record
// carries (see oanda's own doc comment) through to the candidate Bar.
func normalizeOANDARecord(rec oanda.Record) normalizedRecord {
	avg, max, crossed, err := spreadSummary(rec)
	if err != nil {
		if crossed {
			return normalizedRecord{outcome: recordOutcomeSuspicious, time: rec.Time, err: err}
		}
		// A non-crossed spread failure (for example, checked-arithmetic
		// overflow while summing) is not a market anomaly — it is this
		// package failing to compute a value at all, which belongs with
		// every other reason a record cannot become a Bar.
		return normalizedRecord{outcome: recordOutcomeRejected, time: rec.Time, err: err}
	}

	bar := Bar{
		Time:      rec.Time,
		Open:      rec.BidOpen,
		High:      rec.BidHigh,
		Low:       rec.BidLow,
		Close:     rec.BidClose,
		AvgSpread: avg,
		MaxSpread: max,
		// Ticks is OANDA's activity/tick count, not a tradable
		// quantity — a plain int64, never num.Quantity (ADR-020).
		Ticks: rec.Volume,
	}
	if err := bar.Validate(); err != nil {
		return normalizedRecord{outcome: recordOutcomeRejected, time: rec.Time, err: err}
	}

	if !rec.Complete {
		return normalizedRecord{outcome: recordOutcomeIncomplete, bar: bar, time: rec.Time}
	}
	return normalizedRecord{outcome: recordOutcomeAccepted, bar: bar, time: rec.Time}
}

// spreadSummary computes AvgSpread and MaxSpread from rec's bid/ask OHLC,
// per the formula ADR-020 settled on: the per-corner spread is
// (ask - bid) at each of open, high, low, and close; AvgSpread is the
// mean of those four, MaxSpread the max. This is the only spread summary
// reconstructible from OANDA's bid/ask OHLC, since raw preserves no
// per-tick spread stream.
//
// When err is non-nil, crossed reports whether it was specifically
// caused by a crossed corner (an ask below its bid, num.ErrNegative from
// num.Price.Sub) — a market anomaly the caller should classify
// recordOutcomeSuspicious — as opposed to any other failure (for
// example, checked-arithmetic overflow while summing), which the caller
// should classify recordOutcomeRejected instead. crossed is meaningless
// when err is nil.
func spreadSummary(rec oanda.Record) (avg, max num.Price, crossed bool, err error) {
	corners := [4]struct{ ask, bid num.Price }{
		{rec.AskOpen, rec.BidOpen},
		{rec.AskHigh, rec.BidHigh},
		{rec.AskLow, rec.BidLow},
		{rec.AskClose, rec.BidClose},
	}

	var spreads [4]num.Price
	for i, c := range corners {
		s, err := c.ask.Sub(c.bid)
		if err != nil {
			return num.Price{}, num.Price{}, errors.Is(err, num.ErrNegative),
				fmt.Errorf("marketdata: normalize: spread: %w", err)
		}
		spreads[i] = s
		if i == 0 || s.Cmp(max) > 0 {
			max = s
		}
	}

	sum := spreads[0]
	for _, s := range spreads[1:] {
		sum, err = sum.Add(s)
		if err != nil {
			return num.Price{}, num.Price{}, false, fmt.Errorf("marketdata: normalize: spread sum: %w", err)
		}
	}
	avg, err = sum.MulRate(num.MustParseRate("0.25"))
	if err != nil {
		return num.Price{}, num.Price{}, false, fmt.Errorf("marketdata: normalize: spread average: %w", err)
	}
	return avg, max, false, nil
}

// normalizeOANDASequence normalizes and validates records — one raw
// OANDA partition's rows, in file order — against rawInterval and cal.
// It returns exactly one normalizedRecord per input record, in input
// order: no record is ever silently dropped, repaired, or coalesced.
//
// Beyond normalizeOANDARecord's own per-record checks, this validates
// two sequence-level invariants no single record can check on its own:
//
//   - No duplicate or out-of-order timestamps: a record whose Time
//     repeats any earlier record's Time in the sequence — not only the
//     immediately preceding one — is recordOutcomeRejected
//     (errRecordDuplicate); a record whose Time precedes the previous
//     record's Time is recordOutcomeRejected (errRecordOutOfOrder). Both
//     checks use each record's raw Time, so a run of corrupt timestamps
//     is still checked relative to the actual input sequence, not just
//     relative to previously accepted records.
//   - Interval alignment: a record whose Time does not fall exactly on
//     cal.Bar(rec.Time, interval)'s computed boundary is
//     recordOutcomeRejected (errRecordMisaligned). A Calendar.Bar
//     failure is different in kind from a misaligned timestamp — it
//     means alignment could not be evaluated at all, not that it failed
//     — so it is not folded into any per-record outcome; it aborts
//     normalizeOANDASequence entirely, as a sequence-level error.
//
// cal must be non-nil; normalizeOANDASequence reports a wrapped
// ErrNilCalendar otherwise. rawInterval must be one this package can map
// to a canonical Interval (see canonicalInterval); otherwise it reports
// a wrapped errUnsupportedRawInterval and normalizes nothing.
func normalizeOANDASequence(rawInterval oanda.RawInterval, cal Calendar, records []oanda.Record) ([]normalizedRecord, error) {
	if cal == nil {
		return nil, fmt.Errorf("marketdata: normalize sequence: %w", ErrNilCalendar)
	}
	interval, err := canonicalInterval(rawInterval)
	if err != nil {
		return nil, err
	}

	out := make([]normalizedRecord, 0, len(records))
	seen := make(map[time.Time]struct{}, len(records))
	var prevTime time.Time
	havePrev := false

	for _, rec := range records {
		nr := normalizeOANDARecord(rec)
		_, isDuplicate := seen[rec.Time]

		switch {
		case isDuplicate:
			nr = normalizedRecord{outcome: recordOutcomeRejected, time: rec.Time,
				err: fmt.Errorf("marketdata: normalize: %s: %w", rec.Time, errRecordDuplicate)}
		case havePrev && rec.Time.Before(prevTime):
			nr = normalizedRecord{outcome: recordOutcomeRejected, time: rec.Time,
				err: fmt.Errorf("marketdata: normalize: %s: %w", rec.Time, errRecordOutOfOrder)}
		}

		if nr.outcome == recordOutcomeAccepted || nr.outcome == recordOutcomeIncomplete {
			span, calErr := cal.Bar(rec.Time, interval)
			if calErr != nil {
				return nil, fmt.Errorf("marketdata: normalize sequence: %s: %w", rec.Time, calErr)
			}
			if !span.Start().Equal(rec.Time) {
				nr = normalizedRecord{outcome: recordOutcomeRejected, time: rec.Time,
					err: fmt.Errorf("marketdata: normalize: %s: %w", rec.Time, errRecordMisaligned)}
			}
		}

		out = append(out, nr)
		seen[rec.Time] = struct{}{}
		prevTime = rec.Time
		havePrev = true
	}
	return out, nil
}
