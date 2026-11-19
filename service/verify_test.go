package service

import (
	"context"
	"testing"
	"time"

	"github.com/rustyeddy/trader/internal/store"
	"github.com/rustyeddy/trader/internal/store/storetest"
	"github.com/rustyeddy/trader/provider/oanda"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func verifyReq(instruments ...string) VerifyRequest {
	return VerifyRequest{Instruments: instruments, Interval: "h1", From: utc(2024, 1, 1, 0), To: utc(2024, 3, 1, 0)}
}

func verify(t *testing.T, req VerifyRequest) ([]PartitionVerification, error) {
	t.Helper()
	var got []PartitionVerification
	err := VerifyCanonical(context.Background(), nil, req, func(r PartitionVerification) { got = append(got, r) })
	return got, err
}

func TestVerifyCanonical_AFreshBuildVerifiesMonthByMonthInOrder(t *testing.T) {
	storetest.Root(t)
	buildHours(t, "EURUSD", utc(2024, 1, 31, 22), 4) // Jan 31 22:00 to Feb 1 01:00
	buildHours(t, "GBPUSD", utc(2024, 1, 31, 22), 4)

	got, err := verify(t, verifyReq("GBPUSD", "EURUSD"))

	require.NoError(t, err)
	require.Len(t, got, 4)
	for i, want := range []struct {
		symbol string
		month  time.Month
		rows   int
	}{{"GBPUSD", time.January, 2}, {"GBPUSD", time.February, 2}, {"EURUSD", time.January, 2}, {"EURUSD", time.February, 2}} {
		assert.Equal(t, want.symbol, got[i].Instrument)
		assert.Equal(t, want.month, got[i].Month)
		assert.Equal(t, VerifyOK, got[i].State)
		assert.Equal(t, want.rows, got[i].RawRows)
		assert.Equal(t, want.rows, got[i].Bars)
		assert.Empty(t, got[i].Findings)
	}
}

func TestVerifyCanonical_ARawFileThatChangedAfterTheBuildFailsTheRun(t *testing.T) {
	storetest.Root(t)
	putRaw(t, "EURUSD", 2024, time.January, rec(utc(2024, 1, 9, 0), true), rec(utc(2024, 1, 9, 1), true))
	require.NoError(t, BuildCanonical(context.Background(), nil, janToMar("EURUSD"), nil))
	// A later download adds a row to the raw: the canonical file is now behind it.
	key := oanda.PartitionKey{Symbol: "EURUSD", Interval: oanda.RawH1, Year: 2024, Month: time.January}
	require.NoError(t, oanda.WritePartition(context.Background(), key,
		[]oanda.Record{rec(utc(2024, 1, 9, 0), true), rec(utc(2024, 1, 9, 1), true), rec(utc(2024, 1, 9, 2), true)}, false))

	got, err := verify(t, verifyReq("EURUSD"))

	require.ErrorIs(t, err, ErrVerifyFailed)
	require.Len(t, got, 2)
	assert.Equal(t, VerifyBad, got[0].State)
	var kinds []FindingKind
	for _, f := range got[0].Findings {
		kinds = append(kinds, f.Kind)
	}
	assert.Equal(t, []FindingKind{"fingerprint", "unaccounted"}, kinds)
	assert.Equal(t, VerifyNoRaw, got[1].State, "February has no raw; that is reported, not a failure")
}

func TestVerifyCanonical_ARawMonthWithNoCanonicalFileIsReportedNotFailed(t *testing.T) {
	storetest.Root(t)
	putRaw(t, "EURUSD", 2024, time.January, rec(utc(2024, 1, 9, 0), true))

	got, err := verify(t, verifyReq("EURUSD"))

	require.NoError(t, err)
	assert.Equal(t, VerifyNoCanonical, got[0].State)
	assert.Equal(t, 1, got[0].RawRows)
}

func TestVerifyCanonical_AnUnreadableRawFileFailsThatPartitionAndTheRunCarriesOn(t *testing.T) {
	storetest.Root(t)
	putRaw(t, "EURUSD", 2024, time.February, rec(utc(2024, 2, 1, 0), true))
	require.NoError(t, BuildCanonical(context.Background(), nil, janToMar("EURUSD"), nil))
	key := oanda.PartitionKey{Symbol: "EURUSD", Interval: oanda.RawH1, Year: 2024, Month: time.January}
	path, err := key.Path()
	require.NoError(t, err)
	require.NoError(t, store.Write(context.Background(), path, []byte("not a raw file\n")))

	got, verr := verify(t, verifyReq("EURUSD"))

	require.ErrorIs(t, verr, ErrVerifyFailed)
	require.Len(t, got, 2)
	assert.Equal(t, VerifyBad, got[0].State)
	assert.Error(t, got[0].Err)
	assert.Equal(t, VerifyOK, got[1].State, "February was still checked")
}

func TestVerifyCanonical_TheExpectedDifferencesAreCountedNotFailed(t *testing.T) {
	storetest.Root(t)
	putRaw(t, "EURUSD", 2024, time.January, rec(utc(2024, 1, 9, 0), true), rec(utc(2024, 1, 9, 1), false))
	require.NoError(t, BuildCanonical(context.Background(), nil, janToMar("EURUSD"), nil))
	req := verifyReq("EURUSD")
	req.To = utc(2024, 2, 1, 0)

	got, err := verify(t, req)

	require.NoError(t, err)
	assert.Equal(t, VerifyOK, got[0].State)
	assert.Equal(t, map[string]int{"incomplete": 1}, got[0].Explained)
	assert.Equal(t, 1, got[0].Bad)
}

func TestSampleMonths(t *testing.T) {
	months := monthsOf(utc(2020, 1, 1, 0), utc(2021, 1, 1, 0))
	require.Len(t, months, 12)
	tests := []struct {
		n    int
		want []int // month numbers
	}{
		{0, []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}},
		{12, []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}},
		{50, []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}},
		{1, []int{1}},
		{2, []int{1, 12}},
		{3, []int{1, 6, 12}},
		{4, []int{1, 4, 8, 12}},
	}
	for _, tc := range tests {
		var got []int
		for _, m := range sampleMonths(months, tc.n) {
			got = append(got, int(m.Month()))
		}
		assert.Equal(t, tc.want, got, "sample %d", tc.n)
	}
	assert.Equal(t, sampleMonths(months, 4), sampleMonths(months, 4), "the same months every time")
}

