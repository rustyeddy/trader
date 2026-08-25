package risk

import (
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestRiskNeverImportsBrokerOrExecution is issue #180 (M4-05)'s own
// architectural guard, mirroring execution/boundary_test.go's
// TestExecutionNeverImportsBrokerOrRisk: risk must never import broker
// (ADR-006 assigns broker submission to a later stage) or execution
// (execution and risk are siblings, package-boundaries.org, neither
// depends on the other). It parses every non-test .go file's own
// import block directly, per-file, rather than a package-level
// `go list` that would merge every file's imports together.
//
// A forbidden root is matched exact-or-prefix-with-"/", not by exact
// string equality, so a future subpackage import (e.g.
// "github.com/rustyeddy/trader/broker/foo") is caught too — the same
// fix review feedback on PR #194 applied to execution's own guard.
func TestRiskNeverImportsBrokerOrExecution(t *testing.T) {
	forbiddenRoots := []string{
		"github.com/rustyeddy/trader/broker",
		"github.com/rustyeddy/trader/execution",
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
