package id

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTextRoundTrip(t *testing.T) {
	want := MustParseRunID(genValidRunID(t, 7))

	text, err := want.MarshalText()
	require.NoError(t, err)
	assert.Equal(t, want.String(), string(text))

	var got RunID
	require.NoError(t, got.UnmarshalText(text))
	assert.True(t, want.Equal(got))
}

func TestMarshalTextRejectsZeroValue(t *testing.T) {
	var zero RunID
	_, err := zero.MarshalText()
	require.ErrorIs(t, err, ErrZeroValue)
}

func TestUnmarshalTextRejectsInvalidInput(t *testing.T) {
	var r RunID
	err := r.UnmarshalText([]byte("not-a-valid-id"))
	require.ErrorIs(t, err, ErrInvalidID)
}

func TestUnmarshalTextRejectsWrongKindPrefix(t *testing.T) {
	orderStr := genValidID(t, "ord", 1)
	var r RunID
	err := r.UnmarshalText([]byte(orderStr))
	require.ErrorIs(t, err, ErrInvalidID)
}

func TestJSONRoundTrip(t *testing.T) {
	want := MustParseOrderID(genValidID(t, "ord", 9))

	data, err := json.Marshal(want)
	require.NoError(t, err)
	assert.JSONEq(t, `"`+want.String()+`"`, string(data))

	var got OrderID
	require.NoError(t, json.Unmarshal(data, &got))
	assert.True(t, want.Equal(got))
}

func TestMarshalJSONRejectsZeroValue(t *testing.T) {
	var zero OrderID
	_, err := json.Marshal(zero)
	require.Error(t, err)
}

func TestUnmarshalJSONRejectsMalformedInput(t *testing.T) {
	var r RunID
	err := json.Unmarshal([]byte(`"not-a-valid-id"`), &r)
	require.ErrorIs(t, err, ErrInvalidID)
}

func TestUnmarshalJSONRejectsNonStringValue(t *testing.T) {
	var r RunID
	err := json.Unmarshal([]byte(`12345`), &r)
	require.Error(t, err)
}

// TestNoRawBytesInJSON pins ADR-style expectations shared with num/config:
// every field marshals as a string, never as a raw number or byte array
// that would expose the [16]byte representation.
func TestNoRawBytesInJSON(t *testing.T) {
	want := MustParseRunID(genValidRunID(t, 3))

	data, err := json.Marshal(want)
	require.NoError(t, err)

	var raw any
	require.NoError(t, json.Unmarshal(data, &raw))
	_, isString := raw.(string)
	assert.True(t, isString, "marshaled ID must be a JSON string, got %T", raw)
}

func TestJSONFieldRoundTripInStruct(t *testing.T) {
	type wrapper struct {
		ID OrderID `json:"id"`
	}

	want := wrapper{ID: MustParseOrderID(genValidID(t, "ord", 4))}

	data, err := json.Marshal(want)
	require.NoError(t, err)

	var got wrapper
	require.NoError(t, json.Unmarshal(data, &got))
	assert.True(t, want.ID.Equal(got.ID))
}
