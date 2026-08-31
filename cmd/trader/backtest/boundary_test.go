package backtest

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// forbiddenOrchestrationCalls names backtest package identifiers this
// command group must never call directly (issue #222 review, point
// 7): "trader backtest" is a composition root over service/backtest,
// not a second backtest orchestrator. Constructing a concrete
// broker/pipeline stack (sim.Broker, execution.Planner, risk.Engine,
// pipeline.Pipeline) is legitimate composition-root work — the same
// thing every other cmd/trader command family's own service.go
// already does — so those imports are not restricted here; only
// calling backtest's own orchestration entry points is.
var forbiddenOrchestrationCalls = map[string]bool{
	"NewRunner":    true,
	"NewScheduler": true,
	"NewReplay":    true,
}

// TestCmdBacktestNeverCallsOrchestrationDirectly parses every non-test
// .go file in this package and fails if it finds a call expression of
// the shape <alias>.NewRunner(...)/<alias>.NewScheduler(...)/
// <alias>.NewReplay(...) where <alias> is bound to
// "github.com/rustyeddy/trader/backtest" in that file's own imports.
func TestCmdBacktestNeverCallsOrchestrationDirectly(t *testing.T) {
	entries, err := os.ReadDir(".")
	require.NoError(t, err)

	fset := token.NewFileSet()
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}

		f, err := parser.ParseFile(fset, name, nil, parser.ParseComments)
		require.NoError(t, err, name)

		backtestAlias := ""
		for _, imp := range f.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			if path != "github.com/rustyeddy/trader/backtest" {
				continue
			}
			if imp.Name != nil {
				backtestAlias = imp.Name.Name
			} else {
				backtestAlias = "backtest"
			}
		}
		if backtestAlias == "" {
			continue // this file doesn't import the domain backtest package at all
		}

		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkg, ok := sel.X.(*ast.Ident)
			if !ok || pkg.Name != backtestAlias {
				return true
			}
			require.False(t, forbiddenOrchestrationCalls[sel.Sel.Name],
				"%s must not call %s.%s directly — service/backtest.Service.Run is the only allowed entry point (issue #222)",
				name, backtestAlias, sel.Sel.Name)
			return true
		})
	}
}
