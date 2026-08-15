package marketdata

import (
	"testing"
	"time"

	"github.com/rustyeddy/trader/marketdata/internal/provider/oanda"
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
	avg, max, err := spreadSummary(rec)
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
	avg, max, err := spreadSummary(rec)
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
	avg, max, err := spreadSummary(rec)
	require.NoError(t, err)
	assert.Equal(t, p(t, "0.011"), max)
	assert.Equal(t, p(t, "0.010"), avg)
}

func TestSpreadSummary_CrossedMarketReportsError(t *testing.T) {
	rec := validOANDARecord(t)
	rec.AskOpen = p(t, "1.09000") // below BidOpen: crossed
	_, _, err := spreadSummary(rec)
	assert.Error(t, err)
}

func TestNormalizeOANDARecord_Accepted(t *testing.T) {
	rec := validOANDARecord(t)
	nr := normalizeOANDARecord(rec)

	require.Equal(t, RecordOutcomeAccepted, nr.Outcome)
	require.NoError(t, nr.Err)
	assert.Equal(t, rec.Time, nr.Bar.Time)
	assert.Equal(t, rec.BidOpen, nr.Bar.Open)
	assert.Equal(t, rec.BidHigh, nr.Bar.High)
	assert.Equal(t, rec.BidLow, nr.Bar.Low)
	assert.Equal(t, rec.BidClose, nr.Bar.Close)
	assert.Equal(t, rec.Volume, nr.Bar.Ticks)
	require.NoError(t, nr.Bar.Validate())
}

func TestNormalizeOANDARecord_Incomplete(t *testing.T) {
	rec := validOANDARecord(t)
	rec.Complete = false
	nr := normalizeOANDARecord(rec)

	assert.Equal(t, RecordOutcomeIncomplete, nr.Outcome)
	assert.NoError(t, nr.Err)
	// The Bar itself is still well-formed; only the outcome marks it
	// non-final.
	assert.NoError(t, nr.Bar.Validate())
}

func TestNormalizeOANDARecord_Suspicious(t *testing.T) {
	rec := validOANDARecord(t)
	rec.AskLow = p(t, "1.00000") // below BidLow: crossed at the low corner
	nr := normalizeOANDARecord(rec)

	assert.Equal(t, RecordOutcomeSuspicious, nr.Outcome)
	assert.Error(t, nr.Err)
	assert.Equal(t, Bar{}, nr.Bar, "no candidate Bar for a suspicious record")
	assert.Equal(t, rec.Time, nr.Time)
}

func TestNormalizeOANDARecord_RejectedBadOHLC(t *testing.T) {
	rec := validOANDARecord(t)
	rec.BidHigh = p(t, "1.00000") // high below low: impossible Bar shape
	rec.AskHigh = p(t, "1.00012")
	nr := normalizeOANDARecord(rec)

	assert.Equal(t, RecordOutcomeRejected, nr.Outcome)
	assert.ErrorIs(t, nr.Err, ErrBarOHLC)
}

func TestNormalizeOANDARecord_RejectedNegativeTicks(t *testing.T) {
	rec := validOANDARecord(t)
	rec.Volume = -1
	nr := normalizeOANDARecord(rec)

	assert.Equal(t, RecordOutcomeRejected, nr.Outcome)
	assert.ErrorIs(t, nr.Err, ErrBarTicks)
}

func TestRecordOutcome_String(t *testing.T) {
	cases := map[RecordOutcome]string{
		RecordOutcomeUnknown:    "unknown",
		RecordOutcomeAccepted:   "accepted",
		RecordOutcomeIncomplete: "incomplete",
		RecordOutcomeSuspicious: "suspicious",
		RecordOutcomeRejected:   "rejected",
		RecordOutcome(200):      "RecordOutcome(200)",
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
	assert.ErrorIs(t, err, ErrUnsupportedRawInterval)
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
	assert.ErrorIs(t, err, ErrUnsupportedRawInterval)
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
		assert.Equal(t, RecordOutcomeAccepted, nr.Outcome, "%+v", nr)
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
	assert.Equal(t, RecordOutcomeAccepted, out[0].Outcome)
	assert.Equal(t, RecordOutcomeRejected, out[1].Outcome)
	assert.ErrorIs(t, out[1].Err, ErrRecordDuplicate)
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
	assert.Equal(t, RecordOutcomeAccepted, out[0].Outcome)
	assert.Equal(t, RecordOutcomeRejected, out[1].Outcome)
	assert.ErrorIs(t, out[1].Err, ErrRecordOutOfOrder)
}

func TestNormalizeOANDASequence_MisalignedRejected(t *testing.T) {
	// H1 records must fall on the hour; :30 past the hour is misaligned.
	rec := oandaRecord(t,
		time.Date(2026, time.January, 6, 9, 30, 0, 0, time.UTC),
		"1.1000", "1.1010", "1.0990", "1.1005", "1.1002", "1.1012", "1.0992", "1.1007", 100, true)
	out, err := normalizeOANDASequence(oanda.RawH1, h1FXCalendar(), []oanda.Record{rec})
	require.NoError(t, err)
	require.Len(t, out, 1)
	assert.Equal(t, RecordOutcomeRejected, out[0].Outcome)
	assert.ErrorIs(t, out[0].Err, ErrRecordMisaligned)
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
	assert.Equal(t, RecordOutcomeAccepted, out[0].Outcome, "%+v", out[0])
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
	assert.Equal(t, RecordOutcomeRejected, out[0].Outcome)
	assert.ErrorIs(t, out[0].Err, ErrRecordMisaligned)
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
	assert.Equal(t, RecordOutcomeAccepted, out[0].Outcome, "%+v", out[0])
}

func TestNormalizeOANDASequence_IncompleteStillOrderedAndAligned(t *testing.T) {
	base := time.Date(2026, time.January, 6, 9, 0, 0, 0, time.UTC)
	rec := oandaRecord(t, base, "1.1000", "1.1010", "1.0990", "1.1005", "1.1002", "1.1012", "1.0992", "1.1007", 100, false)
	out, err := normalizeOANDASequence(oanda.RawH1, h1FXCalendar(), []oanda.Record{rec})
	require.NoError(t, err)
	require.Len(t, out, 1)
	assert.Equal(t, RecordOutcomeIncomplete, out[0].Outcome)
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
	assert.Equal(t, RecordOutcomeRejected, out[0].Outcome)
	assert.Equal(t, RecordOutcomeAccepted, out[1].Outcome, "%+v", out[1])
}

// spreadSummary's corner-sum uses checked arithmetic; four corners each
// near num.Price's representable maximum overflow when summed, even
// though no single corner's spread does.
func TestSpreadSummary_SumOverflowReportsError(t *testing.T) {
	huge := p(t, "60000000000") // 6e10; four of these overflow on summing
	rec := oanda.Record{
		Time:    time.Date(2026, time.January, 6, 9, 0, 0, 0, time.UTC),
		BidOpen: p(t, "0"), AskOpen: huge,
		BidHigh: p(t, "0"), AskHigh: huge,
		BidLow: p(t, "0"), AskLow: huge,
		BidClose: p(t, "0"), AskClose: huge,
		Volume: 1, Complete: true,
	}
	_, _, err := spreadSummary(rec)
	assert.Error(t, err)
}
