package strategyv1

import (
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestPackageNeverImportsAnyTraderPackage is ADR-062's own dependency-
// direction decision made mechanical, mirroring strategy/
// boundary_test.go's per-file-import-parsing approach exactly: this
// package "depends only on generated Protobuf runtime support and the
// standard library — it does not import strategy, order, or any
// other domain package, since wire types are deliberately not
// aliases of domain types." Unlike strategy/boundary_test.go's own
// finite forbidden-root list, this package's rule is unconditional —
// no github.com/rustyeddy/trader/... package at all, domain or
// otherwise, so a single root ("github.com/rustyeddy/trader" itself)
// suffices; adding a second domain package import here would be just
// as much a violation as importing broker or execution would be for
// strategy.
//
// Generated files (strategy.pb.go, strategy_grpc.pb.go) are covered by
// this scan like any other non-test .go file in the package — the
// point is that this package's own dependency graph, generated code
// included, never reaches back into this module.
func TestPackageNeverImportsAnyTraderPackage(t *testing.T) {
	const forbiddenRoot = "github.com/rustyeddy/trader"

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

			require.False(t, isForbiddenTraderImport(path, forbiddenRoot),
				"%s must not import %s or any of its subpackages (ADR-062: this package depends only on the Protobuf/gRPC runtime and the standard library)", name, path)
		}
	}
}

// isForbiddenTraderImport reports whether path is root itself or a
// subpackage of root — exact-or-prefix-with-"/", mirroring strategy/
// boundary_test.go's own isForbiddenImport exactly.
func isForbiddenTraderImport(path, root string) bool {
	return path == root || strings.HasPrefix(path, root+"/")
}

func TestIsForbiddenTraderImport(t *testing.T) {
	const root = "github.com/rustyeddy/trader"

	tests := []struct {
		path string
		want bool
	}{
		{"github.com/rustyeddy/trader", true},
		{"github.com/rustyeddy/trader/order", true},
		{"github.com/rustyeddy/trader/protocol/strategy/v2", true},
		{"google.golang.org/protobuf/proto", false},
		{"google.golang.org/grpc", false},
	}
	for _, tt := range tests {
		require.Equal(t, tt.want, isForbiddenTraderImport(tt.path, root), tt.path)
	}
}
