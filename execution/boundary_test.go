package execution

import (
	"go/parser"
	"go/token"
	"os"
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
func TestExecutionNeverImportsBrokerOrRisk(t *testing.T) {
	forbidden := []string{
		`"github.com/rustyeddy/trader/broker"`,
		`"github.com/rustyeddy/trader/risk"`,
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
			for _, bad := range forbidden {
				require.NotEqual(t, bad, imp.Path.Value,
					"%s must not import %s (ADR-006/package-boundaries.org)", name, bad)
			}
		}
	}
}
