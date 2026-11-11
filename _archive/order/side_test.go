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
	assert.True(t, Buy.Valid())
	assert.True(t, Sell.Valid())
	assert.False(t, sideUnset.Valid())
	assert.False(t, Side(200).Valid())
}

func TestSideZeroValueIsInvalid(t *testing.T) {
	var s Side
	assert.False(t, s.Valid())
}
