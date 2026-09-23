package marketdata_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/internal/clock"
	marketruntime "github.com/rustyeddy/trader/internal/marketdata"
	svc "github.com/rustyeddy/trader/internal/service/marketdata"
	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
)

func TestConvertImportsAndBuildsStooqArchive(t *testing.T) {
	resolver := instrument.NewMemoryResolver()
	id, err := svc.RegisterETFInstrument(resolver, svc.EquityRegistration{
		Provider: "stooq", Exchange: "ARCA", Ticker: "SPY", Currency: num.MustParseCurrency("USD"),
	})
	require.NoError(t, err)
	rawRoot, storeRoot := t.TempDir(), t.TempDir()
	manager, err := marketruntime.New(marketruntime.Config{
		Clock: clock.NewSimulated(time.Date(2020, 3, 1, 0, 0, 0, 0, time.UTC)), StoreRoot: storeRoot,
		RawRoot: rawRoot, Resolver: resolver, ProviderName: "stooq",
		Calendar: marketdata.NewUSEquityCalendar(marketdata.StandardUSEquityHolidays(2020)),
	})
	require.NoError(t, err)
	service, err := svc.New(manager, nil)
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "spy.us.txt")
	require.NoError(t, os.WriteFile(path, []byte(
		"<TICKER>,<PER>,<DATE>,<TIME>,<OPEN>,<HIGH>,<LOW>,<CLOSE>,<VOL>,<OPENINT>\n"+
			"SPY.US,D,20200131,000000,100,101,99,100.5,1000,0\n"), 0o644))
	span, err := marketdata.NewTimeRange(time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2020, 2, 1, 0, 0, 0, 0, time.UTC))
	require.NoError(t, err)

	resp, err := service.Convert(context.Background(), svc.ConvertRequest{
		DatasetRequest: svc.DatasetRequest{Instrument: id, Interval: marketdata.D1, Range: span}, ArchivePath: path,
	})
	require.NoError(t, err)
	require.Equal(t, 1, resp.Import.RowsImported)
	require.Len(t, resp.Build.Result.Published, 1)
}

func TestConvertRejectsNonDailyIntervalBeforeImport(t *testing.T) {
	service := newTestService(t)
	span, err := marketdata.NewTimeRange(time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2020, 2, 1, 0, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	_, err = service.Convert(context.Background(), svc.ConvertRequest{
		DatasetRequest: svc.DatasetRequest{Instrument: instrument.ETFID("ARCA", "SPY"), Interval: marketdata.H1, Range: span}, ArchivePath: "unused",
	})
	require.ErrorIs(t, err, svc.ErrInvalidRequest)
	require.ErrorContains(t, err, "supports only D1")
}
