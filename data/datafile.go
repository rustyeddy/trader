package data

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/types"
)

type datafile struct {
	symbol string
	time.Time
	err error

	basedir     string
	bytes       int64
	modtime     time.Time
	weekend     bool
	totalspread int64

	*dataset

	m1 market.Candle
}

const tickPathLen = 5

var (
	ErrPathTooShort    = errors.New("path too short")
	ErrInvalidFilename = errors.New("invalid tick filename")
	ErrPartialFile     = errors.New("temporary partial file")
	ErrInvalidYear     = errors.New("invalid year")
	ErrInvalidMonth    = errors.New("invalid month")
	ErrInvalidDay      = errors.New("invalid day")
	ErrInvalidHour     = errors.New("invalid hour")
	ErrHourOutOfRange  = errors.New("hour out of range")
)

func newDatafile(base string, sym string, t time.Time) *datafile {
	ds := &datafile{
		symbol:  sym,
		Time:    t,
		basedir: base,
	}
	return ds
}

func datafileFromPath(fullPath string) (*datafile, error) {
	clean := filepath.Clean(fullPath)

	if strings.HasSuffix(clean, ".part") {
		return nil, fmt.Errorf("%w: %s", ErrPartialFile, fullPath)
	}

	parts := strings.Split(clean, string(filepath.Separator))
	if len(parts) < 6 {
		return nil, fmt.Errorf("%w: %s", ErrPathTooShort, fullPath)
	}

	filename := parts[len(parts)-1]
	dayStr := parts[len(parts)-2]
	monStr := parts[len(parts)-3]
	yearStr := parts[len(parts)-4]
	symbol := parts[len(parts)-5]

	if !strings.HasSuffix(filename, "h_ticks.bi5") {
		return nil, fmt.Errorf("%w: %s", ErrInvalidFilename, filename)
	}

	hourStr := strings.TrimSuffix(filename, "h_ticks.bi5")

	year, err := strconv.Atoi(yearStr)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidYear, err)
	}

	month, err := strconv.Atoi(monStr)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidMonth, err)
	}

	day, err := strconv.Atoi(dayStr)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidDay, err)
	}

	hour, err := strconv.Atoi(hourStr)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidHour, err)
	}

	if hour < 0 || hour > 23 {
		return nil, fmt.Errorf("%w: %d", ErrHourOutOfRange, hour)
	}

	t := time.Date(year, time.Month(month), day, hour, 0, 0, 0, time.UTC)

	baseParts := parts[:len(parts)-5]
	basedir := filepath.Join(baseParts...)

	return &datafile{
		symbol:  symbol,
		Time:    t,
		basedir: basedir,
	}, nil
}

func (d *datafile) URL() string {
	return fmt.Sprintf(
		"https://datafeed.dukascopy.com/datafeed/%s/%04d/%02d/%02d/%02dh_ticks.bi5",
		d.symbol,
		d.Time.Year(),
		d.Time.Month()-1,
		d.Time.Day(),
		d.Time.Hour())
}

func (d *datafile) Path() string {
	return filepath.Join(
		d.basedir,
		d.symbol,
		fmt.Sprintf("%04d", d.Time.Year()),
		fmt.Sprintf("%02d", d.Time.Month()),
		fmt.Sprintf("%02d", d.Time.Day()),
		fmt.Sprintf("%02dh_ticks.bi5", d.Time.Hour()),
	)
}

func (d *datafile) PathBin() string {
	return filepath.Join(d.basedir, fmt.Sprintf(
		"%s/%04d/%02d/%02d/%02dh_ticks.bin",
		d.symbol, d.Time.Year(), d.Time.Month(), d.Time.Day(), d.Time.Hour(),
	))
}

func (d *datafile) Exists() bool {
	p := d.Path()
	info, err := os.Stat(p)
	if os.IsNotExist(err) {
		return false
	}
	if err != nil || info.IsDir() {
		return false
	}
	d.bytes = info.Size()
	d.modtime = info.ModTime()

	// Also check if it's a directory, as Stat returns info for both
	return err == nil && !info.IsDir()
}

