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
// Stooq partition's rows, in file order — mirroring
// normalizeOANDASequence's sequence-level checks (no duplicate or
// out-of-order timestamps) with one deliberate omission: interval-
// alignment validation against a Calendar.
//
// # Why no calendar-alignment check (yet)
//
// normalizeOANDASequence rejects a record whose Time does not fall
// exactly on cal.Bar(rec.Time, interval)'s computed boundary. Applying
// that same check here, against the only Calendar this package has
// today (FXCalendar), would reject every Stooq record: FXCalendar's D1
// boundary is the FX daily rollover (17:00 America/New_York, ADR-021),
// which has no relationship to a NYSE/Nasdaq trading day. There is no
// real U.S. equity trading calendar yet — ADR-047 defers it to EQ-03
// (#296) — so this function trusts each record's own date verbatim,
// the same documented, deliberate limitation stooq.Record.Time's own
// doc comment states. Once a real equity Calendar exists, adding the
// identical alignment check normalizeOANDASequence already performs is
// the natural follow-up, not a redesign of this function's shape.
func normalizeStooqSequence(records []stooq.Record) ([]normalizedRecord, error) {
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

		out = append(out, nr)
		seen[rec.Time] = struct{}{}
		prevTime = rec.Time
		havePrev = true
	}
	return out, nil
}
