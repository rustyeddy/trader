package num

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMinMax_Price(t *testing.T) {
	a, b, c := MustParsePrice("1.5"), MustParsePrice("0.25"), MustParsePrice("3")

	assert.Equal(t, b, Min(a, b, c))
	assert.Equal(t, c, Max(a, b, c))
	assert.Equal(t, a, Min(a), "one value is its own extreme")
	assert.Equal(t, a, Max(a))
	assert.Equal(t, Price{}, Min(Price{}, a), "zero is valid and smallest")
}

func TestMinMax_Quantity(t *testing.T) {
	a, b := MustParseQuantity("10"), MustParseQuantity("2")

	assert.Equal(t, b, Min(a, b))
	assert.Equal(t, a, Max(b, a))
}

func TestMinMax_RateIncludingNegativeAndExtremes(t *testing.T) {
	neg, zero, pos := MustParseRate("-0.5"), Rate{}, MustParseRate("0.5")
	lowest, highest := Rate{raw: math.MinInt64}, Rate{raw: math.MaxInt64}

	assert.Equal(t, neg, Min(zero, neg, pos))
	assert.Equal(t, pos, Max(zero, neg, pos))
	// An ordering computed by subtraction would overflow here.
	assert.Equal(t, lowest, Min(highest, lowest, zero))
	assert.Equal(t, highest, Max(lowest, highest, zero))
}

// Equal values are indistinguishable by value, so only the choice among values
// that compare equal but differ in representation could show which is returned.
// Every num type has one representation per value, so check the contract with
// a test type.
type orderedInt int

func (a orderedInt) Cmp(b orderedInt) int { return int(a) - int(b) }

type tagged struct{ order, tag int }

func (a tagged) Cmp(b tagged) int { return a.order - b.order }

func TestMinMax_EarliestWinsATie(t *testing.T) {
	first, second := tagged{order: 1, tag: 1}, tagged{order: 1, tag: 2}

	assert.Equal(t, first, Min(first, second))
	assert.Equal(t, first, Max(first, second))
	assert.Equal(t, orderedInt(3), Max[orderedInt](1, 3, 2))
}
