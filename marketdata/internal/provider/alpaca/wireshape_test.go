package alpaca

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBarsResponse_Records_ReanchorsToMidnightUTCOfTradingDate(t *testing.T) {
	resp := barsResponse{
		Bars: []wireBar{
			// 2024-01-02T05:00:00Z is 2024-01-02 00:00:00 in
			// America/New_York (EST, UTC-5) — same calendar date.
			{Time: "2024-01-02T05:00:00Z", Open: "100.00", High: "101.00", Low: "99.00", Close: "100.50", Volume: 1000},
			// 2024-07-01T04:00:00Z is 2024-07-01 00:00:00 in
			// America/New_York (EDT, UTC-4) — exercises DST.
			{Time: "2024-07-01T04:00:00Z", Open: "200.00", High: "201.00", Low: "199.00", Close: "200.50", Volume: 2000},
			// 2024-01-02T23:30:00Z is still 2024-01-02 18:30 EST — same
			// trading date despite being late in the UTC day.
			{Time: "2024-01-02T23:30:00Z", Open: "300.00", High: "301.00", Low: "299.00", Close: "300.50", Volume: 3000},
		},
	}

	records, err := resp.records()
	require.NoError(t, err)
	require.Len(t, records, 3)

	assert.True(t, records[0].Time.Equal(time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)))
	assert.True(t, records[1].Time.Equal(time.Date(2024, 7, 1, 0, 0, 0, 0, time.UTC)))
	assert.True(t, records[2].Time.Equal(time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)))

	assert.Equal(t, "100", records[0].Open.String())
	assert.Equal(t, "101", records[0].High.String())
	assert.Equal(t, "99", records[0].Low.String())
	assert.Equal(t, "100.5", records[0].Close.String())
	assert.Equal(t, int64(1000), records[0].Volume)
}

func TestBarsResponse_Records_EmptyBarsIsEmptySlice(t *testing.T) {
	records, err := barsResponse{}.records()
	require.NoError(t, err)
	assert.Empty(t, records)
}

func TestBarsResponse_Records_RejectsMalformedTime(t *testing.T) {
	resp := barsResponse{Bars: []wireBar{{Time: "not-a-time", Open: "1", High: "1", Low: "1", Close: "1"}}}
	_, err := resp.records()
	assert.ErrorIs(t, err, ErrBadRequest)
}

func TestBarsResponse_Records_RejectsMalformedPrice(t *testing.T) {
	resp := barsResponse{Bars: []wireBar{{Time: "2024-01-02T05:00:00Z", Open: "not-a-number", High: "1", Low: "1", Close: "1"}}}
	_, err := resp.records()
	assert.ErrorIs(t, err, ErrBadRequest)
}
