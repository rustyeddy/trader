package service

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rustyeddy/trader/provider/oanda"
	"github.com/rustyeddy/trader/store/storetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func auditReq(instruments ...string) AuditRequest {
	return AuditRequest{Instruments: instruments, Interval: "h1", From: utc(2024, 1, 1, 0), To: utc(2024, 2, 1, 0)}
}

func TestAudit_SoundDataPassesAndIsCounted(t *testing.T) {
	storetest.Root(t)
	buildHours(t, "EURUSD", utc(2024, 1, 9, 0), 5)

	got, err := Audit(context.Background(), nil, auditReq("EURUSD"))

	require.NoError(t, err)
	assert.True(t, got.OK)
	require.Len(t, got.Instruments, 1)
	ia := got.Instruments[0]
	assert.True(t, ia.OK)
	assert.Equal(t, 5, ia.Bars)
	assert.Empty(t, ia.Gaps)
	require.Len(t, ia.Months, 1)
	assert.Equal(t, "loaded", ia.Months[0].State)
	require.Len(t, ia.Raw, 1)
	assert.Equal(t, "verified", ia.Raw[0].State)
	assert.Equal(t, AuditSummary{Months: 1, MonthsLoaded: 1, Bars: 5, RawVerified: 1, RawChecked: 1}, got.Summary)
}

func TestAudit_AnUnexplainedGapFailsUnlessAllowedAndIsStillReported(t *testing.T) {
	storetest.Root(t)
	putRaw(t, "EURUSD", 2024, time.January, rec(utc(2024, 1, 9, 0), true), rec(utc(2024, 1, 9, 1), true), rec(utc(2024, 1, 9, 5), true))
	require.NoError(t, BuildCanonical(context.Background(), nil, janToMar("EURUSD"), nil))

	strict, err := Audit(context.Background(), nil, auditReq("EURUSD"))
	allowedReq := auditReq("EURUSD")
	allowedReq.AllowMissing = true
	allowed, aerr := Audit(context.Background(), nil, allowedReq)

	require.ErrorIs(t, err, ErrAuditFailed)
	assert.False(t, strict.OK)
	assert.NotEmpty(t, strict.Instruments[0].LoadError)
	assert.Equal(t, 1, strict.Summary.GapsUnexplained)
	assert.Equal(t, []AuditGap{{Kind: "unexplained", Start: utc(2024, 1, 9, 2), End: utc(2024, 1, 9, 5), Bars: 3}}, strict.Instruments[0].Gaps)
	assert.Equal(t, 1, strict.Summary.RawVerified, "the raw cross-check still ran and passed")
	require.NoError(t, aerr)
	assert.True(t, allowed.OK)
	assert.Equal(t, strict.Instruments[0].Gaps, allowed.Instruments[0].Gaps, "an allowed gap is still shown")
}

func TestAudit_AYearEndHolidayIsReportedAsAClosureAndPasses(t *testing.T) {
	storetest.Root(t)
	putRaw(t, "EURUSD", 2024, time.December,
		rec(utc(2024, 12, 24, 20), true), rec(utc(2024, 12, 24, 21), true), rec(utc(2024, 12, 25, 22), true), rec(utc(2024, 12, 25, 23), true))
	require.NoError(t, BuildCanonical(context.Background(), nil,
		BuildRequest{Instruments: []string{"EURUSD"}, Interval: "h1", From: utc(2024, 12, 1, 0), To: utc(2025, 1, 1, 0)}, nil))
	req := auditReq("EURUSD")
	req.From, req.To = utc(2024, 12, 1, 0), utc(2025, 1, 1, 0)

	got, err := Audit(context.Background(), nil, req)

	require.NoError(t, err)
	assert.Equal(t, 1, got.Summary.GapsClosure)
	assert.Equal(t, 0, got.Summary.GapsUnexplained)
	assert.Equal(t, "closure", got.Instruments[0].Gaps[0].Kind)
}

