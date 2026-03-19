package data

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/types"
)

type DataKind uint8

const (
	KindUnknown DataKind = iota
	KindTick
	KindCandle
)

const (
	SourceDukascopy = "dukascopy"
	SourceCandles   = "candles"
)

type AssetFlags uint32

const (
	FlagUsable AssetFlags = 1 << iota
	FlagKnownClosed
	FlagDoNotDownload
	FlagDownloadFailed
	FlagManualSkip
)

type Asset struct {
	Key        Key
	Path       string
	Range      types.TimeRange
	Exists     bool
	Complete   bool
	Buildable  bool
	Size       int64
	UpdatedAt  time.Time
	SourceAge  time.Time // optional: mtime of prerequisite/source
	Descriptor string
	Flags      AssetFlags

	MissingInputs int
	Reason        string
}

var (
	inv *Inventory
)

func normalizeSource(s string) string {
	return strings.TrimSpace(strings.ToLower(s))
}

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

type Inventory struct {
	assets map[Key]Asset
	mu     sync.RWMutex
}

func NewInventory() *Inventory {
	return &Inventory{
		assets: make(map[Key]Asset),
	}
}

func (inv *Inventory) Put(a Asset) {
	inv.mu.Lock()
	defer inv.mu.Unlock()

	if inv.assets == nil {
		inv.assets = make(map[Key]Asset)
	}
	inv.assets[a.Key] = a
}

func (inv *Inventory) Get(key Key) (Asset, bool) {
	inv.mu.RLock()
	defer inv.mu.RUnlock()

	a, ok := inv.assets[key]
	return a, ok
}

func (inv *Inventory) Update(key Key, fn func(*Asset) error) error {
	inv.mu.Lock()
	defer inv.mu.Unlock()

	a, ok := inv.assets[key]
	if !ok {
		return fmt.Errorf("asset not found: %v", key)
	}
	if err := fn(&a); err != nil {
		return err
	}
	inv.assets[key] = a
	return nil
}

func (inv *Inventory) Delete(key Key) {
	delete(inv.assets, key)
}

func (inv *Inventory) List() ([]Key, []Asset) {
	n := len(inv.assets)
	keys := make([]Key, n)
	vals := make([]Asset, n)

	var i int
	for k, v := range inv.assets {
		keys[i], vals[i] = k, v
		i++
	}

	return keys, vals
}

func (inv *Inventory) Keys() []Key {
	keys, _ := inv.List()
	return keys
}

func (inv *Inventory) Assets() []Asset {
	_, assets := inv.List()
	return assets
}

func (inv *Inventory) MonthKeysFor(instrument string, kind DataKind, tf types.Timeframe) (keys []Key) {
	return keys
}

func (inv *Inventory) AssetsForMonth(key []Key) (assets []Asset) {

	return assets
}

func (inv *Inventory) HasComplete(key Key) bool {
	a, ok := inv.assets[key]
	return ok && a.Exists && a.Complete
}

func (inv *Inventory) MissingComplete(keys []Key) []Key {
	out := make([]Key, 0)
	for _, k := range keys {
		if !inv.HasComplete(k) {
			out = append(out, k)
		}
	}
	return out
}

func (inv *Inventory) Has(key Key) bool {
	if key.Kind == KindTick {
		key.TF = types.TF0
	}

	_, ok := inv.assets[key]
	return ok
}

func (inv *Inventory) Years(source, instrument string, kind DataKind, tf types.Timeframe) []int {
	source = normalizeSource(source)
	instrument = market.NormalizeInstrument(instrument)

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

	child, ok := inv.Get(Key{
		Source:     normalizeSource(source),
		Instrument: market.NormalizeInstrument(instrument),
		Kind:       KindCandle,
		TF:         tf,
		Year:       year,
	})
	if !ok {
		return false, fmt.Errorf("missing child asset")
	}

	parent, ok := inv.Get(Key{
		Source:     normalizeSource(source),
		Instrument: market.NormalizeInstrument(instrument),
		Kind:       KindCandle,
		TF:         parentTF,
		Year:       year,
	})
	if !ok {
		return false, fmt.Errorf("missing parent asset")
	}
	return parent.UpdatedAt.After(child.UpdatedAt), nil
}

func (inv *Inventory) NeedsDownload(key Key) bool {
	a, ok := inv.assets[key]
	if !ok {
		return true
	}
	if !a.Exists || !a.Complete {
		return true
	}
	return false
}

type Key struct {
	Instrument string
	Source     string
	Kind       DataKind
	TF         types.Timeframe
	Year       int
	Month      int
	Day        int
	Hour       int
}

