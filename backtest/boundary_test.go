package backtest

import (
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestBacktestNeverImportsConcreteAdapters is ADR-035's own guard: only
// the outermost composition root (cmd/trader/backtest) constructs
// concrete adapters such as adapters/broker/sim.Broker; backtest itself
// depends only on the broker.Broker port and other domain packages.
// Mirrors strategy/boundary_test.go's identical parse-each-file-import
// approach.
func TestBacktestNeverImportsConcreteAdapters(t *testing.T) {
	forbiddenRoots := []string{
		"github.com/rustyeddy/trader/adapters",
		"github.com/rustyeddy/trader/service",
		"github.com/rustyeddy/trader/cmd",
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
					"%s must not import %s or any of its subpackages (ADR-035, package-boundaries.org)", name, root)
			}
		}
	}
}

// isForbiddenImport reports whether path is root itself or a
// subpackage of root — exact-or-prefix-with-"/".
func isForbiddenImport(path, root string) bool {
	return path == root || strings.HasPrefix(path, root+"/")
}