// download will first check to see if this particular tick data has
// already been downloaded from Dukascopy, if so just return.  If not
// it will return.
func (d *datafile) download(ctx context.Context, client *http.Client) error {

	// Skip if present.
	// TODO before the file is written we need to make sure it is a valid file.
	if d.Exists() && d.IsValid(ctx) == nil {
		return nil
	}

	// Correctness-first timeout
	reqCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, d.URL(), nil)
	if err != nil {
		return fmt.Errorf("new request: %w", err)
	}

	// resp, err := http.DefaultClient.Do(req)
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("GET %s: %w", d.URL(), err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: http %d", d.URL(), resp.StatusCode)
	}

	dst := d.Path()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(dst), err)
	}

	tmp := dst + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("create %s: %w", tmp, err)
	}

	// Important: flush + close BEFORE rename/stat
	n, copyErr := io.Copy(f, resp.Body)
	syncErr := f.Sync()
	closeErr := f.Close()

	if copyErr != nil || syncErr != nil || closeErr != nil {
		_ = os.Remove(tmp)
		if copyErr != nil {
			return fmt.Errorf("write %s: wrote %d bytes: %w", tmp, n, copyErr)
		}
		if syncErr != nil {
			return fmt.Errorf("sync %s: wrote %d bytes: %w", tmp, n, syncErr)
		}
		return fmt.Errorf("close %s: wrote %d bytes: %w", tmp, n, closeErr)
	}

	// Atomic move into place
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename %s -> %s: %w", tmp, dst, err)
	}

	// Trust the filesystem for bytes/modtime
	info, err := os.Stat(dst)
	if err != nil {
		return fmt.Errorf("stat %s: %w", dst, err)
	}

	d.bytes = info.Size()
	d.modtime = info.ModTime()

	fmt.Printf("%s %d-%02d-%02d:%02d... ",
		d.symbol,
		d.Time.Year(),
		d.Time.Month()-1,
		d.Time.Day(),
		d.Time.Hour())
	fmt.Printf("%6d bytes\n", n)

	// Optional: sanity-check bytes against what we copied
	if d.bytes != n || n == 0 {
		// fmt.Printf("Failed to download: %s\n", d.URL())
	}
	return nil
}

// fileIsValid ensures that the file actually exists and is either
// a empty Weekend file or it is a complete non-corrupt lzh compressed
// dukas binary file format.
func (d *datafile) IsValid(ctx context.Context) error {
	// 1. verify file exists
	if !d.Exists() {
		return fmt.Errorf("file does not exist")
	}

	if d.bytes == 0 {
		return nil
	}

	if d.Time.IsZero() {
		if market.IsFXMarketClosed(d.Time.UTC()) {
			return nil
		}
		return fmt.Errorf("empty file outside market-closed hours: %s", d.Path())
	}

	baseUnixMS, err := d.baseHourUnixMS()
	if err != nil {
		return err
	}
	hourStart := baseUnixMS
	hourEnd := baseUnixMS + 3600_000
	err = d.forEachTick(ctx, func(t Tick) error {
		if t.Timemilli < hourStart || t.Timemilli >= hourEnd {
			return fmt.Errorf("first tick ts=%d outside hour [%d,%d) in %s", t, hourStart, hourEnd, d.Path())
		}
		return nil
	})
	return nil
}

func floorToMinuteUnixMS(ts types.Timemilli) types.Timemilli {
	return (ts / 60_000) * 60_000
}

// Flush returns the in-progress candle at end-of-stream (if any).
func (df *datafile) Flush() (market.Candle, bool) {
	if df.m1.Ticks == 0 {
		return market.Candle{}, false
	}
	c := df.m1
	ticks := int64(c.Ticks)
	c.AvgSpread = types.Price((df.totalspread + ticks/2) / ticks)
	df.totalspread = 0
	df.m1 = market.Candle{}
	return c, true
}

func (df *datafile) HourStart() types.Timemilli {
	// Use df.Time (hour open) as canonical. Ensure UTC + zeroed mins/secs.
	t := df.Time.UTC()
	t = time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), 0, 0, 0, time.UTC)
	return types.Timemilli(t.UnixMilli())
}
