package marketdata_test

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/internal/clock"
	marketruntime "github.com/rustyeddy/trader/internal/marketdata"
	svc "github.com/rustyeddy/trader/internal/service/marketdata"
	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
)

const stooqHeader = "<TICKER>,<PER>,<DATE>,<TIME>,<OPEN>,<HIGH>,<LOW>,<CLOSE>,<VOL>,<OPENINT>\n"

// spyTwoMonths spans January and February 2020.
const spyTwoMonths = stooqHeader +
	"SPY.US,D,20200131,000000,100,101,99,100.5,1000,0\n" +
	"SPY.US,D,20200203,000000,100.5,102,100,101.5,2000,0\n"

func writeZIP(t *testing.T, path string, members map[string]string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	f, err := os.Create(path)
	require.NoError(t, err)
	zw := zip.NewWriter(f)
	for name, content := range members {
		w, err := zw.Create(name)
		require.NoError(t, err)
		_, err = w.Write([]byte(content))
		require.NoError(t, err)
	}
	require.NoError(t, zw.Close())
	require.NoError(t, f.Close())
}

// stooqFixture is a stooq Service with SPY registered.
type stooqFixture struct {
	service   *svc.Service
	spy       instrument.ID
	rawRoot   string
	storeRoot string
}

func newStooqFixture(t *testing.T) stooqFixture {
	t.Helper()
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
	return stooqFixture{service: service, spy: id, rawRoot: rawRoot, storeRoot: storeRoot}
}

func (f stooqFixture) request(archivePath, archiveRoot string) svc.ConvertStooqArchiveRequest {
	return svc.ConvertStooqArchiveRequest{
		DatasetRequest: svc.DatasetRequest{Instrument: f.spy, Interval: marketdata.D1},
		Symbol:         "SPY",
		ArchivePath:    archivePath,
		ArchiveRoot:    archiveRoot,
	}
}

func TestDefaultListing(t *testing.T) {
	for symbol, want := range map[string]svc.ListingDefault{
		"SPY":   {Exchange: "ARCA", Kind: "etf"},
		" qqq ": {Exchange: "NASDAQ", Kind: "etf"},
		"aapl":  {Exchange: "NASDAQ", Kind: "equity"},
	} {
		got, ok := svc.DefaultListing(symbol)
		require.True(t, ok, symbol)
		assert.Equal(t, want, got, symbol)
	}
	_, ok := svc.DefaultListing("UNKNOWN")
	assert.False(t, ok)
	_, ok = svc.DefaultListing("")
	assert.False(t, ok)
}

func TestResolveListingIdentity(t *testing.T) {
	t.Run("explicit values win over a default", func(t *testing.T) {
		got, err := svc.ResolveListingIdentity("SPY", "NYSE", "equity")
		require.NoError(t, err)
		assert.Equal(t, svc.ListingDefault{Exchange: "NYSE", Kind: "equity"}, got)
	})
	t.Run("explicit values for an unknown symbol", func(t *testing.T) {
		got, err := svc.ResolveListingIdentity("MSFT", "NASDAQ", "equity")
		require.NoError(t, err)
		assert.Equal(t, svc.ListingDefault{Exchange: "NASDAQ", Kind: "equity"}, got)
	})
	t.Run("default when neither is given", func(t *testing.T) {
		got, err := svc.ResolveListingIdentity("spy", "", "")
		require.NoError(t, err)
		assert.Equal(t, svc.ListingDefault{Exchange: "ARCA", Kind: "etf"}, got)
	})
	t.Run("unknown symbol without exchange and kind", func(t *testing.T) {
		_, err := svc.ResolveListingIdentity("unknown", "", "")
		require.ErrorIs(t, err, svc.ErrNoListingDefault)
		assert.ErrorContains(t, err, `"UNKNOWN"`)
	})
	for name, args := range map[string][2]string{"exchange only": {"ARCA", ""}, "kind only": {"", "etf"}} {
		t.Run(name, func(t *testing.T) {
			_, err := svc.ResolveListingIdentity("SPY", args[0], args[1])
			require.ErrorIs(t, err, svc.ErrInvalidRequest)
			assert.ErrorContains(t, err, "together")
		})
	}
}

