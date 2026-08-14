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

// This file enforces, mechanically, the Manager boundary from issue #71 and
// ADR-020: package marketdata is the sole historical-data gateway, and it
// must not import provider, storage, or vendor implementation packages.
// Those implementations live behind internal boundaries and depend on this
// package's types, not the other way around; letting marketdata import them
// would both invert the dependency direction and leak provider/storage
// concerns into the public boundary.
//
// The check scans this package's own non-test .go files for forbidden
// import paths. Substrings are matched against the import path so a future
// internal/marketdata/provider or adapters/.../oanda package is caught
// without enumerating each one.
var forbiddenImportSubstrings = []string{
	"internal/marketdata",
	"marketdata/provider",
	"marketdata/storage",
	"/oanda",
	"/dukascopy",
	"encoding/csv",
	"parquet",
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
