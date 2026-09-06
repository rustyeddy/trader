package marketdata

import (
	"fmt"
	"time"

	"github.com/rustyeddy/trader/marketdata/internal/provider/stooq"
)

// normalizeStooqRecord converts one raw Stooq record into a
// normalizedRecord. Unlike normalizeOANDARecord, there is no spread to
// compute (AvgSpread/MaxSpread stay the zero num.Price — ADR-047) and
// no provider "complete" flag to interpret: every row Stooq preserves
// is, by construction, a closed trading day, so recordOutcomeIncomplete
// is never produced here.
func normalizeStooqRecord(rec stooq.Record) normalizedRecord {
	bar := Bar{
		Time:  rec.Time,
		Open:  rec.Open,
		High:  rec.High,
		Low:   rec.Low,
		Close: rec.Close,
		// AvgSpread and MaxSpread are left at their zero num.Price:
		// Stooq has no bid/ask history to derive a spread from
		// (ADR-047's known, documented limitation).
		Ticks: rec.Volume,
	}
	if err := bar.Validate(); err != nil {
		return normalizedRecord{outcome: recordOutcomeRejected, time: rec.Time, err: err}
	}
	return normalizedRecord{outcome: recordOutcomeAccepted, bar: bar, time: rec.Time}
}

// normalizeStooqSequence normalizes and validates records — one raw
// Stooq partition's rows, in file order — against cal, mirroring
// normalizeOANDASequence's sequence-level checks exactly: no duplicate
// or out-of-order timestamps, and interval alignment against
// cal.Bar(rec.Time, D1).
//
// # Calendar alignment
//
// Until issue #296 (EQ-03) landed, this function trusted each record's
// own date verbatim, specifically because the only Calendar available
// then (FXCalendar) would have rejected every real Stooq record: its
// D1 boundary is the FX daily rollover (17:00 America/New_York,
// ADR-021), which has no relationship to Stooq's own midnight-UTC
// daily bars. cal is now expected to be a USEquityCalendar (or another
// Calendar whose D1 boundary actually agrees with the provider's own
// convention) — see USEquityCalendar's own doc comment for why its D1
// anchor is midnight UTC specifically to agree with Stooq. A record
// whose Time does not fall exactly on cal.Bar(rec.Time, D1)'s computed
// boundary is recordOutcomeRejected (errRecordMisaligned), the same
// outcome and sentinel normalizeOANDASequence already uses for the
// identical check.
//
// cal must be non-nil; normalizeStooqSequence reports a wrapped
// ErrNilCalendar otherwise.
func normalizeStooqSequence(cal Calendar, records []stooq.Record) ([]normalizedRecord, error) {
	if cal == nil {
		return nil, fmt.Errorf("marketdata: normalize sequence: %w", ErrNilCalendar)
	}

	out := make([]normalizedRecord, 0, len(records))
	seen := make(map[time.Time]struct{}, len(records))
	var prevTime time.Time
	havePrev := false

	for _, rec := range records {
		nr := normalizeStooqRecord(rec)
		_, isDuplicate := seen[rec.Time]

		switch {
		case isDuplicate:
			nr = normalizedRecord{outcome: recordOutcomeRejected, time: rec.Time,
				err: fmt.Errorf("marketdata: normalize: %s: %w", rec.Time, errRecordDuplicate)}
		case havePrev && rec.Time.Before(prevTime):
			nr = normalizedRecord{outcome: recordOutcomeRejected, time: rec.Time,
				err: fmt.Errorf("marketdata: normalize: %s: %w", rec.Time, errRecordOutOfOrder)}
		}

		if nr.outcome == recordOutcomeAccepted {
			span, calErr := cal.Bar(rec.Time, D1)
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
