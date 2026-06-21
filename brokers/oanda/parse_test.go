package oanda

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseFloatField_Success(t *testing.T) {
	v, err := parseFloatField("price", "1.08500")
	require.NoError(t, err)
	assert.InDelta(t, 1.085, v, 1e-9)
}

func TestParseFloatField_InvalidReturnsError(t *testing.T) {
	_, err := parseFloatField("price", "not-a-number")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse price")
}

func TestParseOptionalFloatField_EmptyStringReturnsZero(t *testing.T) {
	v, err := parseOptionalFloatField("balance", "")
	require.NoError(t, err)
	assert.Equal(t, 0.0, v)
}

func TestParseOptionalFloatField_ValidStringParsed(t *testing.T) {
	v, err := parseOptionalFloatField("balance", "10000.50")
	require.NoError(t, err)
	assert.InDelta(t, 10000.50, v, 1e-9)
}

func TestParseOptionalFloatField_InvalidReturnsError(t *testing.T) {
	_, err := parseOptionalFloatField("balance", "bad")
	require.Error(t, err)
}

func TestParseIntField_Success(t *testing.T) {
	v, err := parseIntField("units", "-50000")
	require.NoError(t, err)
	assert.Equal(t, int64(-50000), v)
}

func TestParseIntField_InvalidReturnsError(t *testing.T) {
	_, err := parseIntField("units", "3.14")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse units")
}

func TestParseTimeField_ValidRFC3339Nano(t *testing.T) {
	raw := "2024-06-15T10:30:00.000000000Z"
	ts, err := parseTimeField("time", raw)
	require.NoError(t, err)
	assert.Equal(t, 2024, ts.Year())
	assert.Equal(t, time.June, ts.Month())
	assert.Equal(t, 15, ts.Day())
}

func TestParseTimeField_InvalidReturnsError(t *testing.T) {
	_, err := parseTimeField("time", "not-a-time")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse time")
}
