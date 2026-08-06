package id

import (
	"crypto/rand"
	"errors"
	"fmt"
	"sync"

	"github.com/rustyeddy/trader/clock"
	"github.com/rustyeddy/trader/id/internal/ulid"
)

// Source supplies the 80-bit entropy component of a new identifier.
type Source interface {
	// Entropy returns 10 fresh random bytes.
	Entropy() ([10]byte, error)
}

// Random is a Source backed by crypto/rand: the production default. Its
// zero value is ready to use.
type Random struct{}

// Entropy implements Source.
func (Random) Entropy() ([10]byte, error) {
	var b [10]byte
	if _, err := rand.Read(b[:]); err != nil {
		return [10]byte{}, fmt.Errorf("id: reading entropy: %w", err)
	}
	return b, nil
}

// ErrClockMovedBackward reports that a Generator observed its clock produce
// an earlier timestamp than one it had already used to generate an
// identifier. A Simulated clock cannot do this — ADR-015 has Advance
// reject a negative duration — but a Real clock can, briefly, after a wall-
// clock adjustment; Generate reports this rather than silently reusing or
// ignoring the earlier timestamp.
var ErrClockMovedBackward = errors.New("id: clock moved backward")

// ErrEntropyExhausted reports that a Generator produced 2^80 identifiers
// within a single millisecond and cannot produce another monotonically
// increasing one without either waiting for the next millisecond or
// drawing fresh, non-monotonic entropy — something Generate never does
// silently.
var ErrEntropyExhausted = errors.New("id: entropy exhausted within one millisecond")

// Generator produces monotonic Trader-owned identifiers from an injected
// clock.Clock and entropy Source, per ADR-015: it never calls time.Now
// itself. Multiple identifiers generated within the same millisecond
// receive strictly increasing entropy rather than fresh random values —
// the ULID spec's monotonic generation guidance — so their lexicographic
// sort order matches creation order at millisecond resolution.
//
// Generator owns its own synchronization; a *Generator is safe for
// concurrent use, and every generated identifier's uniqueness and
// ordering guarantees hold across goroutines sharing one Generator, not
// just within a single goroutine's calls to it.
type Generator struct {
	clock  clock.Clock
	source Source

	mu       sync.Mutex
	hasLast  bool
	lastMS   int64
	lastRand [10]byte
}

// NewGenerator returns a Generator that timestamps identifiers using c and
// draws entropy from source.
func NewGenerator(c clock.Clock, source Source) *Generator {
	return &Generator{clock: c, source: source}
}

// Generate produces a new identifier of kind K.
//
// Generate returns ErrEntropyExhausted if it has already produced 2^80
// identifiers within the current millisecond, and ErrClockMovedBackward if
// g's clock reports a time earlier than one it has already used. Neither
// falls back to weaker behavior silently.
func Generate[K Kind](g *Generator) (ID[K], error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	ms := g.clock.Now().UnixMilli()

	var rnd [10]byte
	switch {
	case !g.hasLast || ms > g.lastMS:
		r, err := g.source.Entropy()
		if err != nil {
			return ID[K]{}, err
		}
		rnd = r
	case ms == g.lastMS:
		next, overflowed := ulid.IncrementEntropy(g.lastRand)
		if overflowed {
			return ID[K]{}, ErrEntropyExhausted
		}
		rnd = next
	default:
		return ID[K]{}, ErrClockMovedBackward
	}

	v, err := ulid.New(ms, rnd)
	if err != nil {
		return ID[K]{}, err
	}

	g.lastMS = ms
	g.lastRand = rnd
	g.hasLast = true

	return ID[K]{value: v, set: true}, nil
}
