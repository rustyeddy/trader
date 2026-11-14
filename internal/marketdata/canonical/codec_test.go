package canonical

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func encode(t *testing.T, f File) string {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, Encode(context.Background(), &buf, f))
	return buf.String()
}

func TestEncode_WritesTheDocumentedShape(t *testing.T) {
	f := goodFile(t)
	f.Bad = []BadRecord{{Time: utc(2024, 1, 2, 9), Reason: ReasonCrossed, RawRow: 4}}

	got := encode(t, f)

	lines := strings.Split(strings.TrimSuffix(got, "\n"), "\n")
	require.Len(t, lines, 7)
	assert.Equal(t, "# schema=canonical-v2 provider=oanda symbol=EURUSD interval=h1 year=2024 month=01", lines[0])
	assert.Equal(t, `# meta {"raw_fingerprint":"sha256:abc","builder_version":"builder-v1","validator_version":"validator-v1","availability_policy":"scheduled-close-v1","built_at":"2026-11-14T12:00:00Z","bar_count":3,"bad_count":1,"first_bar":"2024-01-02T00:00:00Z","last_bar":"2024-01-02T02:00:00Z"}`, lines[1])
	assert.Equal(t, "# bad time=2024-01-02T09:00:00Z reason=crossed raw_row=4", lines[2])
	assert.Equal(t, "time,available_at,bid_o,bid_h,bid_l,bid_c,ask_o,ask_h,ask_l,ask_c,volume", lines[3])
	assert.Equal(t, "2024-01-02T00:00:00Z,2024-01-02T01:00:00Z,1.10000,1.10100,1.09900,1.10050,1.10010,1.10111,1.09911,1.10061,100", lines[4])
}

func TestEncode_RoundTripsGoodAndBadRecords(t *testing.T) {
	f := goodFile(t)
	f.Bad = []BadRecord{
		{Time: utc(2024, 1, 2, 9), Reason: ReasonCrossed, RawRow: 4},
		{Time: utc(2024, 1, 2, 9).Add(30 * time.Second), Reason: ReasonMisaligned, RawRow: 5},
		{Time: utc(2024, 1, 2, 1), Reason: ReasonDuplicate, RawRow: 9},
	}

	got, err := Decode(strings.NewReader(encode(t, f)))

	require.NoError(t, err)
	assert.Equal(t, f.Key, got.Key)
	assert.True(t, f.Meta.SameBuild(got.Meta))
	assert.True(t, f.Meta.BuiltAt.Equal(got.Meta.BuiltAt))
	require.Len(t, got.Bars, 3)
	for i := range f.Bars {
		assert.True(t, f.Bars[i].Time.Equal(got.Bars[i].Time))
		assert.True(t, f.Bars[i].AvailableAt.Equal(got.Bars[i].AvailableAt))
		assert.Equal(t, f.Bars[i].Bid, got.Bars[i].Bid)
		assert.Equal(t, f.Bars[i].Ask, got.Bars[i].Ask)
		assert.Equal(t, f.Bars[i].Volume, got.Bars[i].Volume)
	}
	require.Len(t, got.Bad, 3)
	for i := range f.Bad {
		assert.True(t, f.Bad[i].Time.Equal(got.Bad[i].Time))
		assert.Equal(t, f.Bad[i].Reason, got.Bad[i].Reason)
		assert.Equal(t, f.Bad[i].RawRow, got.Bad[i].RawRow)
	}
	assert.False(t, got.Clean())
}

func TestEncode_AFileWithOnlyBadRecordsRoundTrips(t *testing.T) {
	f := File{Key: jan24H1(), Meta: testMeta(), Bad: []BadRecord{{Time: utc(2024, 1, 2, 0), Reason: ReasonIncomplete, RawRow: 1}}}

	got, err := Decode(strings.NewReader(encode(t, f)))

	require.NoError(t, err)
	assert.Empty(t, got.Bars)
	assert.Len(t, got.Bad, 1)
}

func TestEncode_IsDeterministic(t *testing.T) {
	assert.Equal(t, encode(t, goodFile(t)), encode(t, goodFile(t)))
}

func TestEncode_RefusesWhatIsNotValidOrNotRepresentable(t *testing.T) {
	bad := goodFile(t)
	bad.Bars[0].Volume = -1
	assert.ErrorIs(t, Encode(context.Background(), &bytes.Buffer{}, bad), ErrMalformedData)

	// A price with a sixth decimal is an error, never rounded.
	f := goodFile(t)
	f.Bars[0].Bid.Open = px("1.100005")
	f.Bars[0].Bid.Low = px("1.09900")
	err := Encode(context.Background(), &bytes.Buffer{}, f)
	assert.ErrorIs(t, err, ErrMalformedData)
	assert.Contains(t, err.Error(), "decimals")
}

func TestEncode_StopsWhenCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := Encode(ctx, &bytes.Buffer{}, goodFile(t))

	assert.ErrorIs(t, err, context.Canceled)
}

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("disk full") }

func TestEncode_ReportsAWriteFailure(t *testing.T) {
	err := Encode(context.Background(), failWriter{}, goodFile(t))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "disk full")
}

