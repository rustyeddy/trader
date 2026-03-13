package data

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rustyeddy/trader/types"
)

type DataKind uint8

const (
	KindUnknown DataKind = iota
	KindTick
	KindCandle
)

func (k DataKind) String() string {
	switch k {
	case KindTick:
		return "ticks"
	case KindCandle:
		return "candles"
	default:
		return "unknown"
	}
}

type AssetKey struct {
	Source     string
	Instrument string
	Kind       DataKind
	TF         types.Timeframe

	Year  int
	Month int
	Day   int
	Hour  int

	Path string
}

// compare returns:
//
//	-1 if ak < k
//	 0 if ak == k
//	 1 if ak > k
func (ak AssetKey) compare(k AssetKey) int {
	if ak.Source < k.Source {
		return -1
	}
	if ak.Source > k.Source {
		return 1
	}

	if ak.Instrument < k.Instrument {
		return -1
	}
	if ak.Instrument > k.Instrument {
		return 1
	}

	if ak.Kind < k.Kind {
		return -1
	}
	if ak.Kind > k.Kind {
		return 1
	}

	if ak.TF < k.TF {
		return -1
	}
	if ak.TF > k.TF {
		return 1
	}

	if ak.Year < k.Year {
		return -1
	}
	if ak.Year > k.Year {
		return 1
	}

	if ak.Month < k.Month {
		return -1
	}
	if ak.Month > k.Month {
		return 1
	}

	if ak.Day < k.Day {
		return -1
	}
	if ak.Day > k.Day {
		return 1
	}

	if ak.Hour < k.Hour {
		return -1
	}
	if ak.Hour > k.Hour {
		return 1
	}

	// Optional: compare path last, though ideally it should not be here.
	if ak.Path < k.Path {
		return -1
	}
	if ak.Path > k.Path {
		return 1
	}

	return 0
}

func (ak AssetKey) before(k AssetKey) bool {
	return ak.compare(k) < 0
}

func (ak AssetKey) after(k AssetKey) bool {
	return ak.compare(k) > 0
}

// Time returns the UTC time represented by the key.
// Missing fields are normalized to the earliest valid value.
//
// Examples:
//
//	Year=2024, Month=0, Day=0, Hour=0 -> 2024-01-01 00:00:00 UTC
//	Year=2024, Month=5, Day=0, Hour=0 -> 2024-05-01 00:00:00 UTC
//	Year=2024, Month=5, Day=7, Hour=13 -> 2024-05-07 13:00:00 UTC
func (ak AssetKey) Time() time.Time {
	year := ak.Year
	if year <= 0 {
		year = 1970
	}

	month := ak.Month
	if month < 1 || month > 12 {
		month = 1
	}

	day := ak.Day
	if day < 1 || day > 31 {
		day = 1
	}

	hour := ak.Hour
	if hour < 0 || hour > 23 {
		hour = 0
	}

	return time.Date(year, time.Month(month), day, hour, 0, 0, 0, time.UTC)
}

// IsForexMarketClosed reports whether spot FX is closed at time t.
//
// Common retail FX convention:
//   - Opens Sunday 17:00 New York time
//   - Closes Friday 17:00 New York time
//
// This function ignores special holiday closures for now.
// It converts t into America/New_York and applies the weekly session rules.
func IsForexMarketClosed(t time.Time) bool {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		// conservative fallback: use UTC if timezone load fails
		loc = time.UTC
	}

	nt := t.In(loc)
	wd := nt.Weekday()
	h := nt.Hour()

	switch wd {
	case time.Saturday:
		return true
	case time.Sunday:
		// closed until 17:00 NY time
		return h < 17
	case time.Friday:
		// closed from 17:00 NY time onward
		return h >= 17
	default:
		return false
	}
}

type Asset struct {
	Key        AssetKey
	Path       string
	Range      types.TimeRange
	Exists     bool
	Complete   bool
	Size       int64
	UpdatedAt  time.Time
	SourceAge  time.Time // optional: mtime of prerequisite/source
	Descriptor string
}

type Inventory struct {
	assets map[AssetKey]Asset
}

func NewInventory() *Inventory {
	return &Inventory{
		assets: make(map[AssetKey]Asset),
	}
}

func (inv *Inventory) Put(a Asset) {
	inv.assets[a.Key] = a
}

