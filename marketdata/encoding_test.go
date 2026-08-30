package marketdata

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIntervalJSONRoundTrip(t *testing.T) {
	for _, iv := range []Interval{M1, H1, H4, D1, W1} {
		data, err := json.Marshal(iv)
		require.NoError(t, err, iv.String())

		var got Interval
		require.NoError(t, json.Unmarshal(data, &got), iv.String())
		assert.Equal(t, iv, got, iv.String())
	}
}

func TestIntervalJSONShape(t *testing.T) {
	data, err := json.Marshal(H4)
	require.NoError(t, err)
	assert.JSONEq(t, `{"unit":"hour","count":4}`, string(data))
}

func TestIntervalUnmarshalJSONRejectsUnknownUnit(t *testing.T) {
	var iv Interval
	err := json.Unmarshal([]byte(`{"unit":"fortnight","count":1}`), &iv)
	require.ErrorIs(t, err, ErrInvalidIntervalJSON)
}

func TestIntervalUnmarshalJSONRejectsNonPositiveCount(t *testing.T) {
	var iv Interval
	err := json.Unmarshal([]byte(`{"unit":"hour","count":0}`), &iv)
	require.ErrorIs(t, err, ErrInvalidIntervalJSON)
}

func TestIntervalUnmarshalJSONRejectsMalformed(t *testing.T) {
	var iv Interval
	// A JSON number is syntactically valid at the top level (so
	// encoding/json actually dispatches to Interval's own
	// UnmarshalJSON), but does not decode into intervalWire's object
	// shape — unlike a syntactically invalid top-level document, which
	// encoding/json rejects before ever calling UnmarshalJSON.
	err := json.Unmarshal([]byte(`123`), &iv)
	require.ErrorIs(t, err, ErrInvalidIntervalJSON)
}

func TestIntervalStringIsNotTheWireFormat(t *testing.T) {
	// H4.String() is "H4" — a reader must not be able to feed that
	// display string into UnmarshalJSON and get a valid Interval back;
	// String is documented as display-only, never a parser contract.
	var iv Interval
	err := json.Unmarshal([]byte(`"H4"`), &iv)
	require.Error(t, err)
}

func TestTimeRangeJSONRoundTrip(t *testing.T) {
	start := time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, time.January, 2, 0, 0, 0, 0, time.UTC)
	span, err := NewTimeRange(start, end)
	require.NoError(t, err)

	data, err := json.Marshal(span)
	require.NoError(t, err)

	var got TimeRange
	require.NoError(t, json.Unmarshal(data, &got))
	assert.True(t, got.Start().Equal(start))
	assert.True(t, got.End().Equal(end))
}

func TestTimeRangeJSONDeterministic(t *testing.T) {
	start := time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, time.January, 2, 0, 0, 0, 0, time.UTC)
	span, err := NewTimeRange(start, end)
	require.NoError(t, err)

	a, err := json.Marshal(span)
	require.NoError(t, err)
	b, err := json.Marshal(span)
	require.NoError(t, err)
	assert.Equal(t, a, b)
}

func TestTimeRangeUnmarshalJSONRejectsEndNotAfterStart(t *testing.T) {
	start := time.Date(2024, time.January, 2, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC)
	data, err := json.Marshal(timeRangeWire{Start: start, End: end})
	require.NoError(t, err)

	var got TimeRange
	err = json.Unmarshal(data, &got)
	require.Error(t, err)
}

func TestTimeRangeUnmarshalJSONRejectsMalformed(t *testing.T) {
	var r TimeRange
	err := json.Unmarshal([]byte(`not json`), &r)
	require.Error(t, err)
}

func TestIntervalMarshalJSONRejectsInvalidInterval(t *testing.T) {
	var iv Interval // zero value: UnitMinute, count 0 — invalid
	_, err := json.Marshal(iv)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrInvalidIntervalJSON)
}
