package marketdata

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
)

// TestImports_OnlyNumAndTheStandardLibrary enforces the package boundary of
// ADR-091: the public model imports num and the standard library and nothing
// else, so no consumer can be pulled into an import cycle through it and the
// model never depends on storage, providers, services or the root engine.
func TestImports_OnlyNumAndTheStandardLibrary(t *testing.T) {
	const num = "github.com/rustyeddy/trader/num"
	files, err := filepath.Glob("*.go")
	require.NoError(t, err)
	checked := 0
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		checked++
		src, err := os.ReadFile(name)
		require.NoError(t, err)
		f, err := parser.ParseFile(token.NewFileSet(), name, src, parser.ImportsOnly)
		require.NoError(t, err)
		for _, imp := range f.Imports {
			path, err := strconv.Unquote(imp.Path.Value)
			require.NoError(t, err)
			first, _, _ := strings.Cut(path, "/")
			stdlib := !strings.Contains(first, ".")
			assert.True(t, stdlib || path == num, "%s imports %s: marketdata may import only num and the standard library", name, path)
		}
	}
	assert.Positive(t, checked)
}
