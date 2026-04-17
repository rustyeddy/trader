package data

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/rustyeddy/trader/types"
)

type dukasfile struct {
	key Key

	symbol string
	time.Time
	err error

	bytes       int64
	modtime     time.Time
	weekend     bool
	totalspread int64

	m1 types.Candle
}

func newDatafile(sym string, t time.Time) *dukasfile {
	// Canonicalize to UTC wall-clock hour (matches Dukascopy folder semantics).
	t = time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), 0, 0, 0, time.UTC)
	df := &dukasfile{
		symbol: sym,
		Time:   t,
	}
	df.Key()
	return df
}

func (d *dukasfile) Key() Key {
	if d.key.Instrument == "" {
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
	}
	return d.key
}

func (d *dukasfile) Instrument() string {
	return d.symbol
}

func (d *dukasfile) URL() string {
	return fmt.Sprintf(
		"https://datafeed.dukascopy.com/datafeed/%s/%04d/%02d/%02d/%02dh_ticks.bi5",
		d.symbol,
		d.Time.Year(),
		d.Time.Month()-1,
		d.Time.Day(),
		d.Time.Hour())
}

// fileIsValid ensures that the file actually exists and is either
// a empty Weekend file or it is a complete non-corrupt lzh compressed
// dukas binary file format.
func (d *dukasfile) IsValid(ctx context.Context) error {
	// 1. verify file exists
	ok, err := store.Exists(d.key)
	if err != nil || !ok {
		return err
	}

	if d.bytes == 0 {
		return nil
	}

	path := d.key.Path()
	if !d.Time.IsZero() {
		if types.IsFXMarketClosed(d.Time.UTC()) {
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

	it, err := store.OpenTickIterator(d.Key())
	if err != nil {
		return err
	}
	for it.Next() {
		t := it.Item()

		if t.Timemilli < hourStart || t.Timemilli >= hourEnd {
			return fmt.Errorf("first tick ts=%d outside hour [%d,%d) in %s",
				t.Timemilli, hourStart, hourEnd, path)
		}
	}
	return nil
}

func (d *dukasfile) baseHourUnixMS() (types.Timemilli, error) {
	p := store.PathForAsset(d.Key())
	m := rePath.FindStringSubmatch(p)
	if m == nil {
		return 0, fmt.Errorf("cannot parse datetime from path: %s", p)
	}
	year, _ := strconv.Atoi(m[1])
	mon, _ := strconv.Atoi(m[2])
	day, _ := strconv.Atoi(m[3])
	hh, _ := strconv.Atoi(m[4])

	t := time.Date(year, time.Month(mon), day, hh, 0, 0, 0, time.UTC)
	return types.Timemilli(t.UnixMilli()), nil
}

func (d *dukasfile) forEachTick1(ctx context.Context, fn func(Tick) error) error {
	it, err := store.OpenTickIterator(d.Key())
	if err != nil {
		return err
	}
	defer it.Close()

	for it.Next() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if err := fn(it.Item()); err != nil {
			return err
		}
	}

	return it.Err()
}

// TODO move these somewhere
// If you can't access market's bitSet/bitIsSet because they are unexported,
// include these tiny helpers in the data package (or export them from market).
func bitIsSet(bits []uint64, idx int) bool {
	return (bits[idx>>6] & (1 << uint(idx&63))) != 0
}
func bitSet(bits []uint64, idx int) {
	bits[idx>>6] |= 1 << uint(idx&63)
}
