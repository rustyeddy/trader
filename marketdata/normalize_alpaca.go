package marketdata

import (
	"fmt"
	"time"

	"github.com/rustyeddy/trader/marketdata/internal/provider/alpaca"
)

// normalizeAlpacaRecord converts one raw Alpaca record into a
// normalizedRecord. Mirrors normalizeStooqRecord exactly: there is no
// spread to compute (AvgSpread/MaxSpread stay the zero num.Price,
// ADR-047's precedent) and no provider "complete" flag to interpret —
// every Record this package's client writes already represents a
// fully closed trading day (alpaca.Partition.LastComplete's own doc
// comment), so recordOutcomeIncomplete is never produced here.
func normalizeAlpacaRecord(rec alpaca.Record) normalizedRecord {
	bar := Bar{
		Time:  rec.Time,
		Open:  rec.Open,
		High:  rec.High,
		Low:   rec.Low,
		Close: rec.Close,
		Ticks: rec.Volume,
	}
	if err := bar.Validate(); err != nil {
		return normalizedRecord{outcome: recordOutcomeRejected, time: rec.Time, err: err}
	}
	return normalizedRecord{outcome: recordOutcomeAccepted, bar: bar, time: rec.Time}
}

// normalizeAlpacaSequence normalizes and validates records — one raw
// Alpaca partition's rows, in file order — against cal, mirroring
// normalizeStooqSequence's sequence-level checks exactly: no duplicate
// or out-of-order timestamps, and interval alignment against
// cal.Bar(rec.Time, D1). cal is expected to be a USEquityCalendar
// (or another Calendar whose D1 boundary agrees with Alpaca's own
// midnight-UTC-of-trading-date convention, after wireshape.go's
// re-anchoring) — see readAndNormalizeRaw's alpaca case for the
// verification that cal actually is one.
//
// cal must be non-nil; normalizeAlpacaSequence reports a wrapped
// ErrNilCalendar otherwise.
func normalizeAlpacaSequence(cal Calendar, records []alpaca.Record) ([]normalizedRecord, error) {
	if cal == nil {
		return nil, fmt.Errorf("marketdata: normalize sequence: %w", ErrNilCalendar)
	}

	out := make([]normalizedRecord, 0, len(records))
	seen := make(map[time.Time]struct{}, len(records))
	var prevTime time.Time
	havePrev := false

	for _, rec := range records {
		nr := normalizeAlpacaRecord(rec)
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
