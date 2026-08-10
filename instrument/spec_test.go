package instrument

import (
	"testing"

	"github.com/rustyeddy/trader/num"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func validFXSpec(t *testing.T) Spec {
	t.Helper()
	s, err := NewSpec(
		num.MustParsePrice("0.00001"),
		num.MustParseQuantity("1"),
		num.MustParseRate("1"),
		num.MustParseCurrency("USD"),
	)
	require.NoError(t, err)
	return s
}

func TestNewSpec(t *testing.T) {
	s := validFXSpec(t)

	assert.Equal(t, "0.00001", s.TickSize().String())
	assert.Equal(t, "1", s.QuantityIncrement().String())
	assert.Equal(t, "1", s.Multiplier().String())
	assert.Equal(t, "USD", s.SettlementCurrency().String())
}

func TestNewSpecRejectsInvalidFields(t *testing.T) {
	tickSize := num.MustParsePrice("0.01")
	qtyIncrement := num.MustParseQuantity("1")
	multiplier := num.MustParseRate("1")
	currency := num.MustParseCurrency("USD")

	tests := []struct {
		name              string
		tickSize          num.Price
		quantityIncrement num.Quantity
		multiplier        num.Rate
		currency          num.Currency
	}{
		{"zero tick size", num.Price{}, qtyIncrement, multiplier, currency},
		{"zero quantity increment", tickSize, num.Quantity{}, multiplier, currency},
		{"zero multiplier", tickSize, qtyIncrement, num.Rate{}, currency},
		{"negative multiplier", tickSize, qtyIncrement, num.MustParseRate("-1"), currency},
		{"invalid currency", tickSize, qtyIncrement, multiplier, num.Currency{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewSpec(tt.tickSize, tt.quantityIncrement, tt.multiplier, tt.currency)
			require.ErrorIs(t, err, ErrInvalidSpec)
		})
	}
}

func TestSpecValidatePrice(t *testing.T) {
	s := validFXSpec(t)

	require.NoError(t, s.ValidatePrice(num.MustParsePrice("1.08450")))
	err := s.ValidatePrice(num.MustParsePrice("1.084503"))
	require.ErrorIs(t, err, ErrInvalidSpec)
}

func TestSpecValidateQuantity(t *testing.T) {
	tickSize := num.MustParsePrice("0.01")
	s, err := NewSpec(tickSize, num.MustParseQuantity("5"), num.MustParseRate("1"), num.MustParseCurrency("USD"))
	require.NoError(t, err)

	require.NoError(t, s.ValidateQuantity(num.MustParseQuantity("100")))
	err = s.ValidateQuantity(num.MustParseQuantity("101"))
	require.ErrorIs(t, err, ErrInvalidSpec)
}

func TestSpecZeroValueIsNotConstructed(t *testing.T) {
	var s Spec
	assert.True(t, s.TickSize().IsZero())
}
