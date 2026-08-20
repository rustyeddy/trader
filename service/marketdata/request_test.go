package marketdata_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
	svc "github.com/rustyeddy/trader/service/marketdata"
)

func mustEURUSD(t *testing.T) instrument.ID {
	t.Helper()
	eur := num.MustParseCurrency("EUR")
	usd := num.MustParseCurrency("USD")
	inst, err := instrument.NewCurrencyPair(eur, usd)
	require.NoError(t, err)
	return inst.ID()
}

func mustRange(t *testing.T) marketdata.TimeRange {
	t.Helper()
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	r, err := marketdata.NewTimeRange(start, end)
	require.NoError(t, err)
	return r
}

func TestDatasetRequestValidate_Valid(t *testing.T) {
	req := svc.DatasetRequest{
		Instrument: mustEURUSD(t),
		Interval:   marketdata.D1,
		Range:      mustRange(t),
	}
	require.NoError(t, req.Validate())
}

func TestDatasetRequestValidate_ZeroInstrument(t *testing.T) {
	req := svc.DatasetRequest{
		Interval: marketdata.D1,
		Range:    mustRange(t),
	}
	err := req.Validate()
	require.ErrorIs(t, err, svc.ErrInvalidRequest)
}

func TestDatasetRequestValidate_InvalidInterval(t *testing.T) {
	req := svc.DatasetRequest{
		Instrument: mustEURUSD(t),
		Range:      mustRange(t),
	}
	err := req.Validate()
	require.ErrorIs(t, err, svc.ErrInvalidRequest)
}

func TestDatasetRequestValidate_ZeroRange(t *testing.T) {
	req := svc.DatasetRequest{
		Instrument: mustEURUSD(t),
		Interval:   marketdata.D1,
	}
	err := req.Validate()
	require.ErrorIs(t, err, svc.ErrInvalidRequest)
}
