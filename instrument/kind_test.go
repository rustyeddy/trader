package instrument

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestKindString(t *testing.T) {
	tests := []struct {
		k    Kind
		want string
	}{
		{KindCurrencyPair, "currency pair"},
		{KindEquity, "equity"},
		{KindETF, "ETF"},
		{KindFuture, "future"},
		{KindContinuousSeries, "continuous series"},
		{KindIndex, "index"},
		{Kind(0), "unknown instrument kind"},
		{Kind(99), "unknown instrument kind"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.k.String())
		})
	}
}

func TestKindIsValid(t *testing.T) {
	valid := []Kind{KindCurrencyPair, KindEquity, KindETF, KindFuture, KindContinuousSeries, KindIndex}
	for _, k := range valid {
		assert.Truef(t, k.IsValid(), "%s should be valid", k)
	}

	var zero Kind
	assert.False(t, zero.IsValid())
	assert.False(t, Kind(99).IsValid())
}
