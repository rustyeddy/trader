package numericstudy

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNoFloatingPoint enforces the study's central claim: this package reaches
// its conclusions without binary floating point at any point, including the
// margin and headroom reporting.
//
// An earlier revision computed margins in float64, which made the README's
// "no float64" claim false in exactly the place a reader would care about — a
// study arguing against binary floating point should not depend on it to
// produce its evidence.  This test keeps that from coming back.
//
// It parses each file rather than grepping so identifiers in comments and
// strings, including this comment, do not trip it.
func TestNoFloatingPoint(t *testing.T) {
	dir, err := os.Getwd()
	require.NoError(t, err)

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)

	checked := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		checked++

		t.Run(e.Name(), func(t *testing.T) {
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, filepath.Join(dir, e.Name()), nil, 0)
			require.NoError(t, err)

			ast.Inspect(file, func(n ast.Node) bool {
				switch v := n.(type) {
				case *ast.Ident:
					if v.Name == "float64" || v.Name == "float32" {
						t.Errorf("%s: %s used at %s",
							e.Name(), v.Name, fset.Position(v.Pos()))
					}
				case *ast.BasicLit:
					if v.Kind == token.FLOAT || v.Kind == token.IMAG {
						t.Errorf("%s: floating-point literal %s at %s",
							e.Name(), v.Value, fset.Position(v.Pos()))
					}
				}
				return true
			})
		})
	}

	assert.Greater(t, checked, 1, "expected to scan the package's Go files")
}
