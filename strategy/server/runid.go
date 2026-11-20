package server

import (
	"crypto/rand"
	"errors"
	"fmt"
	"sync"
	"time"
)

// ErrRunIDExhausted reports that 2^80 RunIDs were issued within one millisecond.
var ErrRunIDExhausted = errors.New("run id: entropy exhausted within one millisecond")

// crockford is the ULID alphabet (Crockford Base32 without I, L, O and U).
const crockford = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// RunIDLen is the length of a RunID: a ULID in its 26-character text form.
const RunIDLen = 26

// RunIDGenerator issues ULIDs (https://github.com/ulid/spec): a 48-bit
// millisecond timestamp and 80 bits of entropy, so RunIDs sort by creation time
// and need no central counter. It is monotonic: IDs issued within one
// millisecond, or after the clock steps back, increment the previous entropy, so
// they still sort in issue order and never repeat. It is safe for concurrent use.
type RunIDGenerator struct {
	// Now and Entropy are injectable for tests; nil means time.Now and crypto/rand.
	Now     func() time.Time
	Entropy func([]byte) error

	mu   sync.Mutex
	ms   int64
	rand [10]byte
	have bool
}

// NewRunID issues the next RunID. Its signature is Config.NewRunID.
func (g *RunIDGenerator) NewRunID() (string, error) {
	now := time.Now
	if g.Now != nil {
		now = g.Now
	}
	ms := now().UnixMilli()
	if ms < 0 || ms >= 1<<48 {
		return "", fmt.Errorf("run id: time %d ms is outside the 48-bit ULID range", ms)
	}

	g.mu.Lock()
	defer g.mu.Unlock()
	if g.have && ms <= g.ms {
		// Same millisecond, or the clock went back: keep the old timestamp (g.ms) and
		// count up so the order and uniqueness hold.
		overflow := true
		for i := len(g.rand) - 1; i >= 0; i-- {
			g.rand[i]++
			if g.rand[i] != 0 {
				overflow = false
				break
			}
		}
		if overflow {
			return "", ErrRunIDExhausted
		}
	} else {
		fill := g.Entropy
		if fill == nil {
			fill = func(b []byte) error { _, err := rand.Read(b); return err }
		}
		var e [10]byte
		if err := fill(e[:]); err != nil {
			return "", fmt.Errorf("run id: %w", err)
		}
		g.rand, g.ms, g.have = e, ms, true
	}
	return encodeULID(g.ms, g.rand), nil
}

// encodeULID writes the 128 bits (timestamp then entropy) as 26 Crockford digits,
// most significant first; the first digit carries only the top 3 bits.
func encodeULID(ms int64, e [10]byte) string {
	var v [16]byte
	for i := 0; i < 6; i++ {
		v[i] = byte(ms >> (8 * (5 - i)))
	}
	copy(v[6:], e[:])
	var out [RunIDLen]byte
	// Walk the 130-bit (2 zero bits + 128) value five bits at a time.
	for i := 0; i < RunIDLen; i++ {
		var d byte
		for b := 0; b < 5; b++ {
			bit := i*5 + b - 2 // index into the 128 bits; negative for the 2 padding bits
			d <<= 1
			if bit >= 0 {
				d |= (v[bit/8] >> (7 - bit%8)) & 1
			}
		}
		out[i] = crockford[d]
	}
	return string(out[:])
}

// RandomRunID issues a RunID from a process-wide RunIDGenerator.
func RandomRunID() (string, error) { return defaultRunIDs.NewRunID() }

var defaultRunIDs RunIDGenerator