func TestVerifyCanonical_ASampleChecksOnlyThoseMonthsInFull(t *testing.T) {
	storetest.Root(t)
	putRaw(t, "EURUSD", 2024, time.January, rec(utc(2024, 1, 9, 0), true))
	putRaw(t, "EURUSD", 2024, time.February, rec(utc(2024, 2, 9, 0), true))
	putRaw(t, "EURUSD", 2024, time.March, rec(utc(2024, 3, 11, 0), true))
	req := verifyReq("EURUSD")
	req.To = utc(2024, 4, 1, 0)
	req.Sample = 2
	require.NoError(t, BuildCanonical(context.Background(), nil, BuildRequest{Instruments: req.Instruments, Interval: "h1", From: req.From, To: req.To}, nil))

	got, err := verify(t, req)

	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, time.January, got[0].Month)
	assert.Equal(t, time.March, got[1].Month, "the first and the last")
}

func TestVerifyCanonical_RefusesWhatItCannotRun(t *testing.T) {
	storetest.Root(t)
	reversed := verifyReq("EURUSD")
	reversed.From, reversed.To = reversed.To, reversed.From
	negative := verifyReq("EURUSD")
	negative.Sample = -1
	badInterval := verifyReq("EURUSD")
	badInterval.Interval = "x9"
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	for name, req := range map[string]VerifyRequest{"reversed range": reversed, "negative sample": negative, "bad interval": badInterval} {
		t.Run(name, func(t *testing.T) {
			err := VerifyCanonical(context.Background(), nil, req, func(PartitionVerification) { t.Fatal("nothing is checked") })
			assert.Error(t, err)
		})
	}
	assert.ErrorIs(t, VerifyCanonical(cancelled, nil, verifyReq("EURUSD"), nil), context.Canceled)
}
