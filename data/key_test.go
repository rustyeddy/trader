package data

import (
	"testing"
	"time"

	"github.com/rustyeddy/trader/types"
	"github.com/stretchr/testify/require"
)

func TestKeyCompare(t *testing.T) {
	t.Parallel()

	base := Key{
		Source:     "candles",
		Instrument: "EURUSD",
		Kind:       KindCandle,
		TF:         types.H1,
		Year:       2026,
		Month:      1,
		Day:        0,
		Hour:       0,
	}

	t.Run("equal keys return 0", func(t *testing.T) {
		t.Parallel()
		require.Equal(t, 0, base.compare(base))
	})

	t.Run("smaller source returns -1", func(t *testing.T) {
		t.Parallel()
		other := base
		other.Source = "zzz"
		require.Equal(t, -1, base.compare(other))
	})

	t.Run("larger source returns 1", func(t *testing.T) {
		t.Parallel()
		other := base
		other.Source = "aaa"
		require.Equal(t, 1, base.compare(other))
	})

	t.Run("smaller instrument returns -1", func(t *testing.T) {
		t.Parallel()
		other := base
		other.Instrument = "USDJPY"
		require.Equal(t, -1, base.compare(other))
	})

	t.Run("larger instrument returns 1", func(t *testing.T) {
		t.Parallel()
		other := base
		other.Instrument = "AUDUSD"
		require.Equal(t, 1, base.compare(other))
	})

	t.Run("smaller kind returns -1", func(t *testing.T) {
		t.Parallel()
		other := base
		other.Kind = KindTick // KindTick < KindCandle
		require.Equal(t, 1, base.compare(other))
	})

	t.Run("smaller year returns -1", func(t *testing.T) {
		t.Parallel()
		other := base
		other.Year = 2027
		require.Equal(t, -1, base.compare(other))
	})

	t.Run("larger year returns 1", func(t *testing.T) {
		t.Parallel()
		other := base
		other.Year = 2025
		require.Equal(t, 1, base.compare(other))
	})

	t.Run("smaller month returns -1", func(t *testing.T) {
		t.Parallel()
		other := base
		other.Month = 2
		require.Equal(t, -1, base.compare(other))
	})

	t.Run("larger month returns 1", func(t *testing.T) {
		t.Parallel()
		other := base
		other.Month = 0
		require.Equal(t, 1, base.compare(other))
	})

	t.Run("smaller day returns -1", func(t *testing.T) {
		t.Parallel()
		other := base
		other.Day = 1
		require.Equal(t, -1, base.compare(other))
	})

	t.Run("larger day returns 1", func(t *testing.T) {
		t.Parallel()
		withDay := base
		withDay.Day = 5
		other := base
		other.Day = 3
		require.Equal(t, 1, withDay.compare(other))
	})

	t.Run("smaller hour returns -1", func(t *testing.T) {
		t.Parallel()
		withHour := base
		withHour.Hour = 10
		other := base
		other.Hour = 12
		require.Equal(t, -1, withHour.compare(other))
	})
}

func TestKeyBeforeAfter(t *testing.T) {
	t.Parallel()

	a := Key{Source: "candles", Instrument: "EURUSD", Kind: KindCandle, TF: types.H1, Year: 2025, Month: 1}
	b := Key{Source: "candles", Instrument: "EURUSD", Kind: KindCandle, TF: types.H1, Year: 2026, Month: 1}

	require.True(t, a.before(b))
	require.False(t, a.after(b))
	require.True(t, b.after(a))
	require.False(t, b.before(a))
	require.False(t, a.before(a))
	require.False(t, a.after(a))
}

