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

// TestServiceBacktestNeverImportsAdaptersOrCmd is ADR-022/issue #221's
// own guard: service/backtest never imports a concrete broker adapter
// or cmd — the whole point of the injected EnvironmentFactory is that
// Service's own dependency graph stays adapter-free (issue #221
// review: "the important invariant is simply service/backtest never
// imports the concrete adapter"). Mirrors backtest/boundary_test.go
// and report/boundary_test.go's identical parse-each-file-import
// approach. service is deliberately not in this list: importing the
// service umbrella package itself would be an import cycle, not an
// architecture violation, so there is nothing to test for it.
func TestServiceBacktestNeverImportsAdaptersOrCmd(t *testing.T) {
	forbiddenRoots := []string{
		"github.com/rustyeddy/trader/adapters",
		"github.com/rustyeddy/trader/cmd",
	}

	entries, err := os.ReadDir(".")
	require.NoError(t, err)

	fset := token.NewFileSet()
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") {
			continue
		}
		if strings.HasSuffix(name, "_test.go") {
			continue
		}

		f, err := parser.ParseFile(fset, name, nil, parser.ImportsOnly)
		require.NoError(t, err, name)

		for _, imp := range f.Imports {
			path, err := strconv.Unquote(imp.Path.Value)
			require.NoError(t, err, name)

			for _, root := range forbiddenRoots {
				require.False(t, isForbiddenImport(path, root),
					"%s must not import %s or any of its subpackages (ADR-022, issue #221)", name, root)
			}
		}
	}
}

// isForbiddenImport reports whether path is root itself or a
// subpackage of root — exact-or-prefix-with-"/".
func isForbiddenImport(path, root string) bool {
	return path == root || strings.HasPrefix(path, root+"/")
}
