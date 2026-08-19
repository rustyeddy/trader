package marketdata

import (
	"errors"
	"testing"
	"time"

	"github.com/rustyeddy/trader/marketdata/internal/provider/oanda"
	"github.com/rustyeddy/trader/num"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// oandaRecord builds a well-formed oanda.Record for tests, using p (from
// bar_test.go) to parse exact prices.
func oandaRecord(t *testing.T, at time.Time, bidO, bidH, bidL, bidC, askO, askH, askL, askC string, volume int64, complete bool) oanda.Record {
	t.Helper()
	return oanda.Record{
		Time:     at,
		BidOpen:  p(t, bidO),
		BidHigh:  p(t, bidH),
		BidLow:   p(t, bidL),
		BidClose: p(t, bidC),
		AskOpen:  p(t, askO),
		AskHigh:  p(t, askH),
		AskLow:   p(t, askL),
		AskClose: p(t, askC),
		Volume:   volume,
		Complete: complete,
	}
}

// validOANDARecord returns a well-formed, aligned H1 record at an
// ordinary UTC hour boundary, the tests mutate.
func validOANDARecord(t *testing.T) oanda.Record {
	t.Helper()
	return oandaRecord(t,
		time.Date(2026, time.January, 6, 9, 0, 0, 0, time.UTC),
		"1.10000", "1.10250", "1.09900", "1.10100",
		"1.10012", "1.10262", "1.09912", "1.10112",
		4213, true)
}

func TestSpreadSummary_ExactFormula(t *testing.T) {
	rec := validOANDARecord(t)
	avg, max, _, err := spreadSummary(rec)
	require.NoError(t, err)

	// Corner spreads: O=0.00012, H=0.00012, L=0.00012, C=0.00012.
	assert.Equal(t, p(t, "0.00012"), avg)
	assert.Equal(t, p(t, "0.00012"), max)
}

func TestSpreadSummary_MaxIsLargestCorner(t *testing.T) {
	rec := oandaRecord(t,
		time.Date(2026, time.January, 6, 9, 0, 0, 0, time.UTC),
		"1.10000", "1.10250", "1.09900", "1.10100",
		"1.10010", "1.10260", "1.09930", "1.10112",
		100, true)
	// Corner spreads: O=0.00010, H=0.00010, L=0.00030, C=0.00012.
	avg, max, _, err := spreadSummary(rec)
	require.NoError(t, err)
	assert.Equal(t, p(t, "0.00030"), max)
	// avg = (0.00010+0.00010+0.00030+0.00012)/4 = 0.00062/4 = 0.000155
	assert.Equal(t, p(t, "0.000155"), avg)
}

func TestSpreadSummary_JPYScalePrecision(t *testing.T) {
	rec := oandaRecord(t,
		time.Date(2026, time.January, 6, 9, 0, 0, 0, time.UTC),
		"107.27200", "107.30000", "107.20000", "107.25000",
		"107.28300", "107.31100", "107.20900", "107.25900",
		500, true)
	// Corner spreads: O=0.011, H=0.011, L=0.009, C=0.009.
	avg, max, _, err := spreadSummary(rec)
	require.NoError(t, err)
	assert.Equal(t, p(t, "0.011"), max)
	assert.Equal(t, p(t, "0.010"), avg)
}

func TestSpreadSummary_CrossedMarketReportsError(t *testing.T) {
	rec := validOANDARecord(t)
	rec.AskOpen = p(t, "1.09000") // below BidOpen: crossed
	_, _, crossed, err := spreadSummary(rec)
	assert.Error(t, err)
	assert.True(t, crossed, "a crossed corner must be distinguishable from any other spread failure")
}

func TestNormalizeOANDARecord_Accepted(t *testing.T) {
	rec := validOANDARecord(t)
	nr := normalizeOANDARecord(rec)

	require.Equal(t, recordOutcomeAccepted, nr.outcome)
	require.NoError(t, nr.err)
	assert.Equal(t, rec.Time, nr.bar.Time)
	assert.Equal(t, rec.BidOpen, nr.bar.Open)
	assert.Equal(t, rec.BidHigh, nr.bar.High)
	assert.Equal(t, rec.BidLow, nr.bar.Low)
	assert.Equal(t, rec.BidClose, nr.bar.Close)
	assert.Equal(t, rec.Volume, nr.bar.Ticks)
	require.NoError(t, nr.bar.Validate())
}

func TestNormalizeOANDARecord_Incomplete(t *testing.T) {
	rec := validOANDARecord(t)
	rec.Complete = false
	nr := normalizeOANDARecord(rec)

	assert.Equal(t, recordOutcomeIncomplete, nr.outcome)
	assert.NoError(t, nr.err)
	// The Bar itself is still well-formed; only the outcome marks it
	// non-final.
	assert.NoError(t, nr.bar.Validate())
}

func TestNormalizeOANDARecord_Suspicious(t *testing.T) {
	rec := validOANDARecord(t)
	rec.AskLow = p(t, "1.00000") // below BidLow: crossed at the low corner
	nr := normalizeOANDARecord(rec)

	assert.Equal(t, recordOutcomeSuspicious, nr.outcome)
	assert.Error(t, nr.err)
	assert.Equal(t, Bar{}, nr.bar, "no candidate Bar for a suspicious record")
	assert.Equal(t, rec.Time, nr.time)
}

func TestNormalizeOANDARecord_RejectedBadOHLC(t *testing.T) {
	rec := validOANDARecord(t)
	rec.BidHigh = p(t, "1.00000") // high below low: impossible Bar shape
	rec.AskHigh = p(t, "1.00012")
	nr := normalizeOANDARecord(rec)

	assert.Equal(t, recordOutcomeRejected, nr.outcome)
	assert.ErrorIs(t, nr.err, ErrBarOHLC)
}

func TestNormalizeOANDARecord_RejectedNegativeTicks(t *testing.T) {
	rec := validOANDARecord(t)
	rec.Volume = -1
	nr := normalizeOANDARecord(rec)

	assert.Equal(t, recordOutcomeRejected, nr.outcome)
	assert.ErrorIs(t, nr.err, ErrBarTicks)
}

func TestRecordOutcome_String(t *testing.T) {
	cases := map[recordOutcome]string{
		recordOutcomeUnknown:    "unknown",
		recordOutcomeAccepted:   "accepted",
		recordOutcomeIncomplete: "incomplete",
		recordOutcomeSuspicious: "suspicious",
		recordOutcomeRejected:   "rejected",
		recordOutcome(200):      "recordOutcome(200)",
	}
	for outcome, want := range cases {
		assert.Equal(t, want, outcome.String())
	}
}

// --- canonicalInterval ---

func TestCanonicalInterval(t *testing.T) {
	cases := []struct {
		raw  oanda.RawInterval
		want Interval
	}{
		{oanda.RawM1, M1},
		{oanda.RawH1, H1},
		{oanda.RawH4, H4},
		{oanda.RawD1, D1},
	}
	for _, tc := range cases {
		t.Run(string(tc.raw), func(t *testing.T) {
			got, err := canonicalInterval(tc.raw)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestCanonicalInterval_UnsupportedW1(t *testing.T) {
	_, err := canonicalInterval(oanda.RawInterval("w1"))
	assert.ErrorIs(t, err, errUnsupportedRawInterval)
}

// --- normalizeOANDASequence ---

func h1FXCalendar() *FXCalendar {
	return NewFXCalendar(FXCalendarParams{})
}

func TestNormalizeOANDASequence_NilCalendar(t *testing.T) {
	_, err := normalizeOANDASequence(oanda.RawH1, nil, nil)
	assert.ErrorIs(t, err, ErrNilCalendar)
}

func TestNormalizeOANDASequence_UnsupportedInterval(t *testing.T) {
	_, err := normalizeOANDASequence(oanda.RawInterval("w1"), h1FXCalendar(), nil)
	assert.ErrorIs(t, err, errUnsupportedRawInterval)
}

func TestNormalizeOANDASequence_AllAccepted(t *testing.T) {
	base := time.Date(2026, time.January, 6, 9, 0, 0, 0, time.UTC) // Tuesday, open
	records := []oanda.Record{
		oandaRecord(t, base, "1.1000", "1.1010", "1.0990", "1.1005", "1.1002", "1.1012", "1.0992", "1.1007", 100, true),
		oandaRecord(t, base.Add(time.Hour), "1.1005", "1.1015", "1.0995", "1.1010", "1.1007", "1.1017", "1.0997", "1.1012", 110, true),
	}
	out, err := normalizeOANDASequence(oanda.RawH1, h1FXCalendar(), records)
	require.NoError(t, err)
	require.Len(t, out, 2)
	for _, nr := range out {
		assert.Equal(t, recordOutcomeAccepted, nr.outcome, "%+v", nr)
	}
}

func TestNormalizeOANDASequence_NoRecordsIsEmptyNotError(t *testing.T) {
	out, err := normalizeOANDASequence(oanda.RawH1, h1FXCalendar(), nil)
	require.NoError(t, err)
	assert.Empty(t, out)
}

func TestNormalizeOANDASequence_DuplicateTimeRejected(t *testing.T) {
	base := time.Date(2026, time.January, 6, 9, 0, 0, 0, time.UTC)
	records := []oanda.Record{
		oandaRecord(t, base, "1.1000", "1.1010", "1.0990", "1.1005", "1.1002", "1.1012", "1.0992", "1.1007", 100, true),
		oandaRecord(t, base, "1.1000", "1.1010", "1.0990", "1.1005", "1.1002", "1.1012", "1.0992", "1.1007", 100, true), // duplicate
	}
	out, err := normalizeOANDASequence(oanda.RawH1, h1FXCalendar(), records)
	require.NoError(t, err)
	require.Len(t, out, 2)
	assert.Equal(t, recordOutcomeAccepted, out[0].outcome)
	assert.Equal(t, recordOutcomeRejected, out[1].outcome)
	assert.ErrorIs(t, out[1].err, errRecordDuplicate)
}

// Regression: a duplicate of any earlier record, not only the
// immediately preceding one, must classify errRecordDuplicate rather
// than errRecordOutOfOrder. 09:00, 10:00, 09:00 -- the third record
// repeats the first, not the second.
func TestNormalizeOANDASequence_NonAdjacentDuplicateRejectedAsDuplicate(t *testing.T) {
	base := time.Date(2026, time.January, 6, 9, 0, 0, 0, time.UTC)
	records := []oanda.Record{
		oandaRecord(t, base, "1.1000", "1.1010", "1.0990", "1.1005", "1.1002", "1.1012", "1.0992", "1.1007", 100, true),
		oandaRecord(t, base.Add(time.Hour), "1.1005", "1.1015", "1.0995", "1.1010", "1.1007", "1.1017", "1.0997", "1.1012", 110, true),
		oandaRecord(t, base, "1.1000", "1.1010", "1.0990", "1.1005", "1.1002", "1.1012", "1.0992", "1.1007", 100, true), // repeats records[0]'s time
	}
	out, err := normalizeOANDASequence(oanda.RawH1, h1FXCalendar(), records)
	require.NoError(t, err)
	require.Len(t, out, 3)
	assert.Equal(t, recordOutcomeAccepted, out[0].outcome)
	assert.Equal(t, recordOutcomeAccepted, out[1].outcome)
	assert.Equal(t, recordOutcomeRejected, out[2].outcome)
	assert.ErrorIs(t, out[2].err, errRecordDuplicate, "must be classified as a duplicate, not out-of-order")
	assert.NotErrorIs(t, out[2].err, errRecordOutOfOrder)
}

func TestNormalizeOANDASequence_OutOfOrderRejected(t *testing.T) {
	base := time.Date(2026, time.January, 6, 9, 0, 0, 0, time.UTC)
	records := []oanda.Record{
		oandaRecord(t, base.Add(time.Hour), "1.1000", "1.1010", "1.0990", "1.1005", "1.1002", "1.1012", "1.0992", "1.1007", 100, true),
		oandaRecord(t, base, "1.1000", "1.1010", "1.0990", "1.1005", "1.1002", "1.1012", "1.0992", "1.1007", 100, true), // earlier than the previous record
	}
	out, err := normalizeOANDASequence(oanda.RawH1, h1FXCalendar(), records)
	require.NoError(t, err)
	require.Len(t, out, 2)
	assert.Equal(t, recordOutcomeAccepted, out[0].outcome)
	assert.Equal(t, recordOutcomeRejected, out[1].outcome)
	assert.ErrorIs(t, out[1].err, errRecordOutOfOrder)
}

func TestNormalizeOANDASequence_MisalignedRejected(t *testing.T) {
	// H1 records must fall on the hour; :30 past the hour is misaligned.
	rec := oandaRecord(t,
		time.Date(2026, time.January, 6, 9, 30, 0, 0, time.UTC),
		"1.1000", "1.1010", "1.0990", "1.1005", "1.1002", "1.1012", "1.0992", "1.1007", 100, true)
	out, err := normalizeOANDASequence(oanda.RawH1, h1FXCalendar(), []oanda.Record{rec})
	require.NoError(t, err)
	require.Len(t, out, 1)
	assert.Equal(t, recordOutcomeRejected, out[0].outcome)
	assert.ErrorIs(t, out[0].err, errRecordMisaligned)
}

func TestNormalizeOANDASequence_D1SessionAligned(t *testing.T) {
	// The FX trading day rolls over at 17:00 New York time (ADR-012);
	// verify the D1 alignment check uses the real session boundary, not
	// UTC midnight.
	aligned := time.Date(2026, time.January, 6, 22, 0, 0, 0, time.UTC) // 17:00 NY in January (EST)
	rec := oandaRecord(t, aligned,
		"1.1000", "1.1010", "1.0990", "1.1005", "1.1002", "1.1012", "1.0992", "1.1007", 1000, true)
	out, err := normalizeOANDASequence(oanda.RawD1, h1FXCalendar(), []oanda.Record{rec})
	require.NoError(t, err)
	require.Len(t, out, 1)
	assert.Equal(t, recordOutcomeAccepted, out[0].outcome, "%+v", out[0])
}

func TestNormalizeOANDASequence_D1UTCMidnightMisaligned(t *testing.T) {
	// UTC midnight is not the FX session boundary; a D1 record there
	// must be rejected as misaligned even though it would be a valid H1
	// alignment.
	rec := oandaRecord(t,
		time.Date(2026, time.January, 6, 0, 0, 0, 0, time.UTC),
		"1.1000", "1.1010", "1.0990", "1.1005", "1.1002", "1.1012", "1.0992", "1.1007", 1000, true)
	out, err := normalizeOANDASequence(oanda.RawD1, h1FXCalendar(), []oanda.Record{rec})
	require.NoError(t, err)
	require.Len(t, out, 1)
	assert.Equal(t, recordOutcomeRejected, out[0].outcome)
	assert.ErrorIs(t, out[0].err, errRecordMisaligned)
}

// TestNormalizeOANDASequence_H4RealBoundaryAccepted is the regression
// for issue #99: before ADR-021's fix, every real OANDA H4 record was
// rejected as misaligned (FXCalendar.Bar truncated H4 to UTC midnight,
// which never matches OANDA's actual rollover-anchored H4 boundaries),
// aborting normalizeAndPublish's entire partition build. This timestamp
// is taken directly from the real archive
// (AUDCAD-2006-11-h4.csv's own 2006-11-01T02:00:00Z row, #81/#99's own
// investigation) and must now be accepted.
func TestNormalizeOANDASequence_H4RealBoundaryAccepted(t *testing.T) {
	rec := oandaRecord(t,
		time.Date(2006, time.November, 1, 2, 0, 0, 0, time.UTC),
		"0.86926", "0.87161", "0.86723", "0.87040", "0.86957", "0.87192", "0.86753", "0.87070", 2828, true)
	out, err := normalizeOANDASequence(oanda.RawH4, h1FXCalendar(), []oanda.Record{rec})
	require.NoError(t, err)
	require.Len(t, out, 1)
	assert.Equal(t, recordOutcomeAccepted, out[0].outcome, "%+v", out[0])
}

// TestNormalizeOANDASequence_H4UTCMidnightNowMisaligned is the fix's
// other side: the *old* UTC-midnight H4 grid (00:00, 04:00, ... UTC)
// this issue found was wrong must now itself be rejected as misaligned,
// proving the fix actually changed H4's accepted grid rather than
// merely widening it to accept everything.
func TestNormalizeOANDASequence_H4UTCMidnightNowMisaligned(t *testing.T) {
	rec := oandaRecord(t,
		time.Date(2006, time.November, 1, 0, 0, 0, 0, time.UTC),
		"0.86926", "0.87161", "0.86723", "0.87040", "0.86957", "0.87192", "0.86753", "0.87070", 2828, true)
	out, err := normalizeOANDASequence(oanda.RawH4, h1FXCalendar(), []oanda.Record{rec})
	require.NoError(t, err)
	require.Len(t, out, 1)
	assert.Equal(t, recordOutcomeRejected, out[0].outcome)
	assert.ErrorIs(t, out[0].err, errRecordMisaligned)
}

// 2026-03-08 is the US spring-forward transition (see fxcalendar_test.go);
// the D1 session opening 2026-03-07 17:00 NY still resolves to a single
// correct UTC instant despite the DST shift, and a record there aligns.
func TestNormalizeOANDASequence_D1AcrossSpringForward(t *testing.T) {
	cal := h1FXCalendar()
	span, err := cal.Bar(nyTime(2026, time.March, 8, 10, 0), D1)
	require.NoError(t, err)

	rec := oandaRecord(t, span.Start(),
		"1.1000", "1.1010", "1.0990", "1.1005", "1.1002", "1.1012", "1.0992", "1.1007", 1000, true)
	out, err := normalizeOANDASequence(oanda.RawD1, cal, []oanda.Record{rec})
	require.NoError(t, err)
	require.Len(t, out, 1)
	assert.Equal(t, recordOutcomeAccepted, out[0].outcome, "%+v", out[0])
}

func TestNormalizeOANDASequence_IncompleteStillOrderedAndAligned(t *testing.T) {
	base := time.Date(2026, time.January, 6, 9, 0, 0, 0, time.UTC)
	rec := oandaRecord(t, base, "1.1000", "1.1010", "1.0990", "1.1005", "1.1002", "1.1012", "1.0992", "1.1007", 100, false)
	out, err := normalizeOANDASequence(oanda.RawH1, h1FXCalendar(), []oanda.Record{rec})
	require.NoError(t, err)
	require.Len(t, out, 1)
	assert.Equal(t, recordOutcomeIncomplete, out[0].outcome)
}

func TestNormalizeOANDASequence_RejectedRecordDoesNotBreakSubsequentAlignmentCheck(t *testing.T) {
	base := time.Date(2026, time.January, 6, 9, 0, 0, 0, time.UTC)
	records := []oanda.Record{
		oandaRecord(t, base, "1.1000", "1.0000", "1.0990", "1.1005", "1.1002", "1.0012", "1.0992", "1.1007", 100, true),                // bad OHLC -> rejected
		oandaRecord(t, base.Add(time.Hour), "1.1005", "1.1015", "1.0995", "1.1010", "1.1007", "1.1017", "1.0997", "1.1012", 110, true), // fine
	}
	out, err := normalizeOANDASequence(oanda.RawH1, h1FXCalendar(), records)
	require.NoError(t, err)
	require.Len(t, out, 2)
	assert.Equal(t, recordOutcomeRejected, out[0].outcome)
	assert.Equal(t, recordOutcomeAccepted, out[1].outcome, "%+v", out[1])
}

// failingCalendar is a Calendar whose Bar always fails, for proving a
// genuine evaluation failure is propagated distinctly from a misaligned
// timestamp.
type failingCalendar struct{ err error }

func (failingCalendar) Status(time.Time) Status             { return StatusOpen }
func (failingCalendar) Session(time.Time) (TimeRange, bool) { return TimeRange{}, true }
func (c failingCalendar) Bar(time.Time, Interval) (TimeRange, error) {
	return TimeRange{}, c.err
}

var _ Calendar = failingCalendar{}

// Regression: a Calendar.Bar failure is not evidence of a misaligned
// record -- it means alignment could not be evaluated at all -- so it
// must abort normalizeOANDASequence as its own sequence-level error,
// not be folded into errRecordMisaligned on some per-record outcome.
func TestNormalizeOANDASequence_CalendarFailurePropagatesDistinctlyFromMisalignment(t *testing.T) {
	calErr := errors.New("boom: calendar cannot evaluate this instant")
	rec := validOANDARecord(t)

	_, err := normalizeOANDASequence(oanda.RawH1, failingCalendar{err: calErr}, []oanda.Record{rec})
	require.Error(t, err)
	assert.ErrorIs(t, err, calErr)
	assert.NotErrorIs(t, err, errRecordMisaligned)
}

// spreadSummary's corner-sum uses checked arithmetic; four corners each
// near num.Price's representable maximum overflow when summed, even
// though no single corner's spread does.
func overflowRecord() oanda.Record {
	huge := num.MustParsePrice("60000000000") // 6e10; four of these overflow on summing
	return oanda.Record{
		Time:    time.Date(2026, time.January, 6, 9, 0, 0, 0, time.UTC),
		BidOpen: num.MustParsePrice("0"), AskOpen: huge,
		BidHigh: num.MustParsePrice("0"), AskHigh: huge,
		BidLow: num.MustParsePrice("0"), AskLow: huge,
		BidClose: num.MustParsePrice("0"), AskClose: huge,
		Volume: 1, Complete: true,
	}
}

func TestSpreadSummary_SumOverflowReportsError(t *testing.T) {
	_, _, crossed, err := spreadSummary(overflowRecord())
	assert.Error(t, err)
	assert.False(t, crossed, "an overflow is not a crossed market")
}

// Regression: an overflow (or any spread failure other than a crossed
// corner) must classify recordOutcomeRejected, not
// recordOutcomeSuspicious — Suspicious is reserved for a genuine
// crossed market.
func TestNormalizeOANDARecord_OverflowIsRejectedNotSuspicious(t *testing.T) {
	nr := normalizeOANDARecord(overflowRecord())
	assert.Equal(t, recordOutcomeRejected, nr.outcome)
	assert.Error(t, nr.err)
}
