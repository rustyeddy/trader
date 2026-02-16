package market

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
	assert.Equal(t, 524158, s.TotalMinutes)
	assert.Equal(t, 372024, s.PresentMinutes)
	assert.Equal(t, 152134, s.MissingMinutes)
	assert.Equal(t, 965, s.GapCount)
	assert.Equal(t, 52, s.WeekendGaps)
	assert.Equal(t, 15, s.SuspiciousGaps)
}

func TestAggregateH1(t *testing.T) {
	h1 := cs.AggregateH1(50)
	h1.BuildGapReport()
	s := h1.Stats()

	assert.Equal(t, 8736, s.TotalMinutes)
	assert.Equal(t, 6212, s.PresentMinutes)
	assert.Equal(t, 2524, s.MissingMinutes)
	assert.Equal(t, 54, s.GapCount)
	assert.Equal(t, 52, s.WeekendGaps)
	assert.Equal(t, 2, s.SuspiciousGaps)
	assert.Equal(t, 49, s.LongestGap)

	// fmt.Printf("H1: %+v\n", h1)
}
