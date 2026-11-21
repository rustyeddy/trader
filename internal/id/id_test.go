package id

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The package makes IDs from the standard library and its own ULID encoding only:
// no third-party ID library, and nothing from the rest of the module (it is a leaf
// the engine and services depend on, never the other way round).
func TestPackage_ImportsOnlyTheStandardLibraryAndItsULID(t *testing.T) {
	files, err := filepath.Glob("*.go")
	require.NoError(t, err)
	require.NotEmpty(t, files)

	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), name, nil, parser.ImportsOnly)
		require.NoError(t, err)
		for _, imp := range f.Imports {
			path, err := strconv.Unquote(imp.Path.Value)
			require.NoError(t, err)
			stdlib := !strings.Contains(strings.SplitN(path, "/", 2)[0], ".")
			assert.True(t, stdlib || path == "github.com/rustyeddy/trader/internal/id/internal/ulid",
				"%s imports %s", name, path)
		}
	}
}
