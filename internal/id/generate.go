package id

import (
	"crypto/rand"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/rustyeddy/trader/internal/id/internal/ulid"
)

// EntropySource supplies the 80-bit entropy component of a new identifier.
type EntropySource interface {
	// Entropy returns 10 fresh random bytes.
	Entropy() ([10]byte, error)
}

// Random is an EntropySource backed by crypto/rand: the production
// default. Its zero value is ready to use.
type Random struct{}

// Entropy implements EntropySource.
func (Random) Entropy() ([10]byte, error) {
	var b [10]byte
	if _, err := rand.Read(b[:]); err != nil {
		return [10]byte{}, fmt.Errorf("id: reading entropy: %w", err)
	}
	return b, nil
}

// ErrEntropyExhausted reports that a Generator made 2^80 IDs within one
// millisecond and cannot make another that still sorts after them.
var ErrEntropyExhausted = errors.New("id: entropy exhausted within one millisecond")

// Generator makes RunIDs from a clock and an entropy source, and never reads the
// time itself. IDs made within the same millisecond get strictly increasing entropy
// rather than fresh random values (the ULID spec's monotonic generation), so their
// text sorts in the order they were made.
//
// If the clock steps backwards (an NTP correction, say) the Generator carries on
// from the last millisecond it used instead of failing: a run must always be able
// to get an ID, and uniqueness and order matter more than the ID's time being exact
// during an adjustment.
//
// A *Generator is safe for concurrent use: uniqueness and order hold across
// goroutines sharing one.
type Generator struct {
	now    func() time.Time
	source EntropySource

	mu       sync.Mutex
	hasLast  bool
	lastMS   int64
	lastRand [10]byte
}

// NewGenerator returns a Generator that stamps IDs with now (pass clock.Clock in
// production, a fixed function in a test) and draws entropy from source.
func NewGenerator(now func() time.Time, source EntropySource) *Generator {
	return &Generator{now: now, source: source}
}

// RunID makes a new RunID.
//
// It returns ErrEntropyExhausted if it has already made 2^80 IDs in the current
// millisecond; it never falls back to weaker entropy silently.
func (g *Generator) RunID() (RunID, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	ms := g.now().UnixMilli()
	if g.hasLast && ms < g.lastMS {
		ms = g.lastMS // the clock went back: continue from the last millisecond
	}

	var rnd [10]byte
	switch {
	case !g.hasLast || ms > g.lastMS:
		r, err := g.source.Entropy()
		if err != nil {
			return RunID{}, err
		}
		rnd = r
	default: // the same millisecond, or the clock went back to it
		next, overflowed := ulid.IncrementEntropy(g.lastRand)
		if overflowed {
			return RunID{}, ErrEntropyExhausted
		}
		rnd = next
	}

	v, err := ulid.New(ms, rnd)
	if err != nil {
		return RunID{}, err
	}

	g.lastMS = ms
	g.lastRand = rnd
	g.hasLast = true

	return RunID{value: v, set: true}, nil
}
