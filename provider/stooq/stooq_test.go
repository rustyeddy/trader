package stooq

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/internal/store/storetest"
)

// TestMain points the store at a temporary directory for the whole package, so
// a test that forgets storetest.Root still never touches the real data root.
func TestMain(m *testing.M) { os.Exit(storetest.Main(m)) }

const head = "<TICKER>,<PER>,<DATE>,<TIME>,<OPEN>,<HIGH>,<LOW>,<CLOSE>,<VOL>,<OPENINT>\r\n"

// line is a synthetic data row for date, YYYYMMDD.
func line(date string) string { return "SPY.US,D," + date + ",000000,10,11.5,9.25,10.1234,100,0\r\n" }

// TestProviderStooq_ImportsOnlyFoundationalPackages keeps the provider a leaf.
func TestProviderStooq_ImportsOnlyFoundationalPackages(t *testing.T) {
	const mod = "github.com/rustyeddy/trader"
	allowed := map[string]bool{mod + "/marketdata": true, mod + "/num": true, mod + "/internal/store": true}
	files, err := filepath.Glob("*.go")
	require.NoError(t, err)
	checked := 0
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		checked++
		f, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.ImportsOnly)
		require.NoError(t, err)
		for _, imp := range f.Imports {
			path, err := strconv.Unquote(imp.Path.Value)
			require.NoError(t, err)
			if path == mod || strings.HasPrefix(path, mod+"/") {
				assert.True(t, allowed[path], "%s imports %s", file, path)
			}
		}
	}
	require.Positive(t, checked)
}
