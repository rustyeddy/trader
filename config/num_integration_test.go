package config

import (
	"strings"
	"testing"

	"github.com/rustyeddy/trader/num"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type numConfig struct {
	MaxPrice num.Price
	Currency num.Currency
}

// TestLoadAndRenderNumTypes is the concrete proof for the package doc
// comment's claim that num.Price/num.Quantity/num.Rate/num.Money/num.Currency
// work as config field types with no special-casing: they satisfy
// encoding.TextUnmarshaler and encoding.TextMarshaler like any other
// caller-defined type, so decode.go and render.go already handle them.
func TestLoadAndRenderNumTypes(t *testing.T) {
	got, err := Load[numConfig](Options{
		Environ:     []string{},
		FileContent: []byte("maxprice: \"123.45\"\ncurrency: USD\n"),
	})
	require.NoError(t, err)
	assert.Equal(t, "123.45", got.MaxPrice.String())
	assert.Equal(t, "USD", got.Currency.String())

	var sb strings.Builder
	require.NoError(t, Render(&sb, got))
	assert.Contains(t, sb.String(), "maxprice = 123.45")
	assert.Contains(t, sb.String(), "currency = USD")
}
