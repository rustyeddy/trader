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

// TestLoadPreservesExactDecimalFromUnquotedYAML is the regression test for
// the bug where fileValues used to unmarshal YAML into map[string]any: an
// unquoted numeric scalar took a detour through float64 on the way to the
// field decoder. float64 cannot exactly represent num.Price's own maximum —
// 19 significant digits against float64's ~15-17 — so that detour was a
// silent precision bug for exactly the exact-decimal types this package
// exists to support. The value is deliberately unquoted here: a quoted
// string never entered map[string]any as a number in the first place, so it
// would not have caught the bug.
func TestLoadPreservesExactDecimalFromUnquotedYAML(t *testing.T) {
	type Config struct {
		MaxPrice num.Price
	}

	got, err := Load[Config](Options{
		Environ:     []string{},
		FileContent: []byte("maxprice: 92233720368.54775807\n"),
	})
	require.NoError(t, err)
	assert.Equal(t, "92233720368.54775807", got.MaxPrice.String())
}

// TestLoadPreservesSmallDecimalFromUnquotedYAML guards the other failure
// shape of the same bug: fmt.Sprint on a small float64 such as 0.00000001
// produces exponent notation ("1e-08"), which num's exact parser rejects
// outright. Routing through float64 turned valid YAML into a load failure.
func TestLoadPreservesSmallDecimalFromUnquotedYAML(t *testing.T) {
	type Config struct {
		Rate num.Rate
	}

	got, err := Load[Config](Options{
		Environ:     []string{},
		FileContent: []byte("rate: 0.00000001\n"),
	})
	require.NoError(t, err)
	assert.Equal(t, "0.00000001", got.Rate.String())
}
