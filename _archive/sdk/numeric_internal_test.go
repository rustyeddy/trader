package sdk

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/num"
)

func TestParsePrice_Valid(t *testing.T) {
	p, err := parsePrice("field", "1.1000")
	require.NoError(t, err)
	require.Equal(t, "1.1", p.String())
}

func TestParsePrice_EmptyRejected(t *testing.T) {
	_, err := parsePrice("field", "")
	require.ErrorIs(t, err, ErrInvalidWireValue)
}

func TestParsePrice_MalformedRejected(t *testing.T) {
	_, err := parsePrice("field", "not-a-number")
	require.ErrorIs(t, err, ErrInvalidWireValue)
}

func TestParseOptionalPrice_EmptyIsNil(t *testing.T) {
	p, err := parseOptionalPrice("field", "")
	require.NoError(t, err)
	require.Nil(t, p)
}

func TestParseOptionalPrice_MalformedRejected(t *testing.T) {
	_, err := parseOptionalPrice("field", "not-a-number")
	require.ErrorIs(t, err, ErrInvalidWireValue)
}

func TestParseQuantity_Valid(t *testing.T) {
	q, err := parseQuantity("field", "1000")
	require.NoError(t, err)
	require.Equal(t, "1000", q.String())
}

func TestParseQuantity_EmptyRejected(t *testing.T) {
	_, err := parseQuantity("field", "")
	require.ErrorIs(t, err, ErrInvalidWireValue)
}

func TestParseQuantity_MalformedRejected(t *testing.T) {
	_, err := parseQuantity("field", "not-a-number")
	require.ErrorIs(t, err, ErrInvalidWireValue)
}

func TestPriceOrEmpty(t *testing.T) {
	require.Equal(t, "", priceOrEmpty(nil))
	p := num.MustParsePrice("1.5")
	require.Equal(t, "1.5", priceOrEmpty(&p))
}

func TestQuantityOrEmpty(t *testing.T) {
	require.Equal(t, "", quantityOrEmpty(nil))
	q := num.MustParseQuantity("10")
	require.Equal(t, "10", quantityOrEmpty(&q))
}
