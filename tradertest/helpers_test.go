package tradertest_test

import (
	"time"

	"github.com/rustyeddy/trader/clock"
	"github.com/rustyeddy/trader/id"
)

// testGenerator returns a deterministic *id.Generator the way any
// external consumer would build one: clock.NewSimulated plus
// id.NewDeterministic, both already public in their own packages. This
// is deliberately not something tradertest wraps — see the package doc
// comment.
func testGenerator() *id.Generator {
	return id.NewGenerator(clock.NewSimulated(time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)), id.NewDeterministic(1, 2))
}
