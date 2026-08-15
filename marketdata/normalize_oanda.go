package marketdata

import (
	"errors"
	"fmt"
	"time"

	"github.com/rustyeddy/trader/marketdata/internal/provider/oanda"
	"github.com/rustyeddy/trader/num"
)

// RecordOutcome classifies what happened when one raw OANDA record was
// normalized and validated (issue #76, ADR-020).
type RecordOutcome uint8

const (
	// RecordOutcomeUnknown is RecordOutcome's zero value; no function in
	// this file returns it for a record it actually processed.
	RecordOutcomeUnknown RecordOutcome = iota
	// RecordOutcomeAccepted means the record produced a valid,
	// provider-complete, correctly time-ordered and interval-aligned
	// Bar.
	RecordOutcomeAccepted
	// RecordOutcomeIncomplete means the record produced an otherwise
	// valid Bar, but OANDA's own complete flag was false: the bar had
	// not closed yet as of when the provider recorded it. It is never
	// treated as an accepted, final observation, and — per ADR-020 — is
	// not persisted as a canonical Bar; it is reported as its own
	// outcome instead.
	RecordOutcomeIncomplete
	// RecordOutcomeSuspicious means the record's own bid/ask values
	// contradict each other — an ask below the corresponding bid at
	// some corner — before a candidate Bar could even be built. This is
	// a data anomaly the raw CSV format cannot rule out on its own,
	// distinct from an otherwise-well-formed Bar shape that Validate
	// rejects.
	RecordOutcomeSuspicious
	// RecordOutcomeRejected means the record could not become a valid
	// Bar: an impossible Bar shape (Bar.Validate failed), a timestamp
	// that duplicates or precedes an earlier record in the same
	// sequence, or a timestamp that does not align to the partition's
	// interval boundary.
	RecordOutcomeRejected
)

// String returns a human-readable RecordOutcome name.
func (o RecordOutcome) String() string {
	switch o {
	case RecordOutcomeUnknown:
		return "unknown"
	case RecordOutcomeAccepted:
		return "accepted"
	case RecordOutcomeIncomplete:
		return "incomplete"
	case RecordOutcomeSuspicious:
		return "suspicious"
	case RecordOutcomeRejected:
		return "rejected"
	default:
		return fmt.Sprintf("RecordOutcome(%d)", uint8(o))
	}
}

// NormalizedRecord is the result of normalizing and validating one raw
// OANDA record. No oanda-native type appears here or anywhere in this
// file's exported surface: Time and the fields inside Bar are Trader's
// own canonical types.
type NormalizedRecord struct {
	Outcome RecordOutcome
	// Bar is populated when Outcome is RecordOutcomeAccepted or
	// RecordOutcomeIncomplete, and is the zero Bar otherwise.
	Bar Bar
	// Time is the record's source open time, preserved verbatim,
	// regardless of Outcome — even a rejected record's timestamp is
	// reported, since ordering/duplicate detection depends on it and a
	// caller diagnosing a rejection needs to know where in the file it
	// happened.
	Time time.Time
	// Err is non-nil exactly when Outcome is RecordOutcomeSuspicious or
	// RecordOutcomeRejected.
	Err error
}

// Sentinel errors returned (wrapped) in NormalizedRecord.Err.
var (
	// ErrRecordDuplicate marks a record whose Time exactly repeats an
	// earlier record's Time in the same sequence.
	ErrRecordDuplicate = errors.New("marketdata: duplicate record time")
	// ErrRecordOutOfOrder marks a record whose Time is not strictly
	// after the previous record's Time in the same sequence.
	ErrRecordOutOfOrder = errors.New("marketdata: record out of chronological order")
	// ErrRecordMisaligned marks a record whose Time does not fall on
	// the partition interval's calendar-aligned boundary.
	ErrRecordMisaligned = errors.New("marketdata: record time is not interval-aligned")
	// ErrUnsupportedRawInterval marks a oanda.RawInterval this package
	// cannot map to a canonical Interval.
	ErrUnsupportedRawInterval = errors.New("marketdata: unsupported raw interval")
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
		return Interval{}, fmt.Errorf("marketdata: normalize: %w: %q", ErrUnsupportedRawInterval, raw)
	}
}

