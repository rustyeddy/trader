package quantitystudy

import (
	"os"
	"testing"

	ns "github.com/rustyeddy/trader/test/internal/numericstudy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNoFloatingPoint holds this study to the same rule as numericstudy: it
// concludes that scaled int64 is the right representation, so it must not
// depend on binary floating point to reach that conclusion — including in its
// reporting and margin arithmetic.
func TestNoFloatingPoint(t *testing.T) {
	dir, err := os.Getwd()
	require.NoError(t, err)

	found, err := ns.FindFloatUsage(dir)
	require.NoError(t, err)

	for _, u := range found {
		assert.Fail(t, "binary floating point in study source",
			"%s at %s", u.What, u.Position)
	}
}
