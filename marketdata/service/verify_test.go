package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/marketdata/internal/canonical"
	"github.com/rustyeddy/trader/store"
	"github.com/rustyeddy/trader/store/storetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// built builds p and returns it, so a test can verify it or damage the file first.
func built(t *testing.T, p marketdata.SourcePartition) marketdata.SourcePartition {
	t.Helper()
	_, err := Build(context.Background(), p, builtAt)
	require.NoError(t, err)
	return p
}

// edit reads the file built from p, lets mutate change it and writes it back,
// bypassing Build, to make a file that disagrees with its raw.
func edit(t *testing.T, p marketdata.SourcePartition, mutate func(*canonical.File)) {
	t.Helper()
	f := readBack(t, p)
	mutate(&f)
	require.NoError(t, canonical.Write(context.Background(), f))
}

func kinds(v Verification) []FindingKind {
	var out []FindingKind
	for _, f := range v.Findings {
		out = append(out, f.Kind)
	}
	return out
}

func hours(n int) []marketdata.SourceRow {
	var rows []marketdata.SourceRow
	for i := range n {
		rows = append(rows, row(i+1, utc(2024, 1, 9, i)))
	}
	return rows
}

func TestVerify_AFileBuiltFromTheRawVerifies(t *testing.T) {
	storetest.Root(t)
	p := built(t, h1Source(hours(5)...))

	got, err := Verify(context.Background(), p)

	require.NoError(t, err)
	assert.Equal(t, Verified, got.State)
	assert.Empty(t, got.Findings)
	assert.Equal(t, 5, got.RawRows)
	assert.Equal(t, 5, got.Bars)
	assert.Equal(t, "oanda/canonical/EURUSD/2024/01/EURUSD-2024-01-h1.csv", got.Path)
}

func TestVerify_EveryKindOfBadRecordIsExplainedByItsRow(t *testing.T) {
	storetest.Root(t)
	incomplete := row(2, utc(2024, 1, 2, 1))
	incomplete.Complete = false
	misaligned := row(3, time.Date(2024, 1, 2, 2, 30, 0, 0, time.UTC))
	shape := row(4, utc(2024, 1, 2, 3))
	shape.Bar.Bid.High = px("1.09000")
	crossed := row(5, utc(2024, 1, 2, 5))
	crossed.Bar.Ask.Open = px("1.09990")
	oneSided := row(6, utc(2024, 1, 2, 6))
	oneSided.Bar.Ask = marketdata.OHLC{}
	p := built(t, h1Source(row(1, utc(2024, 1, 2, 0)), incomplete, misaligned, shape, crossed, oneSided,
		row(7, utc(2024, 1, 2, 7)), row(8, utc(2024, 1, 2, 7)), row(9, utc(2024, 1, 2, 3)), row(10, utc(2024, 1, 2, 8))))

	got, err := Verify(context.Background(), p)

	require.NoError(t, err)
	assert.Equal(t, Verified, got.State, "findings: %v", got.Findings)
	assert.Equal(t, map[string]int{"incomplete": 1, "misaligned": 1, "invalid": 2, "crossed": 1, "duplicate": 1, "out-of-order": 1}, got.Explained,
		"the expected differences are counted by reason")
	assert.Equal(t, 10, got.RawRows)
	assert.Equal(t, 3, got.Bars)
	assert.Equal(t, 7, got.Bad)
}

func TestVerify_ABarThatDiffersFromItsRawRowIsAMismatch(t *testing.T) {
	// The file is valid (Write refuses one that is not), so what differs is the raw:
	// the file holds a different price or volume than the raw row says.
	for name, mutate := range map[string]struct {
		change func(*marketdata.Bar)
		want   int // mismatch findings: the mid is also not the raw midpoint when a side changes
	}{
		"bid":    {func(b *marketdata.Bar) { b.Bid.Close = px("1.10040") }, 2},
		"ask":    {func(b *marketdata.Bar) { b.Ask.High = px("1.10112") }, 2},
		"volume": {func(b *marketdata.Bar) { b.Volume++ }, 1},
	} {
		t.Run(name, func(t *testing.T) {
			storetest.Root(t)
			p := built(t, h1Source(hours(3)...))
			mutate.change(&p.Rows[1].Bar)

			got, err := Verify(context.Background(), p)

			require.NoError(t, err)
			assert.Equal(t, VerifyFailed, got.State)
			assert.Len(t, got.Findings, mutate.want)
			for _, f := range got.Findings {
				assert.Equal(t, FindMismatch, f.Kind)
				assert.Equal(t, utc(2024, 1, 9, 1), f.Time)
				assert.Equal(t, 2, f.RawRow)
			}
		})
	}
}

