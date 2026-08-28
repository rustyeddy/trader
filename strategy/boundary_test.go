package strategy

import (
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestStrategyNeverImportsBrokerExecutionRiskOrPipeline is issue #210
// (M5-02)'s own architectural guard, mirroring execution/
// boundary_test.go's and risk/boundary_test.go's own: strategy must
// never import broker, execution, risk, or pipeline (ADR-005: a
// strategy that can name a broker package can be coupled to one; the
// same reasoning excludes execution/risk/pipeline, none of which a
// broker-neutral strategy contract should ever depend on). It parses
// every non-test .go file's own import block directly, per-file,
// rather than a package-level `go list` that would merge every file's
// imports together.
//
// A forbidden root is matched exact-or-prefix-with-"/", not by exact
// string equality, so a future subpackage import (e.g.
// "github.com/rustyeddy/trader/broker/foo" or
// "github.com/rustyeddy/trader/pipeline/internal/...") is caught too —
// the same fix review feedback on PR #194 established for execution's
// own guard, and #210's own review asked to carry forward here.
func TestStrategyNeverImportsBrokerExecutionRiskOrPipeline(t *testing.T) {
	forbiddenRoots := []string{
		"github.com/rustyeddy/trader/broker",
		"github.com/rustyeddy/trader/execution",
		"github.com/rustyeddy/trader/risk",
		"github.com/rustyeddy/trader/pipeline",
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
					"%s must not import %s or any of its subpackages (ADR-005/ADR-035, package-boundaries.org)", name, root)
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
