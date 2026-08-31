package report

import (
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestReportNeverImportsOrchestrationOrAdapters is ADR-035/ADR-038's
// own guard: report is rendering-only over an already-computed
// BacktestReport. It must never import backtest's own orchestration
// dependencies (marketdata, broker, pipeline, strategy, risk,
// execution, journal adapters), concrete adapters, services, or cmd —
// only backtest itself (for the value types report projects) and
// shared value packages (account, order, num, id). Mirrors backtest/
// boundary_test.go's identical parse-each-file-import approach.
func TestReportNeverImportsOrchestrationOrAdapters(t *testing.T) {
	forbiddenRoots := []string{
		"github.com/rustyeddy/trader/adapters",
		"github.com/rustyeddy/trader/service",
		"github.com/rustyeddy/trader/cmd",
		"github.com/rustyeddy/trader/pipeline",
		"github.com/rustyeddy/trader/broker",
		"github.com/rustyeddy/trader/marketdata",
		"github.com/rustyeddy/trader/strategy",
		"github.com/rustyeddy/trader/risk",
		"github.com/rustyeddy/trader/execution",
		"github.com/rustyeddy/trader/journal",
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
					"%s must not import %s or any of its subpackages (ADR-035, ADR-038, package-boundaries.org)", name, root)
			}
		}
	}
}

// isForbiddenImport reports whether path is root itself or a
// subpackage of root — exact-or-prefix-with-"/".
func isForbiddenImport(path, root string) bool {
	return path == root || strings.HasPrefix(path, root+"/")
}