func TestVerify_AFileWhoseBarDisagreesWithItselfIsUnreadable(t *testing.T) {
	storetest.Root(t)
	p := built(t, h1Source(hours(3)...))
	f := readBack(t, p)
	path, err := f.Key.Path()
	require.NoError(t, err)
	data, err := store.Read(path)
	require.NoError(t, err)
	// Edit a price in the file by hand, leaving the mid as it was.
	damaged := strings.Replace(string(data), "1.10050", "1.10051", 1)
	require.NotEqual(t, string(data), damaged)
	require.NoError(t, store.Write(context.Background(), path, []byte(damaged)))

	got, verr := Verify(context.Background(), p)

	require.NoError(t, verr)
	assert.Equal(t, VerifyFailed, got.State)
	assert.Equal(t, []FindingKind{FindUnreadable}, kinds(got))
}

func TestVerify_ARowTheFileLostIsUnaccounted(t *testing.T) {
	storetest.Root(t)
	p := built(t, h1Source(hours(4)...))
	edit(t, p, func(f *canonical.File) { f.Bars = append(f.Bars[:1], f.Bars[2:]...) })

	got, err := Verify(context.Background(), p)

	require.NoError(t, err)
	assert.Equal(t, VerifyFailed, got.State)
	assert.Equal(t, []FindingKind{FindUnaccounted}, kinds(got))
	assert.Equal(t, 2, got.Findings[0].RawRow)
	assert.Equal(t, utc(2024, 1, 9, 1), got.Findings[0].Time)
}

func TestVerify_ABarWithNoRawRowIsUnexpected(t *testing.T) {
	storetest.Root(t)
	p := built(t, h1Source(hours(4)...))
	short := p
	short.Rows = p.Rows[:3] // the raw lost its last row after the build

	got, err := Verify(context.Background(), short)

	require.NoError(t, err)
	assert.Equal(t, VerifyFailed, got.State)
	assert.Equal(t, []FindingKind{FindUnexpected}, kinds(got))
	assert.Equal(t, utc(2024, 1, 9, 3), got.Findings[0].Time)
}

func TestVerify_ARawFileThatChangedAfterTheBuildIsAFingerprintAndRowFindings(t *testing.T) {
	storetest.Root(t)
	p := built(t, h1Source(hours(3)...))
	changed := p
	changed.Fingerprint = "sha256:bbb"
	changed.Rows = append(append([]marketdata.SourceRow{}, p.Rows...), row(4, utc(2024, 1, 9, 3)))

	got, err := Verify(context.Background(), changed)

	require.NoError(t, err)
	assert.Equal(t, VerifyFailed, got.State)
	assert.Equal(t, []FindingKind{FindFingerprint, FindUnaccounted}, kinds(got))
}

func TestVerify_ABadRecordThatTheRowDoesNotBearOutIsUnjustified(t *testing.T) {
	for name, reason := range map[string]canonical.Reason{
		"incomplete": canonical.ReasonIncomplete, "misaligned": canonical.ReasonMisaligned, "duplicate": canonical.ReasonDuplicate,
		"out_of_order": canonical.ReasonOutOfOrder, "crossed": canonical.ReasonCrossed, "invalid": canonical.ReasonInvalid,
	} {
		t.Run(name, func(t *testing.T) {
			storetest.Root(t)
			p := built(t, h1Source(hours(3)...))
			// Row 2 is a perfectly good, complete, aligned, ordered, valid row: move
			// it from the bars to the bad records under a reason that is not true.
			edit(t, p, func(f *canonical.File) {
				moved := f.Bars[1]
				f.Bars = append(f.Bars[:1], f.Bars[2:]...)
				f.Bad = append(f.Bad, canonical.BadRecord{Time: moved.Time, Reason: reason, RawRow: 2})
			})

			got, err := Verify(context.Background(), p)

			require.NoError(t, err)
			assert.Equal(t, VerifyFailed, got.State)
			assert.Equal(t, []FindingKind{FindUnjustified}, kinds(got))
		})
	}
}

func TestVerify_ABadRecordForARowThatDoesNotExistIsUnexpected(t *testing.T) {
	storetest.Root(t)
	p := built(t, h1Source(hours(3)...))
	edit(t, p, func(f *canonical.File) {
		f.Bad = append(f.Bad, canonical.BadRecord{Time: utc(2024, 1, 9, 20), Reason: canonical.ReasonIncomplete, RawRow: 99})
	})

	got, err := Verify(context.Background(), p)

	require.NoError(t, err)
	assert.Equal(t, []FindingKind{FindUnexpected}, kinds(got))
	assert.Equal(t, 99, got.Findings[0].RawRow)
}

func TestVerify_ABadRecordAtTheWrongTimeIsAMismatch(t *testing.T) {
	storetest.Root(t)
	incomplete := row(2, utc(2024, 1, 9, 1))
	incomplete.Complete = false
	p := built(t, h1Source(row(1, utc(2024, 1, 9, 0)), incomplete))
	edit(t, p, func(f *canonical.File) { f.Bad[0].Time = utc(2024, 1, 9, 5) })

	got, err := Verify(context.Background(), p)

	require.NoError(t, err)
	assert.Equal(t, []FindingKind{FindMismatch}, kinds(got))
}

