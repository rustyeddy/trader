package indicators

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// The package computes values and knows nothing of strategies, brokers, accounts,
// services or transports: outside the standard library it may import only num, the
// price type ATR takes. The root package (the engine) imports it, so it could not
// import the root either.
func TestPackage_ImportsOnlyTheStandardLibraryAndNum(t *testing.T) {
	files, err := filepath.Glob("*.go")
	require.NoError(t, err)
	require.NotEmpty(t, files)
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), name, nil, parser.ImportsOnly)
		require.NoError(t, err, name)
		for _, imp := range f.Imports {
			path, err := strconv.Unquote(imp.Path.Value)
			require.NoError(t, err, name)
			if !strings.Contains(strings.Split(path, "/")[0], ".") { // standard library
				continue
			}
			require.Equal(t, "github.com/rustyeddy/trader/num", path, "%s imports %s", name, path)
		}
	}
}
