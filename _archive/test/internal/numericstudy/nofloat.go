package numericstudy

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

// FloatUse is one binary-floating-point reference found in study source.
type FloatUse struct {
	File     string
	Position string
	What     string
}

// FindFloatUsage reports every float32/float64 identifier and floating-point
// literal in the Go files directly under dir.
//
// The studies conclude that scaled int64 is the right representation; they
// must not depend on binary floating point to reach that conclusion, including
// in their reporting.  An earlier revision computed margins in float64, which
// made a documented claim false in exactly the place a reader would check.
//
// Files are parsed rather than scanned as text, so identifiers appearing in
// comments and string literals — including this package's own prose about
// float64 — do not register.
func FindFloatUsage(dir string) ([]FloatUse, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var found []FloatUse
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}

		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, filepath.Join(dir, e.Name()), nil, 0)
		if err != nil {
			return nil, err
		}

		ast.Inspect(file, func(n ast.Node) bool {
			switch v := n.(type) {
			case *ast.Ident:
				if v.Name == "float64" || v.Name == "float32" {
					found = append(found, FloatUse{
						File:     e.Name(),
						Position: fset.Position(v.Pos()).String(),
						What:     v.Name,
					})
				}
			case *ast.BasicLit:
				if v.Kind == token.FLOAT || v.Kind == token.IMAG {
					found = append(found, FloatUse{
						File:     e.Name(),
						Position: fset.Position(v.Pos()).String(),
						What:     "literal " + v.Value,
					})
				}
			}
			return true
		})
	}

	return found, nil
}