func TestVerify_ABarMadeFromAFormingCandleIsUnjustified(t *testing.T) {
	storetest.Root(t)
	p := built(t, h1Source(hours(3)...))
	p.Rows[1].Complete = false

	got, err := Verify(context.Background(), p)

	require.NoError(t, err)
	assert.Equal(t, []FindingKind{FindUnjustified}, kinds(got))
}

func TestVerify_ASecondRowAtABarsTimeThatIsNotADuplicateIsUnaccounted(t *testing.T) {
	storetest.Root(t)
	p := built(t, h1Source(row(1, utc(2024, 1, 9, 0)), row(2, utc(2024, 1, 9, 1))))
	p.Rows = append(p.Rows, row(3, utc(2024, 1, 9, 1)))

	got, err := Verify(context.Background(), p)

	require.NoError(t, err)
	assert.Equal(t, []FindingKind{FindUnaccounted}, kinds(got))
	assert.Equal(t, 3, got.Findings[0].RawRow)
}

func TestVerify_AFileFromAnotherBuildIsStale(t *testing.T) {
	storetest.Root(t)
	p := built(t, h1Source(hours(2)...))
	edit(t, p, func(f *canonical.File) { f.Meta.BuilderVersion = "builder-v0" })

	got, err := Verify(context.Background(), p)

	require.NoError(t, err)
	assert.Equal(t, VerifyFailed, got.State)
	assert.Equal(t, []FindingKind{FindStale}, kinds(got))
}

func TestVerify_AMissingFileIsNotAnError(t *testing.T) {
	storetest.Root(t)

	got, err := Verify(context.Background(), h1Source(hours(2)...))

	require.NoError(t, err)
	assert.Equal(t, NoCanonical, got.State)
	assert.Empty(t, got.Findings)
}

func TestVerify_AFileThatIsNotCanonicalIsFailedAndUnreadable(t *testing.T) {
	storetest.Root(t)
	p := h1Source(hours(2)...)
	require.NoError(t, store.Write(context.Background(), "oanda/canonical/EURUSD/2024/01/EURUSD-2024-01-h1.csv", []byte("# schema=canonical-v1\nold\n")))

	got, err := Verify(context.Background(), p)

	require.NoError(t, err)
	assert.Equal(t, VerifyFailed, got.State)
	assert.Equal(t, []FindingKind{FindUnreadable}, kinds(got))
}

func TestVerify_ABadlyDuplicatedFileIsUnreadable(t *testing.T) {
	storetest.Root(t)
	p := built(t, h1Source(hours(3)...))
	f := readBack(t, p)
	path, err := f.Key.Path()
	require.NoError(t, err)
	data, err := store.Read(path)
	require.NoError(t, err)
	// Repeat the last data line: a duplicate time, which a valid file never has.
	lines := splitLines(string(data))
	require.NoError(t, store.Write(context.Background(), path, []byte(joinLines(append(lines, lines[len(lines)-1])))))

	got, verr := Verify(context.Background(), p)

	require.NoError(t, verr)
	assert.Equal(t, VerifyFailed, got.State)
	assert.Equal(t, []FindingKind{FindUnreadable}, kinds(got))
}

func TestVerify_FindingsAreCapped(t *testing.T) {
	storetest.Root(t)
	p := built(t, h1Source(hours(3)...))
	big := p
	big.Rows = nil
	for i := range maxFindings + 20 {
		big.Rows = append(big.Rows, row(i+1, utc(2024, 1, 10, 0).Add(time.Duration(i)*time.Hour)))
	}

	got, err := Verify(context.Background(), big)

	require.NoError(t, err)
	assert.Len(t, got.Findings, maxFindings)
	assert.Positive(t, got.Omitted)
}

func TestVerify_RefusesWhatItCannotCheck(t *testing.T) {
	storetest.Root(t)
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	badInterval := h1Source(hours(1)...)
	badInterval.Interval = "h4"
	noName := h1Source(hours(1)...)
	noName.Instrument = ""

	_, errCancelled := Verify(cancelled, h1Source(hours(1)...))
	_, errInterval := Verify(context.Background(), badInterval)
	_, errInvalid := Verify(context.Background(), noName)

	assert.ErrorIs(t, errCancelled, context.Canceled)
	assert.ErrorIs(t, errInterval, ErrUnsupportedInterval)
	assert.ErrorIs(t, errInvalid, marketdata.ErrInvalidSource)
}

func TestFinding_String(t *testing.T) {
	assert.Equal(t, "mismatch 2024-01-09T01:00Z row 2: bid differs",
		Finding{Kind: FindMismatch, Time: utc(2024, 1, 9, 1), RawRow: 2, Detail: "bid differs"}.String())
	assert.Equal(t, "fingerprint: built elsewhere", Finding{Kind: FindFingerprint, Detail: "built elsewhere"}.String())
}

func splitLines(s string) []string {
	var out []string
	for len(s) > 0 {
		i := 0
		for i < len(s) && s[i] != '\n' {
			i++
		}
		out = append(out, s[:i])
		if i < len(s) {
			i++
		}
		s = s[i:]
	}
	return out
}

func joinLines(lines []string) string {
	s := ""
	for _, l := range lines {
		s += l + "\n"
	}
	return s
}
