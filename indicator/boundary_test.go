package indicator

import (
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestIndicatorNeverImportsRuntimeApplicationOrTransportPackages is
// issue #248 (EMA-03)'s own architectural guard, mirroring strategy/
// boundary_test.go's identical pattern: indicator must never import
// strategy, broker, execution, risk, pipeline, backtest, service, cmd,
// or adapters (architecture document: "Indicators calculate values;
// strategies interpret those values" — an indicator that could name a
// strategy or broker package could be coupled to trading decisions or
// execution, which is exactly what this package must never do). It
// parses every non-test .go file's own import block directly, per
// file, rather than a package-level `go list` that would merge every
// file's imports together.
func TestIndicatorNeverImportsRuntimeApplicationOrTransportPackages(t *testing.T) {
	forbiddenRoots := []string{
		"github.com/rustyeddy/trader/strategy",
		"github.com/rustyeddy/trader/broker",
		"github.com/rustyeddy/trader/execution",
		"github.com/rustyeddy/trader/risk",
		"github.com/rustyeddy/trader/pipeline",
		"github.com/rustyeddy/trader/backtest",
		"github.com/rustyeddy/trader/service",
		"github.com/rustyeddy/trader/cmd",
		"github.com/rustyeddy/trader/adapters",
	}

	entries, err := os.ReadDir(".")
	require.NoError(t, err)

	fset := token.NewFileSet()
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || len(name) < 3 || name[len(name)-3:] != ".go" {
			continue
		}
		if len(name) > 8 && name[len(name)-8:] == "_test.go" {
			continue
		}

		f, err := parser.ParseFile(fset, name, nil, parser.ImportsOnly)
		require.NoError(t, err, name)

		for _, imp := range f.Imports {
			path, err := strconv.Unquote(imp.Path.Value)
			require.NoError(t, err, name)

			for _, root := range forbiddenRoots {
				require.False(t, isForbiddenImport(path, root),
					"%s must not import %s or any of its subpackages", name, root)
			}
		}
	}
}

// isForbiddenImport reports whether path is root itself or a
// subpackage of root — exact-or-prefix-with-"/".
func isForbiddenImport(path, root string) bool {
	return path == root || strings.HasPrefix(path, root+"/")
}
