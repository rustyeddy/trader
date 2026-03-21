package data

import (
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
	items Keymap[Asset]
}

func NewInventory() *Inventory {
	return &Inventory{
		items: NewKeymap[Asset](),
	}
}

func (inv *Inventory) Put(a Asset) {
	inv.items.Put(a.Key, a)
}

func (inv *Inventory) Get(key Key) (Asset, bool) {
	return inv.items.Get(key)
}

func (inv *Inventory) Has(key Key) bool {
	return inv.items.Has(key)
}

func (inv *Inventory) Delete(key Key) {
	inv.items.Delete(key)
}

func (inv *Inventory) Keys() []Key {
	return inv.items.Keys()
}

func (inv *Inventory) List() []Asset {
	return inv.items.List()
}

func (inv *Inventory) Len() int {
	return len(inv.items.m)
}

func (inv *Inventory) Update(key Key, fn func(*Asset) error) error {
	return inv.items.Update(key, fn)
}

func (inv *Inventory) HasComplete(key Key) bool {
	a, ok := inv.items.Get(key)
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

func (inv *Inventory) TicksComplete(k Key) (bool, []Key) {
	var keys []Key
	for day := 1; day <= types.DaysInMonth(k.Year, k.Month-1); day++ {
		for hour := 0; hour < 24; hour++ {
			t := time.Date(k.Year, time.Month(k.Month), day, hour, 0, 0, 0, time.UTC)
			if types.IsForexMarketClosed(t) {
				continue
			}

			key := k
			key.Source = "dukascopy"
			key.Kind = KindTick
			key.TF = types.Ticks
			key.Day = day
			key.Hour = hour
			asset, ok := inv.Get(key)
			if !ok || !asset.Exists || !asset.Complete || asset.Size <= 0 {
				return false, nil
			}
			keys = append(keys, key)
		}
	}
	return true, keys
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
