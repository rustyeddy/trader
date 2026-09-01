package m5

import (
	"go/parser"
	"go/token"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// forbiddenRoots are the runtime/application/transport/adapter
// packages a representative external strategy implementation must
// never need (issue #225, M5-17 review, point 7): the whole point of
// this package is that PrivateStrategy compiles using nothing but the
// public strategy.Strategy contract, the same way a real private
// strategy repository would.
var forbiddenRoots = []string{
	"github.com/rustyeddy/trader/backtest",
	"github.com/rustyeddy/trader/service",
	"github.com/rustyeddy/trader/cmd",
	"github.com/rustyeddy/trader/adapters",
	"github.com/rustyeddy/trader/broker",
	"github.com/rustyeddy/trader/execution",
	"github.com/rustyeddy/trader/risk",
	"github.com/rustyeddy/trader/pipeline",
}

// TestPrivateStrategyNeverImportsRuntimeOrTransportPackages parses
// privatestrategy.go's own import block directly (never
// privatestrategy_test.go, which legitimately plays the role of a
// composition root and does import backtest/service/adapters/
// execution/risk/pipeline to actually run PrivateStrategy — see its
// own doc comment) and fails if it names any forbidden root or
// subpackage. A forbidden root is matched exact-or-prefix-with-"/",
// matching every other Trader boundary test's own convention.
func TestPrivateStrategyNeverImportsRuntimeOrTransportPackages(t *testing.T) {
	const file = "privatestrategy.go"

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, file, nil, parser.ImportsOnly)
	require.NoError(t, err)

	for _, imp := range f.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		require.NoError(t, err)

		for _, root := range forbiddenRoots {
			require.False(t, isForbiddenImport(path, root),
				"%s must not import %s or any of its subpackages — a representative external strategy must compile using only the public strategy.Strategy contract (issue #225)", file, root)
		}
	}
}

// isForbiddenImport reports whether path is root itself or a
// subpackage of root — exact-or-prefix-with-"/".
func isForbiddenImport(path, root string) bool {
	return path == root || strings.HasPrefix(path, root+"/")
}