func TestDecode_RejectsMalformedInput(t *testing.T) {
	good := encode(t, goodFile(t))
	lines := strings.Split(strings.TrimSuffix(good, "\n"), "\n")
	join := func(l []string) string { return strings.Join(l, "\n") + "\n" }
	with := func(i int, v string) string { c := append([]string(nil), lines...); c[i] = v; return join(c) }
	without := func(i int) string { c := append([]string(nil), lines...); return join(append(c[:i], c[i+1:]...)) }
	row := lines[4]

	for name, in := range map[string]string{
		"empty":                   "",
		"not a comment":           with(0, "schema=canonical-v2"),
		"v1 schema":               with(0, "# schema=canonical-v1 provider=oanda symbol=EURUSD interval=h1 year=2024 month=01"),
		"missing header field":    with(0, "# schema=canonical-v2 provider=oanda symbol=EURUSD interval=h1 year=2024"),
		"repeated header field":   with(0, "# schema=canonical-v2 provider=oanda provider=oanda symbol=EURUSD interval=h1 year=2024 month=01"),
		"header field no equals":  with(0, "# schema=canonical-v2 provider=oanda symbol=EURUSD interval=h1 year=2024 month=01 junk"),
		"non-numeric year":        with(0, "# schema=canonical-v2 provider=oanda symbol=EURUSD interval=h1 year=20x4 month=01"),
		"no meta":                 without(1),
		"two metas":               join(append(append(append([]string(nil), lines[:2]...), lines[1]), lines[2:]...)),
		"meta not json":           with(1, "# meta {nope"),
		"meta unknown field":      with(1, strings.Replace(lines[1], `"bar_count":3`, `"bar_count":3,"surprise":1`, 1)),
		"meta bad built_at":       with(1, strings.Replace(lines[1], "2026-11-14T12:00:00Z", "yesterday", 1)),
		"meta wrong bar count":    with(1, strings.Replace(lines[1], `"bar_count":3`, `"bar_count":4`, 1)),
		"meta wrong bad count":    with(1, strings.Replace(lines[1], `"bad_count":0`, `"bad_count":2`, 1)),
		"meta wrong first bar":    with(1, strings.Replace(lines[1], `"first_bar":"2024-01-02T00:00:00Z"`, `"first_bar":"2024-01-02T01:00:00Z"`, 1)),
		"unknown comment":         with(2, "# note hello"),
		"bad line missing field":  with(2, "# bad time=2024-01-02T09:00:00Z reason=crossed"),
		"bad line bad time":       with(2, "# bad time=soon reason=crossed raw_row=1"),
		"bad line bad row":        with(2, "# bad time=2024-01-02T09:00:00Z reason=crossed raw_row=x"),
		"bad line unknown reason": with(2, "# bad time=2024-01-02T09:00:00Z reason=odd raw_row=1"),
		"bad line repeated field": with(2, "# bad time=2024-01-02T09:00:00Z time=2024-01-02T09:00:00Z raw_row=1"),
		"no column header":        join(lines[:2]),
		"wrong columns":           with(2, "time,open,high,low,close"),
		"short row":               with(3, "2024-01-02T00:00:00Z,2024-01-02T01:00:00Z,1.10000"),
		"time not utc":            with(3, strings.Replace(row, "2024-01-02T00:00:00Z", "2024-01-02T00:00:00+00:00", 1)),
		"time unparsable":         with(3, strings.Replace(row, "2024-01-02T00:00:00Z", "2024-13-02T00:00:00Z", 1)),
		"available_at unparsable": with(3, strings.Replace(row, "2024-01-02T01:00:00Z", "later", 1)),
		"price four decimals":     with(3, strings.Replace(row, "1.10000", "1.1000", 1)),
		"price six decimals":      with(3, strings.Replace(row, "1.10000", "1.100000", 1)),
		"price with a sign":       with(3, strings.Replace(row, "1.10000", "+1.1000", 1)),
		"price not a number":      with(3, strings.Replace(row, "1.10000", "abcdefg", 1)),
		"volume not an integer":   with(3, strings.Replace(row, ",100", ",1.5", 1)),
		"volume negative":         with(3, strings.Replace(row, ",100", ",-1", 1)),
		"crossed row":             with(3, strings.Replace(row, "1.10010,1.10111,1.09911,1.10061", "1.09010,1.10111,1.09911,1.10061", 1)),
		"rows out of order":       join(append(append([]string(nil), lines[:3]...), lines[5], lines[4], lines[3])),
		"quote left open":         with(3, `"2024-01-02T00:00:00Z,x`),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Decode(strings.NewReader(in))

			require.Error(t, err)
			if name != "empty" {
				assert.ErrorIs(t, err, ErrMalformedData)
			}
		})
	}
}

func TestDecode_AcceptsAMissingFinalNewlineAndCRLF(t *testing.T) {
	good := encode(t, goodFile(t))

	_, err := Decode(strings.NewReader(strings.TrimSuffix(good, "\n")))
	assert.NoError(t, err)
	_, err = Decode(strings.NewReader(strings.ReplaceAll(good, "\n", "\r\n")))
	assert.NoError(t, err)
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("read failed") }

func TestDecode_ReportsAReadFailure(t *testing.T) {
	_, err := Decode(errReader{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "read failed")
}

func TestParsePriceAndFormatPrice(t *testing.T) {
	s, err := formatPrice(px("1.1"))
	require.NoError(t, err)
	assert.Equal(t, "1.10000", s)
	s, err = formatPrice(px("107"))
	require.NoError(t, err)
	assert.Equal(t, "107.00000", s)
	p, err := parsePrice("107.27200")
	require.NoError(t, err)
	assert.Equal(t, px("107.272"), p)
}