func (inv *Inventory) Get(key AssetKey) (Asset, bool) {
	a, ok := inv.assets[key]
	return a, ok
}

func (inv *Inventory) HasComplete(key AssetKey) bool {
	a, ok := inv.assets[key]
	return ok && a.Exists && a.Complete
}

func (inv *Inventory) Has(source, instrument string, kind DataKind, tf types.Timeframe, year int) bool {
	if kind == KindTick {
		tf = types.TF0
	}

	_, ok := inv.assets[AssetKey{
		Source:     normalizeSource(source),
		Instrument: normalizeInstrument(instrument),
		Kind:       kind,
		TF:         tf,
		Year:       year,
	}]
	return ok
}

func (inv *Inventory) Years(source, instrument string, kind DataKind, tf types.Timeframe) []int {
	source = normalizeSource(source)
	instrument = normalizeInstrument(instrument)

	var years []int
	for k := range inv.assets {
		if k.Source == source &&
			k.Instrument == instrument &&
			k.Kind == kind &&
			k.TF == tf {
			years = append(years, k.Year)
		}
	}
	sort.Ints(years)
	return years
}

func (inv *Inventory) LatestYear(source, instrument string, kind DataKind, tf types.Timeframe) (int, bool) {
	years := inv.Years(source, instrument, kind, tf)
	if len(years) == 0 {
		return 0, false
	}
	return years[len(years)-1], true
}

func (inv *Inventory) MissingYears(source, instrument string, kind DataKind, tf types.Timeframe, startYear, endYear int) []int {
	var missing []int
	for y := startYear; y <= endYear; y++ {
		if !inv.Has(source, instrument, kind, tf, y) {
			missing = append(missing, y)
		}
	}
	return missing
}

func (inv *Inventory) StaleDerived(source, instrument string, tf types.Timeframe, year int) (bool, error) {
	var parentTF types.Timeframe
	switch tf {
	case types.H1:
		parentTF = types.M1
	case types.D1:
		parentTF = types.H1
	default:
		return false, fmt.Errorf("timeframe %s has no derived parent", tf.String())
	}

	child, ok := inv.Get(AssetKey{
		Source:     normalizeSource(source),
		Instrument: normalizeInstrument(instrument),
		Kind:       KindCandle,
		TF:         tf,
		Year:       year,
	})
	if !ok {
		return false, fmt.Errorf("missing child asset")
	}

	parent, ok := inv.Get(AssetKey{
		Source:     normalizeSource(source),
		Instrument: normalizeInstrument(instrument),
		Kind:       KindCandle,
		TF:         parentTF,
		Year:       year,
	})
	if !ok {
		return false, fmt.Errorf("missing parent asset")
	}

	return parent.UpdatedAt.After(child.UpdatedAt), nil
}

func (inv *Inventory) NeedsDownload(key AssetKey) bool {
	a, ok := inv.assets[key]
	if !ok {
		return true
	}
	if !a.Exists || !a.Complete {
		return true
	}
	return false
}

func normalizeSource(s string) string {
	return strings.TrimSpace(strings.ToLower(s))
}

type InventoryBuilder struct {
	TicksRoot   string // e.g. ../../tmp/dukas
	CandlesRoot string // e.g. data/candles
}

func NewInventoryBuilder(ticksRoot, candlesRoot string) *InventoryBuilder {
	return &InventoryBuilder{
		TicksRoot:   ticksRoot,
		CandlesRoot: candlesRoot,
	}
}

func (b *InventoryBuilder) Build(ctx context.Context) (*Inventory, error) {
	inv := NewInventory()

	if b.TicksRoot != "" {
		if err := b.scanTicks(inv); err != nil {
			return nil, err
		}
	}
	if b.CandlesRoot != "" {
		if err := b.scanCandles(inv); err != nil {
			// if the candles file does not exist we just assume no candles
			// exist, so it is not an error.
			if errors.Is(err, os.ErrNotExist) {
				return inv, nil
			}
			return nil, err
		}
	}

	return inv, nil
}

