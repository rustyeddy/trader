package execution

import (
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestExecutionNeverImportsBrokerOrRisk is issue #179 (M4-04)'s own
// architectural guard, mirroring broker/boundary_test.go's
// TestAccountAndOrderDoNotImportBroker: execution must never import
// broker (ADR-006 assigns broker submission to a later stage) or risk
// (execution and risk are siblings, package-boundaries.org, neither
// depends on the other). It parses every non-test .go file's own
// import block directly, the same per-file approach the broker package
// guard uses, rather than a package-level `go list` that would merge
// every file's imports together.
//
// A forbidden root is matched exact-or-prefix-with-"/" (isForbiddenImport),
// not by exact string equality: an import of a subpackage such as
// "github.com/rustyeddy/trader/broker/foo" violates the same boundary
// as importing "broker" itself and must be caught too, per review
// feedback on PR #194.
func TestExecutionNeverImportsBrokerOrRisk(t *testing.T) {
	forbiddenRoots := []string{
		"github.com/rustyeddy/trader/broker",
		"github.com/rustyeddy/trader/risk",
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
					"%s must not import %s or any of its subpackages (ADR-006/package-boundaries.org)", name, root)
			}
		}
	}
}

// isForbiddenImport reports whether path is root itself or a
// subpackage of root — exact-or-prefix-with-"/" — so a boundary guard
// naming one package root also catches every subpackage under it,
// rather than only the exact import string.
func isForbiddenImport(path, root string) bool {
	return path == root || strings.HasPrefix(path, root+"/")
}

// TestIsForbiddenImport is a regression for review feedback on PR
// #194: proves the prefix-matching predicate itself actually catches a
// subpackage import (e.g. "broker/foo"), not merely that no current
// file in this package happens to violate the boundary — a test that
// only re-ran TestExecutionNeverImportsBrokerOrRisk against today's
// files would pass even if isForbiddenImport regressed back to exact
// string equality.
func TestIsForbiddenImport(t *testing.T) {
	const root = "github.com/rustyeddy/trader/broker"

	tests := []struct {
		path string
		want bool
	}{
		{"github.com/rustyeddy/trader/broker", true},
		{"github.com/rustyeddy/trader/broker/foo", true},
		{"github.com/rustyeddy/trader/broker/sub/deeper", true},
		{"github.com/rustyeddy/trader/order", false},
		{"github.com/rustyeddy/trader/brokerage", false}, // shares the prefix as a string, but is a different package entirely
	}
	for _, tt := range tests {
		require.Equal(t, tt.want, isForbiddenImport(tt.path, root), tt.path)
	}
}
