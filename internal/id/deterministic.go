package id

import (
	"math/rand/v2"
	"sync"
)

// Deterministic is an EntropySource that draws from a seeded PRNG, for tests and
// replay: the same seed gives the same entropy every time, so with a fixed clock a
// Generator makes the same sequence of IDs every time.
//
// Never use it in production: its entropy is reproducible by construction, which
// is exactly what a production ID must not be.
type Deterministic struct {
	mu  sync.Mutex
	rng *rand.Rand
}

// NewDeterministic returns a Deterministic source seeded with seed1 and seed2, the
// two-value seed math/rand/v2's PCG source takes.
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
