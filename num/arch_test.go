package num

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

// This file enforces, mechanically, the two structural rules ADR-004 and the
// package boundary depend on:
//
//   - no float32/float64 anywhere in num or num/internal/fixed;
//   - no package outside num importing num/internal/fixed.
//
// The import rule is already guaranteed by Go's internal/ package visibility
// rule — the compiler will not build a package outside the num tree that
// imports num/internal/fixed. The test below is a second, independent check
// of the same property: it documents the rule in a place a reviewer will
// find, and it keeps failing loudly if the package is ever moved out from
// under internal/ and loses that compiler guarantee.

// fixedImportPath is the import path packages outside num must never use.
const fixedImportPath = "github.com/rustyeddy/trader/num/internal/fixed"

// TestNoFloatingPoint enforces the ADR-004 float containment policy within
// num's own implementation: no float32 or float64 identifier or literal may
// appear anywhere under num, including num/internal/fixed.
//
// Source is parsed rather than scanned as text, so identifiers and literals
// appearing only in comments or string literals do not register.
func TestNoFloatingPoint(t *testing.T) {
	root, err := os.Getwd()
	require.NoError(t, err)

	found := findFloatUsage(t, root)
	for _, u := range found {
		assert.Fail(t, "binary floating point in num source", "%s at %s", u.what, u.pos)
	}
}

type floatUse struct {
	pos  string
	what string
}

func findFloatUsage(t *testing.T, root string) []floatUse {
	t.Helper()

	var found []floatUse
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".go") {
			return nil
		}

		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}

		ast.Inspect(file, func(n ast.Node) bool {
			switch v := n.(type) {
			case *ast.Ident:
				if v.Name == "float64" || v.Name == "float32" {
					found = append(found, floatUse{
						pos:  fset.Position(v.Pos()).String(),
						what: v.Name,
					})
				}
			case *ast.BasicLit:
				if v.Kind == token.FLOAT || v.Kind == token.IMAG {
					found = append(found, floatUse{
						pos:  fset.Position(v.Pos()).String(),
						what: v.Value,
					})
				}
			}
			return true
		})
		return nil
	})
	require.NoError(t, err)
	return found
}

// TestFixedIsNotImportedOutsideNum walks the whole repository module and
// fails if any package outside the num tree imports num/internal/fixed. See
// the file comment for why this duplicates a guarantee the Go compiler
// already provides.
func TestFixedIsNotImportedOutsideNum(t *testing.T) {
	numDir, err := os.Getwd()
	require.NoError(t, err)

	repoRoot := filepath.Dir(numDir) // num's parent is the module root

	var violations []string
	err = filepath.WalkDir(repoRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".go") {
			return nil
		}

		rel, err := filepath.Rel(repoRoot, path)
		if err != nil {
			return err
		}
		if rel == "num" || strings.HasPrefix(rel, "num"+string(filepath.Separator)) {
			return nil // inside the num tree; the rule does not apply here
		}

		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, imp := range file.Imports {
			importPath := strings.Trim(imp.Path.Value, `"`)
			if importPath == fixedImportPath {
				violations = append(violations, rel+": imports "+fixedImportPath)
			}
		}
		return nil
	})
	require.NoError(t, err)

	for _, v := range violations {
		assert.Fail(t, "package outside num imports num/internal/fixed", v)
	}
}
