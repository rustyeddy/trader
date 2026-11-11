package marketdata

import (
	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

// Fixtures are local to runtime tests; public values retain their own unit tests.
const validRawFingerprint = "sha256:2c26b46b68ffc68ff99b453c1d30413413422d706483bfa0f98a5e886266e7a"

var testNewYorkLocation = func() *time.Location {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		panic(err)
	}
	return loc
}()

func gbpusd() instrument.ID {
	return instrument.CurrencyPairID(num.MustParseCurrency("GBP"), num.MustParseCurrency("USD"))
}

func validManifest(t *testing.T) marketdata.Manifest {
	t.Helper()
	start := time.Date(2020, 3, 2, 0, 0, 0, 0, time.UTC)
	end := time.Date(2020, 3, 2, 4, 0, 0, 0, time.UTC)
	span, err := marketdata.NewTimeRange(start, end)
	require.NoError(t, err)
	return marketdata.Manifest{
		Provider:         "oanda",
		Instrument:       eurusd(),
		Interval:         marketdata.H1,
		Span:             span,
		Basis:            marketdata.BasisBid,
		AdjustmentPolicy: marketdata.AdjustmentNotApplicable,
		SchemaVersion:    1,
		RawFingerprint:   validRawFingerprint,
		BuilderVersion:   "builder-v1",
		ValidatorVersion: "validator-v1",
		ResamplerVersion: "none",
		CalendarVersion:  "fxcalendar-v1",
		BuiltAt:          time.Date(2020, 3, 3, 0, 0, 0, 0, time.UTC),
		BarCount:         2,
		FirstBar:         start,
		LastBar:          start.Add(time.Hour),
	}
}

func validDerivedManifest(t *testing.T) marketdata.Manifest {
	t.Helper()
	m := validManifest(t)
	m.ResamplerVersion = "resampler-v1"
	m.Parent = &marketdata.ParentRef{Instrument: eurusd(), Interval: marketdata.M1, Revision: "parent-rev-1"}
	return m
}

func p(t *testing.T, s string) num.Price {
	t.Helper()
	price, err := num.ParsePrice(s)
	require.NoErrorf(t, err, "ParsePrice(%q)", s)
	return price
}

func validBar(t *testing.T) marketdata.Bar {
	t.Helper()
	return marketdata.Bar{
		Time:      time.Date(2020, 3, 1, 22, 0, 0, 0, time.UTC),
		Open:      p(t, "1.10000"),
		High:      p(t, "1.10250"),
		Low:       p(t, "1.09900"),
		Close:     p(t, "1.10100"),
		AvgSpread: p(t, "0.00012"),
		MaxSpread: p(t, "0.00030"),
		Ticks:     4213,
	}
}

func barAt(t *testing.T, at time.Time) marketdata.Bar {
	t.Helper()
	b := validBar(t)
	b.Time = at
	return b
}

func eurusd() instrument.ID {
	return instrument.CurrencyPairID(num.MustParseCurrency("EUR"), num.MustParseCurrency("USD"))
}

func validBarSet(t *testing.T) marketdata.BarSet {
	t.Helper()
	start := time.Date(2020, 3, 2, 0, 0, 0, 0, time.UTC)
	end := time.Date(2020, 3, 2, 4, 0, 0, 0, time.UTC)
	span, err := marketdata.NewTimeRange(start, end)
	require.NoError(t, err)
	return marketdata.BarSet{
		Instrument: eurusd(),
		Interval:   marketdata.H1,
		Span:       span,
		Basis:      marketdata.BasisBid,
		Bars: []marketdata.Bar{
			barAt(t, start),
			barAt(t, start.Add(time.Hour)),
		},
	}
}

func nyTime(year int, month time.Month, day, hour, minute int) time.Time {
	return time.Date(year, month, day, hour, minute, 0, 0, testNewYorkLocation)
}
