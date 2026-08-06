package config

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

// This file enforces, mechanically, the architectural constraint issue #20
// states in prose: "Domain packages must not read files, flags, or
// environment variables." Composition roots (cmd/) and test harnesses
// (test/) are exempt — they are exactly where configuration assembly
// belongs — and so is config itself, which necessarily calls os.Getenv and
// friends to implement the environment source.
//
// Unlike num's equivalent check (which duplicates a guarantee the Go
// compiler already enforces via internal/ visibility), nothing stops a
// domain package from importing "os" or "flag" today — this test is the
// only thing that would catch it.

// exemptPrefixes are repository-relative path prefixes allowed to touch the
// process environment or command-line flags directly.
var exemptPrefixes = []string{"config", "cmd", "test", ".git"}

func TestDomainPackagesDoNotReadEnvOrFlags(t *testing.T) {
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
			if rel != "." && isExempt(rel) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".go") || isExempt(rel) {
			return nil
		}

		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			return err
		}

		for _, imp := range file.Imports {
			if strings.Trim(imp.Path.Value, `"`) == "flag" {
				violations = append(violations, rel+": imports \"flag\"")
			}
		}

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
			if !ok || pkg.Name != "os" {
				return true
			}
			if sel.Sel.Name == "Getenv" || sel.Sel.Name == "LookupEnv" || sel.Sel.Name == "Environ" {
				violations = append(violations,
					rel+": calls os."+sel.Sel.Name+" at "+fset.Position(call.Pos()).String())
			}
			return true
		})
		return nil
	})
	require.NoError(t, err)

	for _, v := range violations {
		assert.Fail(t, "package outside config/cmd/test reads the environment or flags directly", v)
	}
}

func isExempt(rel string) bool {
	for _, p := range exemptPrefixes {
		if rel == p || strings.HasPrefix(rel, p+string(filepath.Separator)) {
			return true
		}
	}
	return false
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
	t.Fatal("config: no go.mod found above " + dir)
	return ""
}
