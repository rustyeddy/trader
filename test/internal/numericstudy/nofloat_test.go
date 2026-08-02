package numericstudy

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNoFloatingPoint enforces the study's central claim: this package reaches
// its conclusions without binary floating point at any point, including the
// margin and headroom reporting.
//
// An earlier revision computed margins in float64, which made the README's
// "no float64" claim false in exactly the place a reader would care about — a
// study arguing against binary floating point should not depend on it to
// produce its evidence.  This test keeps that from coming back.
func TestNoFloatingPoint(t *testing.T) {
	dir, err := os.Getwd()
	require.NoError(t, err)

	found, err := FindFloatUsage(dir)
	require.NoError(t, err)

	for _, u := range found {
		assert.Fail(t, "binary floating point in study source",
			"%s at %s", u.What, u.Position)
	}
}
