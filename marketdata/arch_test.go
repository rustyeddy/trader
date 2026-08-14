package marketdata

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// This file enforces, mechanically, one piece of the Manager boundary from
// issue #71 and ADR-020: package marketdata is the historical-data gateway,
// and it must not itself become a storage or provider implementation.
//
// It deliberately does NOT forbid marketdata from importing its own
// marketdata/internal/... subpackages. Manager is meant to own and wire those
// internals (a provider, a store), and Go's internal-package rule already
// prevents any package outside the marketdata/ subtree from importing them —
// that is the guarantee that matters. The internal implementations are kept
// independent of the root marketdata package (see
// marketdata/internal/provider/oanda, which uses its own RawInterval rather
// than marketdata.Interval), so Manager can import them without an import
// cycle.
//
// What remains worth asserting is only that the facade does not reach for raw
// storage-format or dropped-vendor libraries directly: that work belongs
// under marketdata/internal, invoked through Manager. The check scans this
// package's own non-test .go files and matches these substrings against each
// import path.
var forbiddenImportSubstrings = []string{
	"encoding/csv",
	"parquet",
	"dukascopy",
}

func TestMarketdataDoesNotImportImplementationPackages(t *testing.T) {
	dir := packageDir(t)

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)

	fset := token.NewFileSet()
	var violations []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		path := filepath.Join(dir, name)
		file, perr := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		require.NoError(t, perr)

		for _, imp := range file.Imports {
			p, uerr := strconv.Unquote(imp.Path.Value)
			require.NoError(t, uerr)
			for _, bad := range forbiddenImportSubstrings {
				if strings.Contains(p, bad) {
					violations = append(violations, name+" imports "+p)
				}
			}
		}
	}

	require.Empty(t, violations,
		"package marketdata must not import provider/storage implementation packages; "+
			"those depend on marketdata, not the reverse:\n%s", strings.Join(violations, "\n"))
}

// packageDir returns the directory of this test's package by walking up to
// the go.mod and appending marketdata. It avoids depending on the process
// working directory being the package directory.
func packageDir(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	require.NoError(t, err)
	// go test runs with the working directory set to the package under
	// test, so the current directory is already package marketdata.
	require.FileExists(t, filepath.Join(dir, "manager.go"))
	return dir
}
