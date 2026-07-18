package idgen

import (
	cryptoRand "crypto/rand"
	"encoding/binary"
	"io"
	"math/rand"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
)

const shortDisplayIDLen = 8

type ulidGenerator struct {
	mu      sync.Mutex
	entropy io.Reader
}

var defaultULIDGenerator = newULIDGenerator()

func newULIDGenerator() *ulidGenerator {
	// Seed a PRNG from crypto/rand so ULID entropy is unpredictable.
	// We use ulid.Monotonic so IDs generated within the same millisecond remain
	// lexicographically increasing.
	var seed int64
	if err := binary.Read(cryptoRand.Reader, binary.LittleEndian, &seed); err != nil || seed == 0 {
		seed = time.Now().UnixNano()
	}

	return &ulidGenerator{
		entropy: ulid.Monotonic(rand.New(rand.NewSource(seed)), 0),
	}
}

func (g *ulidGenerator) New() string {
	g.mu.Lock()
	defer g.mu.Unlock()

	id, err := ulid.New(ulid.Timestamp(time.Now().UTC()), g.entropy)
	if err != nil {
		// If the monotonic entropy source cannot produce the next value in the
		// current millisecond, fall back to a valid ULID that may no longer
		// preserve the same monotonic ordering guarantee.
		id = ulid.Make()
	}
	return id.String()
}

// NewULID returns a ULID string (time-sortable identifier).
//
// ULIDs are lexicographically sortable by generation time, which makes them
// ideal for journaling/trading records and database indexes.
func NewULID() string {
	return defaultULIDGenerator.New()
}

// ShortDisplayID returns a short, human-friendly prefix for headings and logs.
func ShortDisplayID(full string) string {
	if len(full) <= shortDisplayIDLen {
		return full
	}
	return full[:shortDisplayIDLen]
}
