package strategies

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSignalString(t *testing.T) {
	assert.Equal(t, "BUY", Buy.String())
	assert.Equal(t, "SELL", Sell.String())
	assert.Equal(t, "HOLD", Hold.String())
	// Default case for an unknown signal value
	assert.Equal(t, "HOLD", Signal(99).String())
}

func TestDefaultDecision(t *testing.T) {
	d := DefaultDecision{}
	assert.Equal(t, Hold, d.Signal())
	// "jbc" is the placeholder reason returned by DefaultDecision.Reason() in strategy.go.
	assert.Equal(t, "jbc", d.Reason())
}
