package account

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func importsOf(t *testing.T, name string) []string {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), name, nil, parser.ImportsOnly)
	require.NoError(t, err, name)
	var out []string
	for _, imp := range f.Imports {
		p, err := strconv.Unquote(imp.Path.Value)
		require.NoError(t, err, name)
		out = append(out, p)
	}
	return out
}

func goFiles(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(".")
	require.NoError(t, err)
	var out []string
	for _, e := range entries {
		if n := e.Name(); !e.IsDir() && strings.HasSuffix(n, ".go") && !strings.HasSuffix(n, "_test.go") {
			out = append(out, n)
		}
	}
	return out
}

// No file here reaches a provider or storage adapter, and provider names are
// not this package's business: the registry in cmd/internal/accountactivitycfg
// is the only place they appear.
func TestNoFileImportsAnAdapter(t *testing.T) {
	for _, name := range goFiles(t) {
		for _, p := range importsOf(t, name) {
			require.NotContains(t, p, "internal/adapters", "%s imports %s: adapters belong behind the cfg and service packages", name, p)
		}
	}
}

// The command handlers are transports. They call the services and a
// Formatter; reconstruction and execution-quality arithmetic never happen in
// Cobra code. Formatters may import those packages only to render their types.
func TestHandlersNeverReconstructOrMeasure(t *testing.T) {
	handlers := []string{"command.go", "activity.go", "trades.go", "args.go"}
	forbidden := []string{
		"github.com/rustyeddy/trader/internal/tradehistory",
		"github.com/rustyeddy/trader/internal/executionquality",
	}
	for _, name := range handlers {
		for _, p := range importsOf(t, name) {
			for _, f := range forbidden {
				require.NotEqual(t, f, p, "%s must call internal/service/accountreport, not %s", name, p)
			}
		}
	}
}

// Handlers name no provider in code. Help text may say which token variable a
// user must set, but no identifier or import refers to a provider.
func TestNoProviderIdentifiersInHandlers(t *testing.T) {
	for _, name := range []string{"command.go", "activity.go", "trades.go", "args.go"} {
		f, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		require.NoError(t, err, name)
		ast.Inspect(f, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok {
				low := strings.ToLower(id.Name)
				require.False(t, strings.Contains(low, "oanda") || strings.Contains(low, "alpaca"),
					"%s: identifier %s names a provider; provider names live in accountactivitycfg", name, id.Name)
			}
			return true
		})
	}
}