func TestKeyTime(t *testing.T) {
	t.Parallel()

	t.Run("full date", func(t *testing.T) {
		t.Parallel()
		k := Key{Year: 2024, Month: 5, Day: 7, Hour: 13}
		got := k.Time()
		want := time.Date(2024, 5, 7, 13, 0, 0, 0, time.UTC)
		require.Equal(t, want, got)
	})

	t.Run("zero year defaults to 1970", func(t *testing.T) {
		t.Parallel()
		k := Key{Year: 0, Month: 3, Day: 1, Hour: 0}
		got := k.Time()
		require.Equal(t, 1970, got.Year())
	})

	t.Run("zero month defaults to 1", func(t *testing.T) {
		t.Parallel()
		k := Key{Year: 2024, Month: 0, Day: 1, Hour: 0}
		got := k.Time()
		require.Equal(t, time.January, got.Month())
	})

	t.Run("invalid month defaults to 1", func(t *testing.T) {
		t.Parallel()
		k := Key{Year: 2024, Month: 13, Day: 1, Hour: 0}
		got := k.Time()
		require.Equal(t, time.January, got.Month())
	})

	t.Run("zero day defaults to 1", func(t *testing.T) {
		t.Parallel()
		k := Key{Year: 2024, Month: 6, Day: 0, Hour: 5}
		got := k.Time()
		require.Equal(t, 1, got.Day())
	})

	t.Run("invalid day defaults to 1", func(t *testing.T) {
		t.Parallel()
		k := Key{Year: 2024, Month: 6, Day: 32, Hour: 0}
		got := k.Time()
		require.Equal(t, 1, got.Day())
	})

	t.Run("hour out of range defaults to 0", func(t *testing.T) {
		t.Parallel()
		k := Key{Year: 2024, Month: 6, Day: 1, Hour: 25}
		got := k.Time()
		require.Equal(t, 0, got.Hour())
	})
}

func TestKeyIsMonthlyCandle(t *testing.T) {
	t.Parallel()

	require.True(t, Key{Kind: KindCandle, Day: 0, Hour: 0}.IsMonthlyCandle())
	require.False(t, Key{Kind: KindTick, Day: 0, Hour: 0}.IsMonthlyCandle())
	require.False(t, Key{Kind: KindCandle, Day: 1, Hour: 0}.IsMonthlyCandle())
	require.False(t, Key{Kind: KindCandle, Day: 0, Hour: 1}.IsMonthlyCandle())
}

func TestKeyIsHourlyTick(t *testing.T) {
	t.Parallel()

	require.True(t, Key{Kind: KindTick, Day: 1, Hour: 0}.IsHourlyTick())
	require.True(t, Key{Kind: KindTick, Day: 5, Hour: 13}.IsHourlyTick())
	require.False(t, Key{Kind: KindCandle, Day: 1, Hour: 0}.IsHourlyTick())
	require.False(t, Key{Kind: KindTick, Day: 0, Hour: 5}.IsHourlyTick())
}

func TestKeyRange(t *testing.T) {
	t.Parallel()

	t.Run("hourly tick range spans one hour", func(t *testing.T) {
		t.Parallel()
		k := Key{
			Kind:  KindTick,
			Year:  2026,
			Month: 3,
			Day:   15,
			Hour:  10,
		}
		rng := k.Range()
		start := time.Date(2026, 3, 15, 10, 0, 0, 0, time.UTC)
		require.Equal(t, types.Timestamp(start.Unix()), rng.Start)
		require.Equal(t, types.Timestamp(start.Add(time.Hour).Unix()), rng.End)
		require.Equal(t, types.Ticks, rng.TF)
	})

	t.Run("monthly candle range spans one month", func(t *testing.T) {
		t.Parallel()
		k := Key{
			Kind:  KindCandle,
			TF:    types.H1,
			Year:  2026,
			Month: 3,
			Day:   0,
			Hour:  0,
		}
		rng := k.Range()
		start := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
		end := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
		require.Equal(t, types.Timestamp(start.Unix()), rng.Start)
		require.Equal(t, types.Timestamp(end.Unix()), rng.End)
		require.Equal(t, types.H1, rng.TF)
	})

	t.Run("unsupported key returns empty range", func(t *testing.T) {
		t.Parallel()
		k := Key{Kind: KindCandle, Day: 1, Hour: 1}
		rng := k.Range()
		require.Equal(t, types.TimeRange{}, rng)
	})
}

func TestRequiredTickHoursForMonth(t *testing.T) {
	t.Parallel()

	keys := RequiredTickHoursForMonth("dukascopy", "EURUSD", 2026, 3)
	require.NotEmpty(t, keys)

	for _, k := range keys {
		require.Equal(t, "dukascopy", k.Source)
		require.Equal(t, "EURUSD", k.Instrument)
		require.Equal(t, KindTick, k.Kind)
		require.Equal(t, 2026, k.Year)
		require.Equal(t, 3, k.Month)
		require.GreaterOrEqual(t, k.Day, 1)
		require.LessOrEqual(t, k.Day, 31)
		require.GreaterOrEqual(t, k.Hour, 0)
		require.Less(t, k.Hour, 24)
	}
}
