package id

import (
	"math/rand/v2"
	"sync"
)

// Deterministic is an EntropySource that draws entropy from a seeded PRNG,
// for tests and replay: given the same seed, it produces the same sequence of
// entropy values every time, so — combined with a clock.Simulated — a
// Generator using it produces the same sequence of identifiers every time,
// satisfying issue #24's "deterministic generation produces reproducible
// sequences" requirement.
//
// Deterministic must never be used in production: its entropy is
// reproducible by construction, which is exactly what production
// identifiers must not be.
type Deterministic struct {
	mu  sync.Mutex
	rng *rand.Rand
}

// NewDeterministic returns a Deterministic source seeded with seed1 and
// seed2 — the same two-value seed shape math/rand/v2's PCG source itself
// takes, exposed directly rather than derived from a single value, so the
// seed a caller passes is exactly the seed that determines the sequence.
func NewDeterministic(seed1, seed2 uint64) *Deterministic {
	return &Deterministic{rng: rand.New(rand.NewPCG(seed1, seed2))}
}

// Entropy implements EntropySource.
func (d *Deterministic) Entropy() ([10]byte, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	var b [10]byte
	for i := range b {
		b[i] = byte(d.rng.IntN(256))
	}
	return b, nil
}
