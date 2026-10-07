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

// incrementalFixture is a stooq service over temp roots plus a helper that
// writes a native archive holding the given daily rows.
type incrementalFixture struct {
	service *svc.Service
	id      instrument.ID
	dir     string
}

func newIncrementalFixture(t *testing.T) incrementalFixture {
	t.Helper()
	resolver := instrument.NewMemoryResolver()
	id, err := svc.RegisterETFInstrument(resolver, svc.EquityRegistration{
		Provider: "stooq", Exchange: "ARCA", Ticker: "SPY", Currency: num.MustParseCurrency("USD"),
	})
	require.NoError(t, err)
	manager, err := marketruntime.New(marketruntime.Config{
		Clock: clock.NewSimulated(time.Date(2020, 3, 1, 0, 0, 0, 0, time.UTC)), StoreRoot: t.TempDir(),
		RawRoot: t.TempDir(), Resolver: resolver, ProviderName: "stooq",
		Calendar: marketdata.NewUSEquityCalendar(marketdata.StandardUSEquityHolidays(2020)),
	})
	require.NoError(t, err)
	service, err := svc.New(manager, nil)
	require.NoError(t, err)
	return incrementalFixture{service: service, id: id, dir: t.TempDir()}
}

// archive writes rows as (date, close) pairs and returns the file path.
func (f incrementalFixture) archive(t *testing.T, name string, rows ...[2]string) string {
	t.Helper()
	content := "<TICKER>,<PER>,<DATE>,<TIME>,<OPEN>,<HIGH>,<LOW>,<CLOSE>,<VOL>,<OPENINT>\n"
	for _, r := range rows {
		content += "SPY.US,D," + r[0] + ",000000," + r[1] + "," + r[1] + "," + r[1] + "," + r[1] + ",1000,0\n"
	}
	path := filepath.Join(f.dir, name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	return path
}

func (f incrementalFixture) convert(t *testing.T, path string, force bool) (svc.ConvertResponse, error) {
	t.Helper()
	return f.service.Convert(context.Background(), svc.ConvertRequest{
		DatasetRequest: svc.DatasetRequest{Instrument: f.id, Interval: marketdata.D1}, ArchivePath: path, Force: force,
	})
}

func TestConvertImportsStooqIncrementallyByDefault(t *testing.T) {
	f := newIncrementalFixture(t)
	base := [][2]string{{"20200130", "100"}, {"20200131", "101"}, {"20200203", "102"}}

	first, err := f.convert(t, f.archive(t, "a.txt", base...), false)
	require.NoError(t, err)
	require.Equal(t, 3, first.Import.RowsAdded)
	require.False(t, first.Import.FullReimport)

	grown := append(append([][2]string{}, base...), [2]string{"20200204", "103"})
	second, err := f.convert(t, f.archive(t, "b.txt", grown...), false)
	require.NoError(t, err)
	require.Equal(t, 4, second.Import.RowsImported)
	require.Equal(t, 1, second.Import.RowsAdded, "only the row after the last raw date is new")
	require.Equal(t, 1, second.Import.MonthsWritten)
	require.Len(t, second.Build.Result.Published, 1, "only the touched month is rebuilt")

	third, err := f.convert(t, f.archive(t, "c.txt", grown...), false)
	require.NoError(t, err)
	require.Zero(t, third.Import.RowsAdded)
	require.Zero(t, third.Import.MonthsWritten)
	require.Empty(t, third.Build.Result.Published, "a repeat run changes nothing")
}

func TestConvertForceReimportsWholeStooqExport(t *testing.T) {
	f := newIncrementalFixture(t)
	rows := [][2]string{{"20200130", "100"}, {"20200131", "101"}, {"20200203", "102"}}
	path := f.archive(t, "a.txt", rows...)
	_, err := f.convert(t, path, false)
	require.NoError(t, err)

	forced, err := f.convert(t, path, true)
	require.NoError(t, err)
	require.Equal(t, 3, forced.Import.RowsAdded, "Force writes every row again")
	require.Equal(t, 2, forced.Import.MonthsWritten)
}

func TestConvertReportsStooqAdjustmentMismatch(t *testing.T) {
	f := newIncrementalFixture(t)
	_, err := f.convert(t, f.archive(t, "a.txt", [2]string{"20200130", "100"}, [2]string{"20200131", "101"}, [2]string{"20200203", "102"}), false)
	require.NoError(t, err)

	t.Run("full export re-imports history", func(t *testing.T) {
		resp, err := f.convert(t, f.archive(t, "b.txt", [2]string{"20200130", "50"}, [2]string{"20200131", "50.5"}, [2]string{"20200203", "51"}), false)
		require.NoError(t, err)
		require.True(t, resp.Import.FullReimport)
		require.Equal(t, 3, resp.Import.RowsAdded)
	})
	t.Run("partial export is refused", func(t *testing.T) {
		_, err := f.convert(t, f.archive(t, "c.txt", [2]string{"20200203", "25"}, [2]string{"20200204", "26"}), false)
		require.ErrorIs(t, err, marketruntime.ErrStooqAdjustmentMismatch)
	})
}
