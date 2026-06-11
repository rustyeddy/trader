package trader

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func parseEST(s string) (time.Time, error) {
	t, err := time.ParseInLocation(layout, s, estNoDST)
	if err != nil {
		return time.Time{}, err
	}
	return t.UTC(), nil
}

func secondsToTFString(sec Timestamp) (string, error) {
	if sec <= 0 {
		return "", fmt.Errorf("invalid timeframe seconds: %d", sec)
	}
	if sec < 3600 && sec%60 == 0 {
		return fmt.Sprintf("M%d", sec/60), nil
	}
	if sec < 86400 && sec%3600 == 0 {
		return fmt.Sprintf("H%d", sec/3600), nil
	}
	if sec%86400 == 0 {
		days := sec / 86400
		if days == 7 {
			return "W1", nil
		}
		if days == 30 {
			return "MN1", nil
		}
		return fmt.Sprintf("D%d", days), nil
	}

	return "", fmt.Errorf("cannot map timeframe: %d seconds", sec)
}

func tfStringToSeconds(tf string) (Timestamp, error) {
	switch tf {
	case "M1":
		return 60, nil
	case "M5":
		return 300, nil
	case "M15":
		return 900, nil
	case "M30":
		return 1800, nil
	case "H1":
		return 3600, nil
	case "H4":
		return 14400, nil
	case "D1":
		return 86400, nil
	case "W1":
		return 604800, nil
	case "MN1":
		return 2592000, nil
	default:
		return 0, fmt.Errorf("unsupported timeframe string: %s", tf)
	}
}

// TestParseToUnix verifies parseToUnix handles supported formats and invalid input.
func TestParseToUnix(t *testing.T) {
	t.Parallel()

	got, err := parseToUnix("20240115 000000")
	require.NoError(t, err)
	assert.Equal(t, Timestamp(time.Date(2024, 1, 15, 5, 0, 0, 0, time.UTC).Unix()), got)

	got, err = parseToUnix(" 2024-01-15T00:00:00Z ")
	require.NoError(t, err)
	assert.Equal(t, Timestamp(time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC).Unix()), got)

	got, err = parseToUnix("2024-01-15T00:00:00Z")
	require.NoError(t, err)
	assert.Equal(t, Timestamp(time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC).Unix()), got)

	_, err = parseToUnix("2024-01-15T00:00:30Z")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "timestamp not minute-aligned")

	_, err = parseToUnix("bad-date")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse timestamp")
}

// TestParseEST verifies parseEST parses EST timestamps and rejects bad values.
func TestParseEST(t *testing.T) {
	t.Parallel()

	got, err := parseEST("20240115 000000")
	require.NoError(t, err)
	assert.Equal(t, time.Date(2024, 1, 15, 5, 0, 0, 0, time.UTC), got)

	_, err = parseEST("bad")
	require.Error(t, err)
}

// TestFastPrice verifies fastPrice parses dotted and undotted price strings.
func TestFastPrice(t *testing.T) {
	t.Parallel()

	got, err := fastPrice("1.035030")
	require.NoError(t, err)
	assert.Equal(t, Price(1035030), got)

	got, err = fastPrice(" 1.035030 ")
	require.NoError(t, err)
	assert.Equal(t, Price(1035030), got)

	got, err = fastPrice("1035030")
	require.NoError(t, err)
	assert.Equal(t, Price(1035030), got)

	_, err = fastPrice("not-a-price")
	require.Error(t, err)

	_, err = fastPrice("1.2.3")
	require.Error(t, err)
}

// TestBitHelpers verifies bitSet and bitIsSet operate on multiword bitsets.
func TestBitHelpers(t *testing.T) {
	t.Parallel()

	bits := make([]uint64, 2)
	assert.False(t, bitIsSet(bits, 3))
	bitSet(bits, 3)
	assert.True(t, bitIsSet(bits, 3))
	assert.False(t, bitIsSet(bits, 64))
	bitSet(bits, 64)
	assert.True(t, bitIsSet(bits, 64))
}

// TestSecondsToTFStringAndBack verifies timeframe conversions are reversible for supported values.
func TestSecondsToTFStringAndBack(t *testing.T) {
	t.Parallel()

	cases := []struct {
		sec     Timestamp
		want    string
		wantErr bool
	}{
		{60, "M1", false},
		{300, "M5", false},
		{900, "M15", false},
		{1800, "M30", false},
		{3600, "H1", false},
		{14400, "H4", false},
		{86400, "D1", false},
		{604800, "W1", false},
		{2592000, "MN1", false},
		{0, "", true},
		{59, "", true},
	}

	for _, tt := range cases {
		got, err := secondsToTFString(tt.sec)
		if tt.wantErr {
			require.Error(t, err)
			continue
		}
		require.NoError(t, err)
		assert.Equal(t, tt.want, got)
	}

	for _, tf := range []string{"M1", "M5", "M15", "M30", "H1", "H4", "D1", "W1", "MN1"} {
		sec, err := tfStringToSeconds(tf)
		require.NoError(t, err)
		got, err := secondsToTFString(sec)
		require.NoError(t, err)
		assert.Equal(t, tf, got)
	}

	_, err := tfStringToSeconds("bad")
	require.Error(t, err)
}
