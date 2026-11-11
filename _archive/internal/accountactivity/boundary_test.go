package accountactivity

import (
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// The domain model may import the standard library and these Trader
// packages only. An allowlist, not a deny list, so a new provider SDK,
// journal or runtime dependency fails here rather than slipping in.
var allowedTraderImports = map[string]bool{
	"github.com/rustyeddy/trader/num":              true,
	"github.com/rustyeddy/trader/instrument":       true,
	"github.com/rustyeddy/trader/order":            true,
	"github.com/rustyeddy/trader/internal/account": true,
}

func TestDomainImportsOnlyStdlibAndAllowedTraderPackages(t *testing.T) {
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
				continue // standard library
			}
			require.True(t, allowedTraderImports[path], "%s imports %s, which the account-activity domain may not depend on", name, path)
		}
	}
}
