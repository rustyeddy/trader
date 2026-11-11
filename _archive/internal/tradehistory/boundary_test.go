package tradehistory

import (
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// The derived view reads the ledger's domain types and nothing else: the
// standard library and these Trader packages. In particular it must not reach
// a store, an adapter, a provider package, the journal or the clock.
var allowedTraderImports = map[string]bool{
	"github.com/rustyeddy/trader/num":                      true,
	"github.com/rustyeddy/trader/instrument":               true,
	"github.com/rustyeddy/trader/order":                    true,
	"github.com/rustyeddy/trader/internal/accountactivity": true,
}

func TestImportsOnlyStdlibAndTheLedgerDomain(t *testing.T) {
	entries, err := os.ReadDir(".")
	require.NoError(t, err)
	fset := token.NewFileSet()
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, parser.ImportsOnly)
		require.NoError(t, err, name)
		for _, imp := range f.Imports {
			path, err := strconv.Unquote(imp.Path.Value)
			require.NoError(t, err, name)
			first, _, _ := strings.Cut(path, "/")
			if !strings.Contains(first, ".") {
				continue
			}
			require.True(t, allowedTraderImports[path], "%s imports %s, which trade reconstruction may not depend on", name, path)
		}
	}
}
