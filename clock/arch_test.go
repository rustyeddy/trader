package clock

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

// This file enforces, mechanically, two structural guarantees from
// ADR-015 and issue #23.
//
// The first is the narrowed constraint from the issue #23 review: domain
// and application-orchestration code must not call time.Now, time.NewTimer,
// time.After, or time.Sleep directly. clock itself, composition roots
// (cmd/), adapters (adapters/), and any _test.go file anywhere (which
// covers benchmarks living inside a domain package, not just test/) are
// exempt — the goal is deterministic trading behavior, not eliminating
// every direct use of time from the module.
//
// The second is that the simulated implementation contains no goroutines
// of its own, checked directly against clock/simulated.go's source rather
// than through a runtime.NumGoroutine() count, which is a known-flaky way
// to observe the same thing.

var directTimeCallExemptPrefixes = []string{"clock", "cmd", "adapters", ".git"}

var directTimeCalls = map[string]bool{
	"Now":      true,
	"NewTimer": true,
	"After":    true,
	"Sleep":    true,
}

func TestDomainCodeDoesNotCallTimeDirectly(t *testing.T) {
	root := repoRoot(t)

	var violations []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}

		if d.IsDir() {
			if rel != "." && isDirectTimeCallExempt(rel) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".go") || strings.HasSuffix(d.Name(), "_test.go") {
			return nil
		}
		if isDirectTimeCallExempt(rel) {
			return nil
		}

		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}

		for _, pos := range findDirectTimeCalls(fset, file) {
			violations = append(violations, rel+": "+pos)
		}
		return nil
	})
	require.NoError(t, err)

	for _, v := range violations {
		assert.Fail(t, "domain/application code calls a time function directly instead of receiving a clock.Clock", v)
	}
}

// findDirectTimeCalls returns one description per call to a name in
// directTimeCalls on the package imported as "time" in file, resolving
// whatever local identifier that import was actually bound to
// (import stdtime "time" included) rather than assuming it is literally
// named "time". A dot import of "time" is not handled: resolving a bare,
// unqualified call against a dot-imported package's exported names would
// need full identifier resolution to avoid false positives against an
// unrelated local function of the same name, and a dot import of "time" is
// both extremely rare and easy to catch in ordinary review.
func findDirectTimeCalls(fset *token.FileSet, file *ast.File) []string {
	alias, imported := timeImportAlias(file)
	if !imported {
		return nil
	}

	var found []string
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		pkg, ok := sel.X.(*ast.Ident)
		if !ok || pkg.Name != alias {
			return true
		}
		if directTimeCalls[sel.Sel.Name] {
			found = append(found, "calls "+alias+"."+sel.Sel.Name+" at "+fset.Position(call.Pos()).String())
		}
		return true
	})
	return found
}

// timeImportAlias reports the local identifier file binds the standard
// library's "time" package to — "time" itself unless the import is
// aliased — and whether file imports it at all.
func timeImportAlias(file *ast.File) (alias string, imported bool) {
	for _, imp := range file.Imports {
		if strings.Trim(imp.Path.Value, `"`) != "time" {
			continue
		}
		if imp.Name == nil {
			return "time", true
		}
		return imp.Name.Name, true
	}
	return "", false
}

// TestFindDirectTimeCallsResolvesImportAlias is the regression test for the
// bypass a hardcoded "time" identifier check would miss: aliasing the
// import (import stdtime "time") does not change what package a call
// reaches, and the check must not either.
func TestFindDirectTimeCallsResolvesImportAlias(t *testing.T) {
	const src = `package example

import stdtime "time"

func f() {
	_ = stdtime.Now()
}
`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "example.go", src, 0)
	require.NoError(t, err)

	found := findDirectTimeCalls(fset, file)
	require.Len(t, found, 1)
	assert.Contains(t, found[0], "calls stdtime.Now")
}

// TestFindDirectTimeCallsUnaliasedImport confirms the ordinary case — an
// unaliased "time" import — still works the same way it did before alias
// resolution was added.
func TestFindDirectTimeCallsUnaliasedImport(t *testing.T) {
	const src = `package example

import "time"

func f() {
	_ = time.Now()
}
`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "example.go", src, 0)
	require.NoError(t, err)

	found := findDirectTimeCalls(fset, file)
	require.Len(t, found, 1)
	assert.Contains(t, found[0], "calls time.Now")
}

// TestFindDirectTimeCallsNoTimeImport confirms a file that never imports
// "time" at all — including one with an unrelated selector call that
// happens to be named Now — produces no findings.
func TestFindDirectTimeCallsNoTimeImport(t *testing.T) {
	const src = `package example

type clock struct{}

func (clock) Now() int { return 0 }

func f() {
	var c clock
	_ = c.Now()
}
`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "example.go", src, 0)
	require.NoError(t, err)

	assert.Empty(t, findDirectTimeCalls(fset, file))
}

func isDirectTimeCallExempt(rel string) bool {
	for _, p := range directTimeCallExemptPrefixes {
		if rel == p || strings.HasPrefix(rel, p+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// TestSimulatedContainsNoGoroutines parses clock/simulated.go directly and
// fails if it finds a go statement anywhere in it. Simulated's entire
// design rests on every operation being a synchronous, mutex-protected
// mutation; this keeps that guarantee from silently regressing.
func TestSimulatedContainsNoGoroutines(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "simulated.go", nil, 0)
	require.NoError(t, err)

	var found []string
	ast.Inspect(file, func(n ast.Node) bool {
		if g, ok := n.(*ast.GoStmt); ok {
			found = append(found, fset.Position(g.Pos()).String())
		}
		return true
	})

	for _, pos := range found {
		assert.Fail(t, "simulated.go must not start a goroutine", pos)
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	require.NoError(t, err)

	for range 10 {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatal("clock: no go.mod found above " + dir)
	return ""
}
