package order

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTypeString(t *testing.T) {
	assert.Equal(t, "market", Market.String())
	assert.Equal(t, "limit", Limit.String())
	assert.Equal(t, "stop", Stop.String())
	assert.Equal(t, "stop_limit", StopLimit.String())
	assert.Contains(t, Type(200).String(), "200")
}

func TestTypeValid(t *testing.T) {
	assert.True(t, Market.valid())
	assert.True(t, Limit.valid())
	assert.True(t, Stop.valid())
	assert.True(t, StopLimit.valid())
	assert.False(t, typeUnset.valid())
	assert.False(t, Type(200).valid())
}

func TestTypeRequiredPrices(t *testing.T) {
	cases := []struct {
		typ                 Type
		wantLimit, wantStop bool
	}{
		{Market, false, false},
		{Limit, true, false},
		{Stop, false, true},
		{StopLimit, true, true},
	}
	for _, tc := range cases {
		t.Run(tc.typ.String(), func(t *testing.T) {
			assert.Equal(t, tc.wantLimit, tc.typ.requiresLimitPrice())
			assert.Equal(t, tc.wantStop, tc.typ.requiresStopPrice())
		})
	}
}
