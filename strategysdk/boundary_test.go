package strategysdk

import (
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestStrategySDKNeverImportsForbiddenPackages mirrors
// strategy/boundary_test.go's own per-file import-parsing approach
// and its exact forbidden set (ADR-062's own "Dependency direction"
// section: "strategysdk depends on order, marketdata, instrument, and
// num ... never on strategy itself, and never on backtest, service,
// cmd, adapters, broker, execution, risk, pipeline, or chart ... since
// a guest process has exactly the same restrictions an in-process
// strategy does"). It parses every non-test .go file's own import
// block directly, per-file, rather than a package-level `go list`
// that would merge every file's imports together.
func TestStrategySDKNeverImportsForbiddenPackages(t *testing.T) {
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
		"github.com/rustyeddy/trader/chart",
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
					"%s must not import %s or any of its subpackages (ADR-062, issue #381)", name, root)
			}
		}
	}
}

// isForbiddenImport reports whether path is root itself or a
// subpackage of root — exact-or-prefix-with-"/".
func isForbiddenImport(path, root string) bool {
	return path == root || strings.HasPrefix(path, root+"/")
}
