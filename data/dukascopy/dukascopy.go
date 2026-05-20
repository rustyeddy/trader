// Package dukascopy implements the data.Provider interface for Dukascopy
// historical tick files. Raw data is hourly .bi5 files at:
//
//	https://datafeed.dukascopy.com/datafeed/<instrument>/<year>/<month-1>/<day>/<hour>h_ticks.bi5
package dukascopy

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"time"

	"github.com/rustyeddy/trader"
)

// SourceName is the canonical name under which this provider is registered.
const SourceName = "dukascopy"

// File represents one hourly tick file at the Dukascopy datafeed.
type File struct {
	key trader.Key

	symbol  string
	t       time.Time
	bytes   int64
	modtime time.Time
}

// NewFile returns a File describing the hourly tick file for the given
// instrument and time. The time is truncated to the start of the hour in UTC.
func NewFile(sym string, t time.Time) *File {
	t = time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), 0, 0, 0, time.UTC)
	f := &File{symbol: sym, t: t}
	f.Key()
	return f
}

// Key returns the trader.Key for this file's hourly tick slot.
func (f *File) Key() trader.Key {
	if f.key.Instrument == "" {
		f.key = trader.Key{
			Instrument: f.symbol,
			Source:     SourceName,
			Kind:       trader.KindTick,
			TF:         trader.Ticks,
			Year:       f.t.Year(),
			Month:      int(f.t.Month()),
			Day:        f.t.Day(),
			Hour:       f.t.Hour(),
		}
	}
	return f.key
}

// Instrument returns the FX symbol.
func (f *File) Instrument() string {
	return f.symbol
}

// URL returns the Dukascopy datafeed URL for this hour.
// Note: Dukascopy uses 0-indexed months in the path.
func (f *File) URL() string {
	return fmt.Sprintf(
		"https://datafeed.dukascopy.com/datafeed/%s/%04d/%02d/%02d/%02dh_ticks.bi5",
		f.symbol,
		f.t.Year(),
		f.t.Month()-1,
		f.t.Day(),
		f.t.Hour())
}

// IsValid checks that the local file for this hour exists and is either
// a legitimately empty market-closed file or a non-corrupt .bi5.
func (f *File) IsValid(ctx context.Context) error {
	store := trader.GetStore()
	ok, err := store.Exists(f.key)
	if err != nil || !ok {
		return err
	}

	path := f.key.Path()
	if f.bytes == 0 {
		if !f.t.IsZero() && trader.IsForexMarketClosed(f.t.UTC()) {
			return nil
		}
		return fmt.Errorf("empty file outside market-closed hours: %s", path)
	}

	baseUnixMS, err := f.baseHourUnixMS()
	if err != nil {
		return err
	}
	hourStart := baseUnixMS
	hourEnd := baseUnixMS + 3600_000

	it, err := store.OpenTickIterator(f.Key())
	if err != nil {
		return err
	}
	defer it.Close()

	for it.Next() {
		t := it.Item()
		ms := t.TimeMS()
		if ms < hourStart || ms >= hourEnd {
			return fmt.Errorf("first tick ts=%d outside hour [%d,%d) in %s",
				ms, hourStart, hourEnd, path)
		}
	}
	return it.Err()
}

var rePath = regexp.MustCompile(`[/\\](\d{4})[/\\](\d{2})[/\\](\d{2})[/\\](\d{2})h_ticks\.bi5$`)

func (f *File) baseHourUnixMS() (int64, error) {
	p := trader.GetStore().PathForAsset(f.Key())
	m := rePath.FindStringSubmatch(p)
	if m == nil {
		return 0, fmt.Errorf("cannot parse datetime from path: %s", p)
	}
	year, _ := strconv.Atoi(m[1])
	mon, _ := strconv.Atoi(m[2])
	day, _ := strconv.Atoi(m[3])
	hh, _ := strconv.Atoi(m[4])

	t := time.Date(year, time.Month(mon), day, hh, 0, 0, 0, time.UTC)
	return t.UnixMilli(), nil
}

// BitIsSet reports whether bit idx is set in a packed uint64 bitset.
func BitIsSet(bits []uint64, idx int) bool {
	return (bits[idx>>6] & (1 << uint(idx&63))) != 0
}

// BitSet sets bit idx in a packed uint64 bitset.
func BitSet(bits []uint64, idx int) {
	bits[idx>>6] |= 1 << uint(idx&63)
}
