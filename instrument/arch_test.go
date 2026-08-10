package instrument

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

// This file mechanically enforces the "no synthetic or multi-leg fields"
// constraint from issue #25 and the architecture document's "Deferred:
// Synthetic and Multi-Leg Instruments" section: nothing in this package's
// non-test source may name a struct field after legs, synthetic
// composition, or multi-leg execution. Adding such a field is a deliberate
// architectural decision — the deferred section names the reasons for
// waiting in detail — not something that should slip in as an incidental
// addition to an unrelated change.
var forbiddenFieldSubstrings = []string{"leg", "synthetic", "multileg"}

// TestNoSyntheticOrMultiLegFields walks this package's own non-test source
// files and fails if any struct field name matches a forbidden substring.
func TestNoSyntheticOrMultiLegFields(t *testing.T) {
	dir, err := os.Getwd()
	require.NoError(t, err)

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}

		fset := token.NewFileSet()
		path := filepath.Join(dir, e.Name())
		file, err := parser.ParseFile(fset, path, nil, 0)
		require.NoError(t, err)

		for _, v := range findForbiddenFields(fset, file) {
			assert.Fail(t, "forbidden field name", v)
		}
	}
}

// TestForbiddenFieldDetectorFindsSyntheticFields proves the detector used
// above actually fires, against a fabricated source that is never part of
// the real package.
func TestForbiddenFieldDetectorFindsSyntheticFields(t *testing.T) {
	const src = `package instrument

type Bad struct {
	Legs      []int
	Synthetic bool
	MultiLeg  bool
	Fine      string
}
`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "bad.go", src, 0)
	require.NoError(t, err)

	violations := findForbiddenFields(fset, file)
	// MultiLeg matches both "leg" and "multileg", so Legs + Synthetic +
	// MultiLeg (x2) is 4 violations, not 3.
	assert.Len(t, violations, 4)
}

func findForbiddenFields(fset *token.FileSet, file *ast.File) []string {
	var violations []string

	ast.Inspect(file, func(n ast.Node) bool {
		ts, ok := n.(*ast.TypeSpec)
		if !ok {
			return true
		}
		st, ok := ts.Type.(*ast.StructType)
		if !ok || st.Fields == nil {
			return true
		}
		for _, field := range st.Fields.List {
			for _, name := range field.Names {
				lower := strings.ToLower(name.Name)
				for _, forbidden := range forbiddenFieldSubstrings {
					if strings.Contains(lower, forbidden) {
						violations = append(violations, ts.Name.Name+"."+name.Name+" at "+
							fset.Position(field.Pos()).String()+" matches deferred synthetic/multi-leg naming ("+forbidden+")")
					}
				}
			}
		}
		return true
	})

	return violations
}