// normalizeOANDARecord converts one raw OANDA record into a
// NormalizedRecord. It never touches float64: every value moves through
// exact num.Price arithmetic, from the already-exact fields oanda.Record
// carries (see oanda's own doc comment) through to the candidate Bar.
func normalizeOANDARecord(rec oanda.Record) NormalizedRecord {
	avg, max, err := spreadSummary(rec)
	if err != nil {
		return NormalizedRecord{Outcome: RecordOutcomeSuspicious, Time: rec.Time, Err: err}
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
		return NormalizedRecord{Outcome: RecordOutcomeRejected, Time: rec.Time, Err: err}
	}

	if !rec.Complete {
		return NormalizedRecord{Outcome: RecordOutcomeIncomplete, Bar: bar, Time: rec.Time}
	}
	return NormalizedRecord{Outcome: RecordOutcomeAccepted, Bar: bar, Time: rec.Time}
}

// spreadSummary computes AvgSpread and MaxSpread from rec's bid/ask OHLC,
// per the formula ADR-020 settled on: the per-corner spread is
// (ask - bid) at each of open, high, low, and close; AvgSpread is the
// mean of those four, MaxSpread the max. This is the only spread summary
// reconstructible from OANDA's bid/ask OHLC, since raw preserves no
// per-tick spread stream.
//
// spreadSummary reports a wrapped num.ErrNegative if any corner's ask is
// below its bid — a crossed market, which normalizeOANDARecord classifies
// RecordOutcomeSuspicious rather than attempting to build a Bar from it.
func spreadSummary(rec oanda.Record) (avg, max num.Price, err error) {
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
			return num.Price{}, num.Price{}, fmt.Errorf("marketdata: normalize: spread: %w", err)
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
			return num.Price{}, num.Price{}, fmt.Errorf("marketdata: normalize: spread sum: %w", err)
		}
	}
	avg, err = sum.MulRate(num.MustParseRate("0.25"))
	if err != nil {
		return num.Price{}, num.Price{}, fmt.Errorf("marketdata: normalize: spread average: %w", err)
	}
	return avg, max, nil
}

// normalizeOANDASequence normalizes and validates records — one raw
// OANDA partition's rows, in file order — against rawInterval and cal.
// It returns exactly one NormalizedRecord per input record, in input
// order: no record is ever silently dropped, repaired, or coalesced.
//
// Beyond normalizeOANDARecord's own per-record checks, this validates
// two sequence-level invariants no single record can check on its own:
//
//   - Strict chronological order with no duplicates: a record whose Time
//     does not strictly follow the previous record's Time is
//     RecordOutcomeRejected (ErrRecordDuplicate or ErrRecordOutOfOrder),
//     regardless of whether its own Bar shape was otherwise valid. The
//     comparison uses each record's raw Time, so a run of corrupt
//     timestamps is still checked relative to the actual input sequence,
//     not just relative to previously accepted records.
//   - Interval alignment: a record whose Time does not fall exactly on
//     cal.Bar(rec.Time, interval)'s computed boundary is
//     RecordOutcomeRejected (ErrRecordMisaligned).
//
// cal must be non-nil; normalizeOANDASequence reports a wrapped
// ErrNilCalendar otherwise. rawInterval must be one this package can map
// to a canonical Interval (see canonicalInterval); otherwise it reports
// a wrapped ErrUnsupportedRawInterval and normalizes nothing.
func normalizeOANDASequence(rawInterval oanda.RawInterval, cal Calendar, records []oanda.Record) ([]NormalizedRecord, error) {
	if cal == nil {
		return nil, fmt.Errorf("marketdata: normalize sequence: %w", ErrNilCalendar)
	}
	interval, err := canonicalInterval(rawInterval)
	if err != nil {
		return nil, err
	}

	out := make([]NormalizedRecord, 0, len(records))
	var prevTime time.Time
	havePrev := false

	for _, rec := range records {
		nr := normalizeOANDARecord(rec)

		if havePrev {
			switch {
			case rec.Time.Equal(prevTime):
				nr = NormalizedRecord{Outcome: RecordOutcomeRejected, Time: rec.Time,
					Err: fmt.Errorf("marketdata: normalize: %s: %w", rec.Time, ErrRecordDuplicate)}
			case rec.Time.Before(prevTime):
				nr = NormalizedRecord{Outcome: RecordOutcomeRejected, Time: rec.Time,
					Err: fmt.Errorf("marketdata: normalize: %s: %w", rec.Time, ErrRecordOutOfOrder)}
			}
		}

		if nr.Outcome == RecordOutcomeAccepted || nr.Outcome == RecordOutcomeIncomplete {
			span, err := cal.Bar(rec.Time, interval)
			if err != nil || !span.Start().Equal(rec.Time) {
				nr = NormalizedRecord{Outcome: RecordOutcomeRejected, Time: rec.Time,
					Err: fmt.Errorf("marketdata: normalize: %s: %w", rec.Time, ErrRecordMisaligned)}
			}
		}

		out = append(out, nr)
		prevTime = rec.Time
		havePrev = true
	}
	return out, nil
}
