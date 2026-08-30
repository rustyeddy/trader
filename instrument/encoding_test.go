package instrument_test

import (
	"encoding/json"
	"testing"

	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/num"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIDJSONRoundTrip(t *testing.T) {
	id := instrument.CurrencyPairID(num.MustParseCurrency("EUR"), num.MustParseCurrency("USD"))
	data, err := json.Marshal(id)
	require.NoError(t, err)
	assert.Equal(t, `"fx:EUR/USD"`, string(data))

	var got instrument.ID
	require.NoError(t, json.Unmarshal(data, &got))
	assert.True(t, id.Equal(got))
}

func TestIDJSONZeroValueRoundTrip(t *testing.T) {
	var id instrument.ID
	data, err := json.Marshal(id)
	require.NoError(t, err)
	assert.Equal(t, `""`, string(data))

	var got instrument.ID
	require.NoError(t, json.Unmarshal(data, &got))
	assert.True(t, got.IsZero())
}

func TestIDUnmarshalJSONRejectsNonString(t *testing.T) {
	var id instrument.ID
	err := json.Unmarshal([]byte(`123`), &id)
	require.ErrorIs(t, err, instrument.ErrInvalidID)
}

func TestIDMarshalJSONDeterministic(t *testing.T) {
	id := instrument.CurrencyPairID(num.MustParseCurrency("GBP"), num.MustParseCurrency("USD"))
	a, err := json.Marshal(id)
	require.NoError(t, err)
	b, err := json.Marshal(id)
	require.NoError(t, err)
	assert.Equal(t, a, b)
}
