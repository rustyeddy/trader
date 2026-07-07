package marketdata

import (
	"strings"
	"time"

	"github.com/rustyeddy/trader/market"
)

type DataKind uint8

const (
	KindUnknown DataKind = iota
	KindTick
	KindCandle
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
	Range      market.TimeRange
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
	return inv.items.Len()
}

func (inv *Inventory) Update(key Key, fn func(*Asset) error) error {
	return inv.items.Update(key, fn)
}

func (inv *Inventory) HasComplete(key Key) bool {
	a, ok := inv.items.Get(key)
	return assetExistsAndComplete(a, ok)
}

func (inv *Inventory) WantReasonFor(key Key) (WantReason, bool) {
	a, ok := inv.items.Get(key)
	return assetWantReason(a, ok)
}

func (inv *Inventory) MissingComplete(keys []Key) []Key {
	out := make([]Key, 0)
	for _, k := range keys {
		a, ok := inv.items.Get(k)
		if !assetExistsAndComplete(a, ok) {
			out = append(out, k)
		}
	}
	return out
}

func (inv *Inventory) TicksComplete(k Key) (complete bool, required []Key, missing []Key, err error) {
	required, err = RequiredTickHoursForMonth(market.SourceDukascopy, k.Instrument, k.Year, k.Month)
	if err != nil {
		return false, nil, nil, err
	}
	missing = make([]Key, 0)
	for _, key := range required {
		asset, ok := inv.Get(key)
		if !assetUsableTickFile(asset, ok) {
			missing = append(missing, key)
		}
	}
	return len(missing) == 0, required, missing, nil
}

func assetWantReason(a Asset, ok bool) (WantReason, bool) {
	if !ok || !a.Exists {
		return WantMissing, true
	}
	if !a.Complete {
		return WantIncomplete, true
	}
	return "", false
}

func assetExistsAndComplete(a Asset, ok bool) bool {
	return ok && a.Exists && a.Complete
}

func assetUsableTickFile(a Asset, ok bool) bool {
	return assetExistsAndComplete(a, ok) && a.Size > 0
}

type BuildStatus int

const (
	BuildUnknown BuildStatus = iota
	BuildReady
	BuildBlocked
	BuildExistsComplete
)

type BuildDecision struct {
	Key
	Status   BuildStatus
	Required []Key
	Missing  []Key
	Reason   string
}
