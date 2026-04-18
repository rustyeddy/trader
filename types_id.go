package trader

import (
	cryptoRand "crypto/rand"
	"encoding/binary"
	"io"
	"math/rand"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
)

var (
	ulidMu sync.Mutex
	mono   io.Reader
)

func init() {
	// Seed a PRNG from crypto/rand so ULID entropy is unpredictable.
	// We use ulid.Monotonic so IDs generated within the same millisecond remain
	// lexicographically increasing.
	var seed int64
	binary.Read(cryptoRand.Reader, binary.LittleEndian, &seed)
	if seed == 0 {
		seed = time.Now().UnixNano()
	}

	mono = ulid.Monotonic(rand.New(rand.NewSource(seed)), 0)
}

// New returns a ULID string (time-sortable identifier).
//
// ULIDs are lexicographically sortable by generation time, which makes them
// ideal for journaling/trading records and SQLite indexes.
func NewULID() string {
	ulidMu.Lock()
	defer ulidMu.Unlock()

	id, err := ulid.New(ulid.Timestamp(time.Now().UTC()), mono)
	if err != nil {
		// Errors are extremely unlikely unless time goes backwards or entropy fails.
		panic(err)
	}
	return id.String()
}
