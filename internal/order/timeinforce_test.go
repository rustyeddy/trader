package order

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTimeInForceString(t *testing.T) {
	assert.Equal(t, "gtc", GTC.String())
	assert.Equal(t, "day", DAY.String())
	assert.Equal(t, "ioc", IOC.String())
	assert.Equal(t, "fok", FOK.String())
	assert.Contains(t, TimeInForce(200).String(), "200")
}

func TestTimeInForceValid(t *testing.T) {
	assert.True(t, GTC.valid())
	assert.True(t, DAY.valid())
	assert.True(t, IOC.valid())
	assert.True(t, FOK.valid())
	assert.False(t, tifUnset.valid())
	assert.False(t, TimeInForce(200).valid())
}