func TestFindStooqArchive(t *testing.T) {
	t.Run("finds the unique archive in a nested directory", func(t *testing.T) {
		root := t.TempDir()
		path := filepath.Join(root, "daily", "SPY_us_d.ZIP")
		writeZIP(t, path, nil)
		writeZIP(t, filepath.Join(root, "qqq_us_d.zip"), nil)
		require.NoError(t, os.WriteFile(filepath.Join(root, "spy.txt"), nil, 0o644))

		got, err := svc.FindStooqArchive(root, "spy")
		require.NoError(t, err)
		assert.Equal(t, path, got)
	})
	t.Run("matches whole name tokens only", func(t *testing.T) {
		root := t.TempDir()
		writeZIP(t, filepath.Join(root, "spyder.zip"), nil)
		_, err := svc.FindStooqArchive(root, "SPY")
		require.ErrorIs(t, err, svc.ErrArchiveNotFound)
	})
	t.Run("missing archive", func(t *testing.T) {
		root := t.TempDir()
		_, err := svc.FindStooqArchive(root, "SPY")
		require.ErrorIs(t, err, svc.ErrArchiveNotFound)
		assert.ErrorContains(t, err, "SPY")
		assert.ErrorContains(t, err, root)
	})
	t.Run("ambiguous archives", func(t *testing.T) {
		root := t.TempDir()
		writeZIP(t, filepath.Join(root, "spy_2024.zip"), nil)
		writeZIP(t, filepath.Join(root, "spy-2025.zip"), nil)
		_, err := svc.FindStooqArchive(root, "SPY")
		require.ErrorIs(t, err, svc.ErrAmbiguousArchive)
	})
	t.Run("root not configured", func(t *testing.T) {
		_, err := svc.FindStooqArchive("", "SPY")
		require.ErrorIs(t, err, svc.ErrArchiveRootNotConfigured)
	})
	t.Run("unreadable root", func(t *testing.T) {
		_, err := svc.FindStooqArchive(filepath.Join(t.TempDir(), "absent"), "SPY")
		require.Error(t, err)
		assert.NotErrorIs(t, err, svc.ErrArchiveNotFound)
		assert.ErrorContains(t, err, "search Stooq archive root")
	})
}

func TestConvertStooqArchive_DiscoversArchiveAndDefaultsRangeToSource(t *testing.T) {
	f := newStooqFixture(t)
	t.Setenv("TMPDIR", t.TempDir())
	root := t.TempDir()
	archivePath := filepath.Join(root, "daily", "spy_us_d.zip")
	writeZIP(t, archivePath, map[string]string{"data/daily/us/nyse etfs/2/spy.us.txt": spyTwoMonths})
	before := sha256.Sum256(mustRead(t, archivePath))

	resp, err := f.service.ConvertStooqArchive(context.Background(), f.request("", root))
	require.NoError(t, err)

	assert.Equal(t, 2, resp.Import.RowsImported)
	assert.Equal(t, 2, resp.Import.MonthsWritten)
	assert.Len(t, resp.Build.Result.Published, 2, "a zero range converts the whole source span")
	assert.FileExists(t, filepath.Join(f.storeRoot, "stooq", "SPY", "2020", "02", "SPY-2020-02-d1.csv"))
	assert.Equal(t, before, sha256.Sum256(mustRead(t, archivePath)), "archive must not be modified")
	entries, err := os.ReadDir(os.TempDir())
	require.NoError(t, err)
	assert.Empty(t, entries, "temporary extraction directory must be removed")
}

