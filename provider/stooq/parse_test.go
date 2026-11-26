package stooq

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
)

func sum(s string) string {
	h := sha256.Sum256([]byte(s))
	return "sha256:" + hex.EncodeToString(h[:])
}

func TestParse_MapsRowsToMonthlyPartitionsExactly(t *testing.T) {
	data := head + line("20240228") + line("20240229") + line("20240301")

	got, err := Parse("SPY", []byte(data))

	require.NoError(t, err)
	require.Len(t, got, 2)
	feb := got[0]
	assert.Equal(t, []any{"stooq", "SPY", "d1", 2024, time.February}, []any{feb.Source, feb.Instrument, feb.Interval, feb.Year, feb.Month})
	assert.Equal(t, sum(line("20240228")+line("20240229")), feb.Fingerprint, "of the month's exact bytes")
	assert.Equal(t, sum(line("20240301")), got[1].Fingerprint)
	assert.Equal(t, marketdata.SourceRow{Row: 3, Complete: true, Bar: marketdata.Bar{
		Instrument: "SPY", Time: time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC), Volume: 100,
		Trade: marketdata.OHLC{Open: num.MustParsePrice("10"), High: num.MustParsePrice("11.5"),
			Low: num.MustParsePrice("9.25"), Close: num.MustParsePrice("10.1234")},
	}}, got[1].Rows[0])
	for _, p := range got {
		require.NoError(t, p.Validate())
	}
}

func TestParse_AcceptsLFAndAMissingFinalNewline(t *testing.T) {
	data := strings.TrimSuffix(strings.ReplaceAll(head+line("20240301")+line("20240304"), "\r\n", "\n"), "\n")

	got, err := Parse("SPY", []byte(data))

	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Len(t, got[0].Rows, 2)
}

func TestParse_KeepsFileOrderAndDuplicatesForTheServiceToJudge(t *testing.T) {
	data := head + line("20240304") + line("20240201") + line("20240301") + line("20240304")

	got, err := Parse("SPY", []byte(data))

	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, 2, got[0].Rows[0].Row, "February, in month order")
	var rows []int
	for _, r := range got[1].Rows {
		rows = append(rows, r.Row)
	}
	assert.Equal(t, []int{1, 3, 4}, rows)
}

func TestParse_PassesUnrepresentableValuesOnWithoutPrices(t *testing.T) {
	for name, row := range map[string]string{
		"nine decimals":     "SPY.US,D,20240301,000000,10.123456789,11,9,10,100,0",
		"price too large":   "SPY.US,D,20240301,000000,10,99999999999999,9,10,100,0",
		"negative price":    "SPY.US,D,20240301,000000,10,11,-9,10,100,0",
		"fractional volume": "SPY.US,D,20240301,000000,10,11,9,10,100.5,0",
		"volume too large":  "SPY.US,D,20240301,000000,10,11,9,10,99999999999999999999,0",
	} {
		t.Run(name, func(t *testing.T) {
			got, err := Parse("SPY", []byte(head+row+"\r\n"))

			require.NoError(t, err)
			r := got[0].Rows[0]
			assert.True(t, r.Unrepresentable)
			assert.True(t, r.Bar.Trade.IsZero(), "no rounded or partial price")
			assert.Zero(t, r.Bar.Volume)
		})
	}
}

func TestParse_AcceptsAWholeVolumeWrittenWithAZeroFraction(t *testing.T) {
	got, err := Parse("SPY", []byte(head+"SPY.US,D,20240301,000000,10,11,9,10,100.00,0\n"))

	require.NoError(t, err)
	assert.False(t, got[0].Rows[0].Unrepresentable)
	assert.EqualValues(t, 100, got[0].Rows[0].Bar.Volume)
}

func TestParse_RejectsWhatItCannotRead(t *testing.T) {
	row := func(fields string) string { return head + fields + "\r\n" }
	for name, tc := range map[string]struct {
		data string
		want error
	}{
		"empty":              {"", ErrMalformedData},
		"header only":        {head, ErrMalformedData},
		"loose download":     {"Date,Open,High,Low,Close,Volume\n2024-03-01,1,1,1,1,1\n", ErrMalformedData},
		"raw-v1 partition":   {"# schema=raw-v1 source=stooq instrument=SPY tf=d1 year=2024 month=03\n", ErrMalformedData},
		"field count":        {row("SPY.US,D,20240301,000000,10,11,9,10,100"), ErrMalformedData},
		"blank line":         {head + line("20240301") + "\r\n" + line("20240304"), ErrMalformedData},
		"bad date":           {row("SPY.US,D,20240230,000000,10,11,9,10,100,0"), ErrMalformedData},
		"dashed date":        {row("SPY.US,D,2024-03-01,000000,10,11,9,10,100,0"), ErrMalformedData},
		"price text":         {row("SPY.US,D,20240301,000000,ten,11,9,10,100,0"), ErrMalformedData},
		"empty price":        {row("SPY.US,D,20240301,000000,,11,9,10,100,0"), ErrMalformedData},
		"volume text":        {row("SPY.US,D,20240301,000000,10,11,9,10,many,0"), ErrMalformedData},
		"volume exponent":    {row("SPY.US,D,20240301,000000,10,11,9,10,1e3,0"), ErrMalformedData},
		"volume bare point":  {row("SPY.US,D,20240301,000000,10,11,9,10,100.,0"), ErrMalformedData},
		"another ticker":     {row("QQQ.US,D,20240301,000000,10,11,9,10,100,0"), ErrUnsupportedSeries},
		"hourly":             {row("SPY.US,60,20240301,000000,10,11,9,10,100,0"), ErrUnsupportedSeries},
		"intraday time":      {row("SPY.US,D,20240301,150000,10,11,9,10,100,0"), ErrUnsupportedSeries},
		"open interest":      {row("SPY.US,D,20240301,000000,10,11,9,10,100,7"), ErrUnsupportedSeries},
		"bad row after good": {head + line("20240301") + "junk\r\n", ErrMalformedData},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := Parse("SPY", []byte(tc.data))

			assert.ErrorIs(t, err, tc.want)
			assert.Nil(t, got)
		})
	}
	_, err := Parse("", []byte(head+line("20240301")))
	assert.ErrorIs(t, err, ErrUnsupportedSeries)
}