func TestAudit_ADisagreementWithRawFailsAndNamesTheFinding(t *testing.T) {
	storetest.Root(t)
	putRaw(t, "EURUSD", 2024, time.January, rec(utc(2024, 1, 9, 0), true), rec(utc(2024, 1, 9, 1), true))
	require.NoError(t, BuildCanonical(context.Background(), nil, janToMar("EURUSD"), nil))
	key := oanda.PartitionKey{Symbol: "EURUSD", Interval: oanda.RawH1, Year: 2024, Month: time.January}
	require.NoError(t, oanda.WritePartition(context.Background(), key,
		[]oanda.Record{rec(utc(2024, 1, 9, 0), true), rec(utc(2024, 1, 9, 1), true), rec(utc(2024, 1, 9, 2), true)}, false))

	got, err := Audit(context.Background(), nil, auditReq("EURUSD"))

	require.ErrorIs(t, err, ErrAuditFailed)
	assert.Equal(t, 1, got.Summary.RawFailed)
	assert.Equal(t, 1, got.Summary.InstrumentsFailing)
	require.Len(t, got.Instruments[0].Raw, 1)
	var kinds []string
	for _, f := range got.Instruments[0].Raw[0].Findings {
		kinds = append(kinds, f.Kind)
	}
	assert.Equal(t, []string{"fingerprint", "unaccounted"}, kinds)
	assert.Equal(t, 2, got.Summary.RawFindings)
}

func TestAudit_AMissingMonthFailsUnlessAllowedAndABadMonthToo(t *testing.T) {
	storetest.Root(t)
	putRaw(t, "EURUSD", 2024, time.January, rec(utc(2024, 1, 9, 0), true), rec(utc(2024, 1, 9, 1), false))
	require.NoError(t, BuildCanonical(context.Background(), nil, janToMar("EURUSD"), nil))
	req := auditReq("EURUSD")
	req.To = utc(2024, 3, 1, 0) // February has nothing

	strict, err := Audit(context.Background(), nil, req)
	req.AllowBad, req.AllowMissing = true, true
	allowed, aerr := Audit(context.Background(), nil, req)

	require.ErrorIs(t, err, ErrAuditFailed)
	assert.Equal(t, 1, strict.Summary.MonthsBad)
	assert.Equal(t, 1, strict.Summary.MonthsMissing)
	require.NoError(t, aerr)
	assert.True(t, allowed.OK)
	assert.Equal(t, 1, allowed.Summary.MonthsBad, "still reported as bad when allowed")
	assert.Equal(t, 1, allowed.Summary.RawNoRaw, "February has no raw")
}

func TestAudit_OneFailingInstrumentDoesNotHideTheOthers(t *testing.T) {
	storetest.Root(t)
	buildHours(t, "EURUSD", utc(2024, 1, 9, 0), 3)
	putRaw(t, "GBPUSD", 2024, time.January, rec(utc(2024, 1, 9, 0), true), rec(utc(2024, 1, 9, 5), true))
	require.NoError(t, BuildCanonical(context.Background(), nil, janToMar("GBPUSD"), nil))

	got, err := Audit(context.Background(), nil, auditReq("EURUSD", "GBPUSD"))

	require.ErrorIs(t, err, ErrAuditFailed)
	require.Len(t, got.Instruments, 2)
	assert.True(t, got.Instruments[0].OK)
	assert.False(t, got.Instruments[1].OK)
	assert.Equal(t, 1, got.Summary.InstrumentsFailing)
}

func TestAudit_SampleLimitsTheRawCrossCheckOnly(t *testing.T) {
	storetest.Root(t)
	for _, m := range []time.Month{time.January, time.February, time.March} {
		putRaw(t, "EURUSD", 2024, m, rec(utc(2024, m, 9, 0), true))
	}
	require.NoError(t, BuildCanonical(context.Background(), nil,
		BuildRequest{Instruments: []string{"EURUSD"}, Interval: "h1", From: utc(2024, 1, 1, 0), To: utc(2024, 4, 1, 0)}, nil))
	req := auditReq("EURUSD")
	req.To = utc(2024, 4, 1, 0)
	req.AllowMissing = true // the bars are days apart
	req.Sample = 2

	got, err := Audit(context.Background(), nil, req)

	require.NoError(t, err)
	assert.Len(t, got.Instruments[0].Months, 3, "every month is still classified")
	assert.Len(t, got.Instruments[0].Raw, 2, "two are cross-checked against raw")
	assert.Equal(t, 2, got.Sample)
}

func TestAudit_TheReportIsMachineReadable(t *testing.T) {
	storetest.Root(t)
	buildHours(t, "EURUSD", utc(2024, 1, 9, 0), 2)
	got, err := Audit(context.Background(), nil, auditReq("EURUSD"))
	require.NoError(t, err)

	data, err := json.Marshal(got)

	require.NoError(t, err)
	var back map[string]any
	require.NoError(t, json.Unmarshal(data, &back))
	assert.Equal(t, true, back["ok"])
	assert.Equal(t, "oanda", back["source"])
	assert.Equal(t, "2024-01-01T00:00:00Z", back["from"])
	inst := back["instruments"].([]any)[0].(map[string]any)
	assert.Equal(t, "EURUSD", inst["instrument"])
	assert.Equal(t, []any{}, inst["gaps"], "an empty list, never null")
	assert.Contains(t, back["summary"], "months_loaded")
}

