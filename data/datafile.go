package data

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/types"
)

type datafile struct {
	key Key

	symbol string
	time.Time
	err error

	basedir     string
	bytes       int64
	modtime     time.Time
	weekend     bool
	totalspread int64

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

func newDatafile(sym string, t time.Time) *datafile {
	// Canonicalize to UTC wall-clock hour (matches Dukascopy folder semantics).
	t = time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), 0, 0, 0, time.UTC)
	df := &datafile{
		symbol: sym,
		Time:   t,
	}
	return df
}

func (d datafile) Key() Key {
	d.key = Key{
		Instrument: d.symbol,
		Source:     "dukascopy",
		Kind:       KindTick,
		TF:         types.Ticks,
		Year:       d.Time.Year(),
		Month:      int(d.Time.Month()),
		Day:        d.Time.Day(),
		Hour:       d.Time.Hour(),
	}
	return d.key
}


func (d *datafile) Instrument() string {
	return d.symbol
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

// func (d *datafile) Path() string {
// 	return filepath.Join(
// 		d.basedir,
// 		d.symbol,
// 		fmt.Sprintf("%04d", d.Time.Year()),
// 		fmt.Sprintf("%02d", d.Time.Month()),
// 		fmt.Sprintf("%02d", d.Time.Day()),
// 		fmt.Sprintf("%02dh_ticks.bi5", d.Time.Hour()),
// 	)
// }

// PathBin() TODO move to the Store. Store owns the filesystem
// func (d *datafile) PathBin() string {
// 	return filepath.Join(d.basedir, fmt.Sprintf(
// 		"%s/%04d/%02d/%02d/%02dh_ticks.bin",
// 		d.symbol, d.Time.Year(), d.Time.Month(), d.Time.Day(), d.Time.Hour(),
// 	))
// }

// download will first check to see if this particular tick data has
// already been downloaded from Dukascopy, if so just return.  If not
// it will return.
func (d *datafile) download(ctx context.Context, client *http.Client) error {
	k := d.Key()

	// Skip if present.
	// TODO before the file is written we need to make sure it is a valid file.
	ok, err := store.Exists(k)
	if ok {
		return nil
	}

	// Correctness-first timeout
	reqCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
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

	dst := store.PathForAsset(k)
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
	ok, err := store.Exists(d.Key())
	if err != nil || !ok {
		return err
	}

	if d.bytes == 0 {
		return nil
	}

	path := store.PathForAsset(d.key)
	if !d.Time.IsZero() {
		if market.IsFXMarketClosed(d.Time.UTC()) {
			return nil
		}
		return fmt.Errorf("empty file outside market-closed hours: %s", path)
	}

	baseUnixMS, err := d.baseHourUnixMS()
	if err != nil {
		return err
	}
	hourStart := baseUnixMS
	hourEnd := baseUnixMS + 3600_000
	err = d.forEachTick(ctx, func(t Tick) error {
		if t.Timemilli < hourStart || t.Timemilli >= hourEnd {
			return fmt.Errorf("first tick ts=%d outside hour [%d,%d) in %s",
				t, hourStart, hourEnd, path)
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

func (df *datafile) hourStart() types.Timemilli {
	t := df.Time
	t = time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), 0, 0, 0, time.UTC)
	return types.Timemilli(t.UnixMilli())
}

// NOTE: If Tick.Timestamp is already unix seconds, remove the /1000 conversion below.
// This implementation assumes Tick.Timestamp is unix milliseconds.
func (df *datafile) buildM1(ctx context.Context) (*market.CandleSet, error) {
	const minutesPerHour = 60
	hourStart := df.hourStart()

	inst := market.GetInstrument(df.symbol)
	if inst == nil {
		return nil, fmt.Errorf("Didnot find and instrument with the symbol %s\n", df.symbol)
	}
	cs := &market.CandleSet{
		Instrument: inst,            // adjust if your lookup differs
		Start:      hourStart.Sec(), // Timemilli -> Timestamp (seconds)
		Timeframe:  60,
		Scale:      1_000_000,
		Source:     "dukascopy",
		Candles:    make([]market.Candle, minutesPerHour),
		Valid:      make([]uint64, (minutesPerHour+63)/64),
	}

	var (
		curIdx        = -1
		cur           market.Candle
		spreadSum     int64
		havePrevClose bool
		prevClose     types.Price
	)

	finalize := func() error {
		if curIdx < 0 {
			return nil
		}
		if cur.Ticks <= 0 {
			return nil
		}
		ticks := int64(cur.Ticks)
		cur.AvgSpread = types.Price((spreadSum + ticks/2) / ticks)

		cs.Candles[curIdx] = cur
		bitSet(cs.Valid, curIdx)

		prevClose = cur.Close
		havePrevClose = true
		return nil
	}

	fillFlat := func(idx int, px types.Price) {
		// Fill OHLC but do NOT set Valid bit.
		cs.Candles[idx] = market.Candle{
			Open:  px,
			High:  px,
			Low:   px,
			Close: px,
			Ticks: 0,
		}
	}

	err := df.forEachTick(ctx, func(t Tick) error {
		ts := t.Timemilli
		if ts <= 0 {
			return fmt.Errorf("bad tick timestamp: %d", t.Timemilli)
		}

		// They should all agree to within [hourStart, hourStart+3600000).
		minuteOpen := ts.FloorToMinute()
		idx := int((minuteOpen - hourStart) / types.MinuteInMS) // 60_000
		if idx < 0 || idx >= minutesPerHour {
			return fmt.Errorf("tick outside hour window: minute=%d hourStart=%d idx=%d",
				minuteOpen, hourStart, idx)
		}

		mid := t.Mid()
		spread := t.Spread()

		if curIdx == -1 {
			curIdx = idx
			cur = market.Candle{
				Open:      mid,
				High:      mid,
				Low:       mid,
				Close:     mid,
				Ticks:     1,
				MaxSpread: spread,
			}
			spreadSum = int64(spread)
			return nil
		}

		if idx == curIdx {
			if mid > cur.High {
				cur.High = mid
			}
			if mid < cur.Low {
				cur.Low = mid
			}
			cur.Close = mid
			cur.Ticks++

			if spread > cur.MaxSpread {
				cur.MaxSpread = spread
			}
			spreadSum += int64(spread)
			return nil
		}

		if idx < curIdx {
			return fmt.Errorf("out-of-order tick minute: idx %d < curIdx %d", idx, curIdx)
		}

		if err := finalize(); err != nil {
			return err
		}

		if havePrevClose {
			for m := curIdx + 1; m < idx; m++ {
				if !bitIsSet(cs.Valid, m) && isZeroCandle(cs.Candles[m]) {
					fillFlat(m, prevClose)
				}
			}
		}

		curIdx = idx
		cur = market.Candle{
			Open:      mid,
			High:      mid,
			Low:       mid,
			Close:     mid,
			Ticks:     1,
			MaxSpread: spread,
		}
		spreadSum = int64(spread)
		return nil
	})
	if err != nil {
		return nil, err
	}

	if err := finalize(); err != nil {
		return nil, err
	}

	if havePrevClose && curIdx >= 0 {
		for m := curIdx + 1; m < minutesPerHour; m++ {
			if !bitIsSet(cs.Valid, m) && isZeroCandle(cs.Candles[m]) {
				fillFlat(m, prevClose)
			}
		}
	}

	return cs, nil
}

func isZeroCandle(c market.Candle) bool {
	return c.Open == 0 && c.High == 0 && c.Low == 0 && c.Close == 0 && c.Ticks == 0
}

// If you can't access market's bitSet/bitIsSet because they are unexported,
// include these tiny helpers in the data package (or export them from market).
func bitIsSet(bits []uint64, idx int) bool {
	return (bits[idx>>6] & (1 << uint(idx&63))) != 0
}
func bitSet(bits []uint64, idx int) {
	bits[idx>>6] |= 1 << uint(idx&63)
}
