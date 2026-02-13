package pricing

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

var cs *CandleSet

func init() {
	var err error

	fname := "../testdata/DAT_ASCII_EURUSD_M1_2025.csv"
	cs, err = NewCandleSet(fname)
	if err != nil {
		panic(err)
	}
}

func TestIterator(t *testing.T) {

	expected := Candle{O: 1035030, H: 1035140, L: 1035030, C: 1035140}
	it := cs.Iterator()
	it.Next()
	assert.Equal(t, int64(1735768800), it.StartTime())

	ca := it.Candle()
	assert.Equal(t, expected, ca)

	i := 0
	for it.Next() {
		i++
	}

	assert.Equal(t, i, 372023)
}

func TestReadCandleSetFile(t *testing.T) {
	s := cs.Stats()
	assert.Equal(t, s.TotalMinutes, 524158)
	assert.Equal(t, s.PresentMinutes, 372024)
	assert.Equal(t, s.MissingMinutes, 152134)
	assert.Equal(t, s.GapCount, 965)
	assert.Equal(t, s.WeekendGaps, 52)
	assert.Equal(t, 15, s.SuspiciousGaps)
}
