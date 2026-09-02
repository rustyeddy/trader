package emacross

import (
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestEmacrossNeverImportsRuntimeApplicationOrTransportPackages is
// issue #249 (EMA-04)'s own architectural guard, mirroring
// strategy/boundary_test.go's and indicator/boundary_test.go's
// identical pattern: a concrete strategy implementation must remain as
// broker/execution/risk/backtest-adapter neutral as the strategy
// contract it implements (ADR-005). It parses every non-test .go
// file's own import block directly, per file, rather than a
// package-level `go list` that would merge every file's imports
// together.
func TestEmacrossNeverImportsRuntimeApplicationOrTransportPackages(t *testing.T) {
	forbiddenRoots := []string{
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

func TestIsForbiddenImport(t *testing.T) {
	const root = "github.com/rustyeddy/trader/broker"

	tests := []struct {
		path string
		want bool
	}{
		{"github.com/rustyeddy/trader/broker", true},
		{"github.com/rustyeddy/trader/broker/foo", true},
		{"github.com/rustyeddy/trader/order", false},
		{"github.com/rustyeddy/trader/brokerage", false},
	}
	for _, tt := range tests {
		require.Equal(t, tt.want, isForbiddenImport(tt.path, root), tt.path)
	}
}
