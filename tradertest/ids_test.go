package tradertest_test

import (
	"testing"

	"github.com/rustyeddy/trader/tradertest"
	"github.com/stretchr/testify/assert"
)

func TestMustAccountIDIsDeterministicAndDistinct(t *testing.T) {
	g := testGenerator()
	a := tradertest.MustAccountID(g)
	b := tradertest.MustAccountID(g)

	assert.False(t, a.IsZero())
	assert.False(t, b.IsZero())
	assert.NotEqual(t, a, b)
}

func TestMustOrderIDIsDeterministicAndDistinct(t *testing.T) {
	g := testGenerator()
	a := tradertest.MustOrderID(g)
	b := tradertest.MustOrderID(g)

	assert.False(t, a.IsZero())
	assert.NotEqual(t, a, b)
}

func TestMustFillIDIsDeterministicAndDistinct(t *testing.T) {
	g := testGenerator()
	a := tradertest.MustFillID(g)
	b := tradertest.MustFillID(g)

	assert.False(t, a.IsZero())
	assert.NotEqual(t, a, b)
}
