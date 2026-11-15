package marketdata

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seriesBar is a valid bar of EURUSD opening at hour h of 2024-01-02.
func seriesBar(t *testing.T, h int) Bar {
	t.Helper()
	b := goodBar(t)
	b.Instrument = "EURUSD"
	b.Time = utc(2024, 1, 2, h)
	b.AvailableAt = b.Time.Add(time.Hour)
	return b
}

func goodSeries(t *testing.T) Series {
	t.Helper()
	return Series{Source: "oanda", Instrument: "EURUSD", Interval: "h1",
		Bars: []Bar{seriesBar(t, 0), seriesBar(t, 1), seriesBar(t, 3)}}
}

func TestSeries_Validate(t *testing.T) {
	for name, tc := range map[string]struct {
		mutate func(*Series)
		ok     bool
	}{
		"valid, with a gap":    {func(*Series) {}, true},
		"empty":                {func(s *Series) { s.Bars = nil }, true},
		"no source":            {func(s *Series) { s.Source = "" }, false},
		"no instrument":        {func(s *Series) { s.Instrument = "" }, false},
		"no interval":          {func(s *Series) { s.Interval = "" }, false},
		"bar of another":       {func(s *Series) { s.Bars[1].Instrument = "GBPUSD" }, false},
		"bar not named":        {func(s *Series) { s.Bars[1].Instrument = "" }, false},
		"invalid bar":          {func(s *Series) { s.Bars[2].Volume = -1 }, false},
		"bar without avail":    {func(s *Series) { s.Bars[0].AvailableAt = time.Time{} }, false},
		"time repeated":        {func(s *Series) { s.Bars[1] = s.Bars[0] }, false},
		"time descending":      {func(s *Series) { s.Bars[0], s.Bars[1] = s.Bars[1], s.Bars[0] }, false},
		"availability equal":   {func(s *Series) { s.Bars[1].AvailableAt = s.Bars[0].AvailableAt }, false},
		"availability earlier": {func(s *Series) { s.Bars[2].AvailableAt = s.Bars[1].AvailableAt.Add(-time.Minute) }, false},
	} {
		t.Run(name, func(t *testing.T) {
			s := goodSeries(t)
			tc.mutate(&s)

			err := s.Validate()

			if tc.ok {
				assert.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, ErrInvalidSeries)
		})
	}
}
