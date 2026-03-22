package data

type Want struct {
	Key
	WantReason
}

type Wantlist struct {
	items Keymap[Want]
}

type WantReason string

const (
	WantMissing    WantReason = "missing"
	WantIncomplete WantReason = "incomplete"
	WantStale      WantReason = "stale"
)

func NewWantlist() *Wantlist {
	return &Wantlist{
		items: NewKeymap[Want](),
	}
}

func (wl *Wantlist) Put(w Want) {
	wl.items.Put(w.Key, w)
}

func (wl *Wantlist) Get(key Key) (Want, bool) {
	return wl.items.Get(key)
}

func (wl *Wantlist) Has(key Key) bool {
	return wl.items.Has(key)
}

func (wl *Wantlist) Delete(key Key) {
	wl.items.Delete(key)
}

func (wl *Wantlist) Keys() []Key {
	return wl.items.Keys()
}

func (wl *Wantlist) List() []Want {
	return wl.items.List()
}

func (wl *Wantlist) Len() int {
	return len(wl.items.m)
}

func (wl *Wantlist) Update(key Key, fn func(*Want) error) error {
	return wl.items.Update(key, fn)
}