func (b *InventoryBuilder) scanTicks(inv *Inventory) error {
	type tickYearAgg struct {
		pathCount int
		firstPath string
		minYear   int
		maxYear   int
		modTime   time.Time
		size      int64
	}

	agg := map[string]*tickYearAgg{}

	err := filepath.Walk(b.TicksRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(info.Name()), ".bi5") {
			return nil
		}

		inst, year, ok := parseTickPath(path)
		if !ok {
			return nil
		}

		key := fmt.Sprintf("%s|%d", inst, year)
		a := agg[key]
		if a == nil {
			a = &tickYearAgg{
				firstPath: path,
				minYear:   year,
				maxYear:   year,
				modTime:   info.ModTime(),
			}
			agg[key] = a
		}
		a.pathCount++
		a.size += info.Size()
		if info.ModTime().After(a.modTime) {
			a.modTime = info.ModTime()
		}
		return nil
	})
	if err != nil {
		return err
	}

	for key, a := range agg {
		parts := strings.Split(key, "|")
		inst := parts[0]
		year, _ := strconv.Atoi(parts[1])

		inv.Put(Asset{
			Key: AssetKey{
				Source:     "dukascopy",
				Instrument: inst,
				Kind:       KindTick,
				TF:         types.TF0,
				Year:       year,
			},
			Path:       a.firstPath,
			Range:      types.YearRange(year),
			UpdatedAt:  a.modTime,
			Complete:   true, // can later validate expected hour count
			Descriptor: fmt.Sprintf("dukascopy raw bi5 files (%d files)", a.pathCount),
		})
	}

	return nil
}

func parseTickPath(path string) (instrument string, year int, ok bool) {
	p := filepath.ToSlash(path)
	parts := strings.Split(p, "/")
	if len(parts) < 5 {
		return "", 0, false
	}

	// Expect .../<instrument>/<year>/<month>/<day>/<file>
	n := len(parts)
	instrument = normalizeInstrument(parts[n-5])

	y, err := strconv.Atoi(parts[n-4])
	if err != nil {
		return "", 0, false
	}

	return instrument, y, true
}

func (b *InventoryBuilder) scanCandles(inv *Inventory) error {
	return filepath.Walk(b.CandlesRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(info.Name()), ".csv") {
			return nil
		}

		source, inst, tf, year, ok := parseCandlePath(path)
		if !ok {
			return nil
		}

		inv.Put(Asset{
			Key: AssetKey{
				Source:     source,
				Instrument: inst,
				Kind:       KindCandle,
				TF:         tf,
				Year:       year,
			},
			Path:       path,
			Range:      types.YearRange(year),
			Size:       info.Size(),
			UpdatedAt:  info.ModTime(),
			Complete:   true,
			Descriptor: "yearly candle csv",
		})
		return nil
	})
}

func parseCandlePath(path string) (source string, instrument string, tf types.Timeframe, year int, ok bool) {
	p := filepath.ToSlash(path)
	parts := strings.Split(p, "/")
	if len(parts) < 4 {
		return "", "", types.TF0, 0, false
	}

	// Expect .../<source>/<instrument>/<tf>/<file>.csv
	n := len(parts)
	source = normalizeSource(parts[n-4])
	instrument = normalizeInstrument(parts[n-3])

	tfStr := strings.ToUpper(parts[n-2])
	switch tfStr {
	case "M1":
		tf = types.M1
	case "H1":
		tf = types.H1
	case "D1":
		tf = types.D1
	default:
		return "", "", types.TF0, 0, false
	}

	base := strings.ToLower(strings.TrimSuffix(parts[n-1], ".csv"))
	// e.g. eurusd-m1-2026
	nameParts := strings.Split(base, "-")
	if len(nameParts) < 3 {
		return "", "", types.TF0, 0, false
	}

	y, err := strconv.Atoi(nameParts[len(nameParts)-1])
	if err != nil {
		return "", "", types.TF0, 0, false
	}

	return source, instrument, tf, y, true
}

func isMajorForexHolidayClosed(t time.Time) bool {
	month := t.Month()
	day := t.Day()
	h := t.Hour()

	// Full closures.
	if month == time.January && day == 1 {
		return true // New Year's Day
	}
	if month == time.December && day == 25 {
		return true // Christmas Day
	}

	// Practical early-close heuristics.
	if month == time.December && day == 24 && h >= 13 {
		return true // Christmas Eve afternoon
	}
	if month == time.December && day == 31 && h >= 13 {
		return true // New Year's Eve afternoon
	}

	return false
}