func (k Key) Path() string {
	return store.PathForAsset(k)
}

// compare returns:
//
//	-1 if ak < k
//	 0 if ak == k
//	 1 if ak > k
func (ak Key) compare(k Key) int {
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

	return 0
}

func (ak Key) before(k Key) bool {
	return ak.compare(k) < 0
}

func (ak Key) after(k Key) bool {
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
func (ak Key) Time() time.Time {
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

func (k Key) IsMonthlyCandle() bool {
	return k.Kind == KindCandle && k.Day == 0 && k.Hour == 0
}

func (k Key) IsHourlyTick() bool {
	return k.Kind == KindTick && k.Day > 0 && k.Hour >= 0
}
func RequiredTickHoursForMonth(source, instrument string, year, month int) []Key {
	start := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, 0)

	out := make([]Key, 0, 24*31)

	for t := start; t.Before(end); t = t.Add(time.Hour) {
		if IsForexMarketClosed(t) {
			continue
		}

		out = append(out, Key{
			Source:     source,
			Instrument: market.NormalizeInstrument(instrument),
			Kind:       KindTick,
			Year:       t.Year(),
			Month:      int(t.Month()),
			Day:        t.Day(),
			Hour:       t.Hour(),
		})
	}

	return out
}

type BuildStatus int

const (
	BuildUnknown BuildStatus = iota
	BuildReady
	BuildBlocked
	BuildExistsComplete
)

type BuildDecision struct {
	Target   Key
	Status   BuildStatus
	Required []Key
	Missing  []Key
	Reason   string
}

func AssessM1Month(inv *Inventory, tickSource, candleSource, instrument string, year, month int) BuildDecision {
	target := Key{
		Source:     candleSource,
		Instrument: market.NormalizeInstrument(instrument),
		Kind:       KindCandle,
		TF:         types.M1,
		Year:       year,
		Month:      month,
	}

	if inv.HasComplete(target) {
		return BuildDecision{
			Target: target,
			Status: BuildExistsComplete,
			Reason: "M1 month already complete",
		}
	}

	required := RequiredTickHoursForMonth(tickSource, instrument, year, month)
	missing := inv.MissingComplete(required)

	if len(missing) > 0 {
		return BuildDecision{
			Target:   target,
			Status:   BuildBlocked,
			Required: required,
			Missing:  missing,
			Reason:   "missing required tick hours",
		}
	}

	return BuildDecision{
		Target:   target,
		Status:   BuildReady,
		Required: required,
		Reason:   "all required tick hours available",
	}
}

func AssessH1Month(inv *Inventory, candleSource, instrument string, year, month int) BuildDecision {
	target := Key{
		Source:     candleSource,
		Instrument: market.NormalizeInstrument(instrument),
		Kind:       KindCandle,
		TF:         types.H1,
		Year:       year,
		Month:      month,
	}

	if inv.HasComplete(target) {
		return BuildDecision{
			Target: target,
			Status: BuildExistsComplete,
			Reason: "H1 month already complete",
		}
	}

	req := Key{
		Source:     candleSource,
		Instrument: market.NormalizeInstrument(instrument),
		Kind:       KindCandle,
		TF:         types.M1,
		Year:       year,
		Month:      month,
	}

	if !inv.HasComplete(req) {
		return BuildDecision{
			Target:  target,
			Status:  BuildBlocked,
			Missing: []Key{req},
			Reason:  "missing complete M1 month",
		}
	}

	return BuildDecision{
		Target:   target,
		Status:   BuildReady,
		Required: []Key{req},
		Reason:   "complete M1 month available",
	}
}

var newYorkLoc = func() *time.Location {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		return time.UTC
	}
	return loc
}()

func IsForexMarketClosed(t time.Time) bool {
	nt := t.In(newYorkLoc)
	wd := nt.Weekday()
	h := nt.Hour()

	switch wd {
	case time.Saturday:
		return true
	case time.Sunday:
		return h < 17
	case time.Friday:
		return h >= 17
	default:
		return isMajorForexHolidayClosed(nt)
	}
}

func isMajorForexHolidayClosed(t time.Time) bool {
	month := t.Month()
	day := t.Day()
	h := t.Hour()

	if month == time.January && day == 1 {
		return true
	}
	if month == time.December && day == 25 {
		return true
	}
	if month == time.December && day == 24 && h >= 13 {
		return true
	}
	if month == time.December && day == 31 && h >= 13 {
		return true
	}

	return false
}
