package order

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSideString(t *testing.T) {
	assert.Equal(t, "buy", Buy.String())
	assert.Equal(t, "sell", Sell.String())
	assert.Contains(t, Side(200).String(), "200")
}

func TestSideValid(t *testing.T) {
	assert.True(t, Buy.valid())
	assert.True(t, Sell.valid())
	assert.False(t, sideUnset.valid())
	assert.False(t, Side(200).valid())
}

func TestSideZeroValueIsInvalid(t *testing.T) {
	var s Side
	assert.False(t, s.valid())
}