func TestAudit_RefusesWhatItCannotRun(t *testing.T) {
	storetest.Root(t)
	reversed := auditReq("EURUSD")
	reversed.From, reversed.To = reversed.To, reversed.From
	negative := auditReq("EURUSD")
	negative.Sample = -1
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	_, noInstrument := Audit(context.Background(), nil, auditReq())
	_, errReversed := Audit(context.Background(), nil, reversed)
	_, errNegative := Audit(context.Background(), nil, negative)
	_, errCancelled := Audit(cancelled, nil, auditReq("EURUSD"))

	assert.Error(t, noInstrument)
	assert.Error(t, errReversed)
	assert.Error(t, errNegative)
	assert.ErrorIs(t, errCancelled, context.Canceled)
}

func TestAudit_ACanonicalMonthWhoseRawIsGoneCannotBeCertified(t *testing.T) {
	root := storetest.Root(t)
	buildHours(t, "EURUSD", utc(2024, 1, 9, 0), 3)
	key := oanda.PartitionKey{Symbol: "EURUSD", Interval: oanda.RawH1, Year: 2024, Month: time.January}
	rawPath, err := key.Path()
	require.NoError(t, err)
	require.NoError(t, os.Remove(filepath.Join(root, rawPath)))

	got, aerr := Audit(context.Background(), nil, auditReq("EURUSD"))

	require.ErrorIs(t, aerr, ErrAuditFailed, "strict by default: a healthy-looking month is not certified without its raw")
	assert.False(t, got.OK)
	assert.Equal(t, AuditUnverifiable, got.Instruments[0].Raw[0].State)
	assert.Equal(t, 1, got.Summary.RawUnverifiable)
	assert.Zero(t, got.Summary.RawNoRaw, "it is not the harmless no-raw of a month with no canonical file either")
	assert.Empty(t, got.Instruments[0].Gaps)
	assert.Equal(t, 3, got.Instruments[0].Bars, "the series itself loaded")
}

func TestAudit_ASampleCountsTheMonthsItCheckedAgainstRaw(t *testing.T) {
	storetest.Root(t)
	for _, m := range []time.Month{time.January, time.February, time.March} {
		putRaw(t, "EURUSD", 2024, m, rec(utc(2024, m, 9, 0), true))
	}
	require.NoError(t, BuildCanonical(context.Background(), nil,
		BuildRequest{Instruments: []string{"EURUSD"}, Interval: "h1", From: utc(2024, 1, 1, 0), To: utc(2024, 4, 1, 0)}, nil))
	req := auditReq("EURUSD")
	req.To, req.AllowMissing, req.Sample = utc(2024, 4, 1, 0), true, 1

	got, err := Audit(context.Background(), nil, req)

	require.NoError(t, err)
	assert.Equal(t, 3, got.Summary.Months)
	assert.Equal(t, 1, got.Summary.RawChecked)
}

func TestAudit_AKnownProviderHoleIsCountedAndTheAuditPasses(t *testing.T) {
	storetest.Root(t)
	// EURUSD h1 2007-01-08 23:00 to 2007-01-09 21:00 is on the reviewed list of holes
	// the provider does not have.
	putRaw(t, "EURUSD", 2007, time.January, rec(utc(2007, 1, 8, 22), true), rec(utc(2007, 1, 9, 21), true))
	require.NoError(t, BuildCanonical(context.Background(), nil,
		BuildRequest{Instruments: []string{"EURUSD"}, Interval: "h1", From: utc(2007, 1, 1, 0), To: utc(2007, 2, 1, 0)}, nil))
	req := auditReq("EURUSD")
	req.From, req.To = utc(2007, 1, 1, 0), utc(2007, 2, 1, 0)

	got, err := Audit(context.Background(), nil, req)

	require.NoError(t, err)
	assert.True(t, got.OK)
	assert.Equal(t, 1, got.Summary.GapsKnownHole)
	assert.Zero(t, got.Summary.GapsUnexplained)
	assert.Equal(t, []AuditGap{{Kind: "known_hole", Start: utc(2007, 1, 8, 23), End: utc(2007, 1, 9, 21), Bars: 22}}, got.Instruments[0].Gaps)
}
