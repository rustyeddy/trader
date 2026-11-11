package marketdata_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	svc "github.com/rustyeddy/trader/internal/service/marketdata"
	"github.com/rustyeddy/trader/marketdata"
)

func TestParseInterval(t *testing.T) {
	for name, want := range map[string]marketdata.Interval{
		"M1": marketdata.M1, "h1": marketdata.H1, " H4 ": marketdata.H4, "d1": marketdata.D1, "W1": marketdata.W1,
	} {
		got, err := svc.ParseInterval(name)
		require.NoError(t, err, name)
		assert.Equal(t, want, got, name)
	}
	_, err := svc.ParseInterval("H99")
	require.ErrorIs(t, err, svc.ErrInvalidInterval)
	assert.EqualError(t, err, `invalid interval "H99": expected one of M1, H1, H4, D1, W1`)
}

func TestParseDate(t *testing.T) {
	got, err := svc.ParseDate("2024-01-07")
	require.NoError(t, err)
	assert.Equal(t, time.Date(2024, 1, 7, 0, 0, 0, 0, time.UTC), got)

	got, err = svc.ParseDate("2024-01-07T17:00:00-05:00")
	require.NoError(t, err)
	assert.Equal(t, time.Date(2024, 1, 7, 22, 0, 0, 0, time.UTC), got)
	assert.Equal(t, time.UTC, got.Location())

	_, err = svc.ParseDate("not-a-date")
	require.ErrorIs(t, err, svc.ErrInvalidDate)
	assert.EqualError(t, err, `invalid date "not-a-date": expected YYYY-MM-DD or RFC3339`)
}

func TestParseRange(t *testing.T) {
	rng, err := svc.ParseRange("", "")
	require.NoError(t, err)
	assert.True(t, rng.Start().IsZero(), "both empty: the operation's default")

	rng, err = svc.ParseRange("2024-01-01", "2024-02-01")
	require.NoError(t, err)
	assert.Equal(t, time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), rng.Start())
	assert.Equal(t, time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC), rng.End())

	_, err = svc.ParseRange("2024-01-01", "")
	assert.ErrorIs(t, err, svc.ErrInvalidRequest)
	_, err = svc.ParseRange("", "2024-01-01")
	assert.ErrorIs(t, err, svc.ErrInvalidRequest)
	_, err = svc.ParseRange("2024-02-01", "2024-01-01")
	assert.ErrorIs(t, err, svc.ErrInvalidRequest, "end before start")
	_, err = svc.ParseRange("bad", "2024-01-01")
	assert.ErrorIs(t, err, svc.ErrInvalidDate)
	_, err = svc.ParseRange("2024-01-01", "bad")
	assert.ErrorIs(t, err, svc.ErrInvalidDate)
}
