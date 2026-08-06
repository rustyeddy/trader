package num

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPriceTextRoundTrip(t *testing.T) {
	want := MustParsePrice("123.45")

	text, err := want.MarshalText()
	require.NoError(t, err)
	assert.Equal(t, "123.45", string(text))

	var got Price
	require.NoError(t, got.UnmarshalText(text))
	assert.True(t, want.Equal(got))
}

func TestPriceTextUnmarshalAcceptsEquivalentForm(t *testing.T) {
	var got Price
	require.NoError(t, got.UnmarshalText([]byte("123.45000000")))
	assert.Equal(t, "123.45", got.String())
}

func TestPriceTextUnmarshalRejectsNegative(t *testing.T) {
	var got Price
	err := got.UnmarshalText([]byte("-1"))
	require.ErrorIs(t, err, ErrNegative)
}

func TestPriceJSONRoundTrip(t *testing.T) {
	want := MustParsePrice("1.08473")

	data, err := json.Marshal(want)
	require.NoError(t, err)
	assert.JSONEq(t, `"1.08473"`, string(data))

	var got Price
	require.NoError(t, json.Unmarshal(data, &got))
	assert.True(t, want.Equal(got))
}

func TestPriceJSONRejectsNumber(t *testing.T) {
	var got Price
	err := json.Unmarshal([]byte(`1.08473`), &got)
	require.Error(t, err, "Price must not accept a bare JSON number")
}

func TestQuantityTextAndJSONRoundTrip(t *testing.T) {
	want := MustParseQuantity("1000.5")

	text, err := want.MarshalText()
	require.NoError(t, err)
	assert.Equal(t, "1000.5", string(text))

	var gotText Quantity
	require.NoError(t, gotText.UnmarshalText(text))
	assert.True(t, want.Equal(gotText))

	data, err := json.Marshal(want)
	require.NoError(t, err)
	assert.JSONEq(t, `"1000.5"`, string(data))

	var gotJSON Quantity
	require.NoError(t, json.Unmarshal(data, &gotJSON))
	assert.True(t, want.Equal(gotJSON))
}

func TestQuantityUnmarshalRejectsNegative(t *testing.T) {
	var got Quantity
	err := got.UnmarshalText([]byte("-1"))
	require.ErrorIs(t, err, ErrNegative)
}

func TestRateTextAndJSONRoundTrip(t *testing.T) {
	want := MustParseRate("-0.005")

	text, err := want.MarshalText()
	require.NoError(t, err)
	assert.Equal(t, "-0.005", string(text))

	var gotText Rate
	require.NoError(t, gotText.UnmarshalText(text))
	assert.True(t, want.Equal(gotText))

	data, err := json.Marshal(want)
	require.NoError(t, err)
	assert.JSONEq(t, `"-0.005"`, string(data))

	var gotJSON Rate
	require.NoError(t, json.Unmarshal(data, &gotJSON))
	assert.True(t, want.Equal(gotJSON))
}

func TestCurrencyTextAndJSONRoundTrip(t *testing.T) {
	want := MustParseCurrency("USD")

	text, err := want.MarshalText()
	require.NoError(t, err)
	assert.Equal(t, "USD", string(text))

	var gotText Currency
	require.NoError(t, gotText.UnmarshalText(text))
	assert.True(t, want.Equal(gotText))

	data, err := json.Marshal(want)
	require.NoError(t, err)
	assert.JSONEq(t, `"USD"`, string(data))

	var gotJSON Currency
	require.NoError(t, json.Unmarshal(data, &gotJSON))
	assert.True(t, want.Equal(gotJSON))
}

func TestCurrencyUnmarshalRejectsMalformed(t *testing.T) {
	var got Currency
	err := got.UnmarshalText([]byte("usd"))
	require.ErrorIs(t, err, ErrInvalidCurrency)
}

func TestMoneyTextRoundTrip(t *testing.T) {
	want := usd("123.45")

	text, err := want.MarshalText()
	require.NoError(t, err)
	assert.Equal(t, "123.45 USD", string(text))

	var got Money
	require.NoError(t, got.UnmarshalText(text))
	assert.True(t, want.Equal(got))
}

func TestMoneyTextRoundTripNegative(t *testing.T) {
	want := usd("-15.00")

	text, err := want.MarshalText()
	require.NoError(t, err)
	assert.Equal(t, "-15 USD", string(text))

	var got Money
	require.NoError(t, got.UnmarshalText(text))
	assert.True(t, want.Equal(got))
}

func TestMoneyTextMarshalRejectsInvalid(t *testing.T) {
	var m Money
	_, err := m.MarshalText()
	require.ErrorIs(t, err, ErrMissingCurrency)
}

func TestMoneyTextUnmarshalRejectsMalformed(t *testing.T) {
	tests := []struct {
		name string
		in   string
	}{
		{name: "missing currency", in: "123.45"},
		{name: "too many fields", in: "123.45 USD extra"},
		{name: "invalid currency", in: "123.45 usd"},
		{name: "invalid amount", in: "abc USD"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var m Money
			err := m.UnmarshalText([]byte(tt.in))
			require.Error(t, err)
		})
	}
}

func TestMoneyJSONRoundTrip(t *testing.T) {
	want := usd("123.45")

	data, err := json.Marshal(want)
	require.NoError(t, err)
	assert.JSONEq(t, `{"amount":"123.45","currency":"USD"}`, string(data))

	var got Money
	require.NoError(t, json.Unmarshal(data, &got))
	assert.True(t, want.Equal(got))
}

func TestMoneyJSONRoundTripNegativeAndZero(t *testing.T) {
	for _, want := range []Money{usd("-15.00"), usd("0")} {
		data, err := json.Marshal(want)
		require.NoError(t, err)

		var got Money
		require.NoError(t, json.Unmarshal(data, &got))
		assert.True(t, want.Equal(got))
	}
}

func TestMoneyJSONMarshalRejectsInvalid(t *testing.T) {
	var m Money
	_, err := json.Marshal(m)
	require.Error(t, err)
}

func TestMoneyJSONUnmarshalRejectsMissingCurrency(t *testing.T) {
	var m Money
	err := json.Unmarshal([]byte(`{"amount":"123.45"}`), &m)
	require.ErrorIs(t, err, ErrInvalidCurrency)
}

func TestMoneyJSONUnmarshalRejectsCrossFieldGarbage(t *testing.T) {
	var m Money
	err := json.Unmarshal([]byte(`{"amount":"abc","currency":"USD"}`), &m)
	require.ErrorIs(t, err, ErrSyntax)
}

// TestNoRawScaledIntegersInJSON pins ADR-004's rule that raw scaled integers
// are never exposed in ordinary public JSON: every field must be textual.
func TestNoRawScaledIntegersInJSON(t *testing.T) {
	p := MustParsePrice("1.08473")
	data, err := json.Marshal(p)
	require.NoError(t, err)
	assert.Equal(t, `"1.08473"`, string(data))

	m := usd("123.45")
	data, err = json.Marshal(m)
	require.NoError(t, err)

	var raw map[string]any
	require.NoError(t, json.Unmarshal(data, &raw))
	for k, v := range raw {
		_, isString := v.(string)
		assert.Truef(t, isString, "field %q is not a string: %v", k, v)
	}
}