func TestConvertStooqArchive_ExplicitArchiveAndRangeClipsToSource(t *testing.T) {
	f := newStooqFixture(t)
	archivePath := filepath.Join(t.TempDir(), "anything.zip")
	writeZIP(t, archivePath, map[string]string{"spy.us.txt": spyTwoMonths})
	req := f.request(archivePath, "")
	span, err := marketdata.NewTimeRange(time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2020, 2, 1, 0, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	req.Range = span

	resp, err := f.service.ConvertStooqArchive(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, 2, resp.Import.RowsImported, "import is not clipped")
	assert.Len(t, resp.Build.Result.Published, 1, "build covers only the requested range")
}

func TestConvertStooqArchive_Failures(t *testing.T) {
	f := newStooqFixture(t)
	qqqOnly := filepath.Join(t.TempDir(), "spy_us_d.zip")
	writeZIP(t, qqqOnly, map[string]string{"qqq.us.txt": stooqHeader})
	notZIP := filepath.Join(t.TempDir(), "spy.zip")
	require.NoError(t, os.WriteFile(notZIP, []byte("not a zip"), 0o644))

	tests := map[string]struct {
		mutate func(*svc.ConvertStooqArchiveRequest)
		is     error
		msg    string
	}{
		"missing archive under root": {
			mutate: func(r *svc.ConvertStooqArchiveRequest) { r.ArchiveRoot = t.TempDir() },
			is:     svc.ErrArchiveNotFound,
		},
		"no archive and no root": {
			mutate: func(r *svc.ConvertStooqArchiveRequest) {},
			is:     svc.ErrArchiveRootNotConfigured,
		},
		"missing symbol member": {
			mutate: func(r *svc.ConvertStooqArchiveRequest) { r.ArchivePath = qqqOnly },
			is:     svc.ErrArchiveMemberNotFound,
			msg:    "contains no spy.us.txt member",
		},
		"archive is not a ZIP": {
			mutate: func(r *svc.ConvertStooqArchiveRequest) { r.ArchivePath = notZIP },
			msg:    "open archive",
		},
		"empty symbol": {
			mutate: func(r *svc.ConvertStooqArchiveRequest) { r.Symbol = " " },
			is:     svc.ErrInvalidRequest,
			msg:    "symbol is required",
		},
		"non-daily interval is rejected before discovery": {
			mutate: func(r *svc.ConvertStooqArchiveRequest) { r.Interval = marketdata.H1 },
			is:     svc.ErrInvalidRequest,
			msg:    "supports only D1",
		},
		"zero instrument": {
			mutate: func(r *svc.ConvertStooqArchiveRequest) { r.Instrument = instrument.ID{} },
			is:     svc.ErrInvalidRequest,
		},
		"unregistered instrument": {
			mutate: func(r *svc.ConvertStooqArchiveRequest) {
				r.Instrument = instrument.ETFID("ARCA", "QQQ")
				r.ArchivePath = qqqOnly
				r.Symbol = "QQQ"
			},
			msg: "resolve listing",
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			req := f.request("", "")
			tc.mutate(&req)
			_, err := f.service.ConvertStooqArchive(context.Background(), req)
			require.Error(t, err)
			if tc.is != nil {
				assert.ErrorIs(t, err, tc.is)
			}
			if tc.msg != "" {
				assert.ErrorContains(t, err, tc.msg)
			}
		})
	}
}

func TestConvertStooqArchive_CanceledContext(t *testing.T) {
	f := newStooqFixture(t)
	archivePath := filepath.Join(t.TempDir(), "spy.zip")
	writeZIP(t, archivePath, map[string]string{"spy.us.txt": spyTwoMonths})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := f.service.ConvertStooqArchive(ctx, f.request(archivePath, ""))
	require.ErrorIs(t, err, context.Canceled)
	entries, err := os.ReadDir(f.rawRoot)
	require.NoError(t, err)
	assert.Empty(t, entries, "nothing is imported")
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	return b
}
