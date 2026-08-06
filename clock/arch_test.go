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
			if !ok || pkg.Name != "time" {
				return true
			}
			if directTimeCalls[sel.Sel.Name] {
				violations = append(violations,
					rel+": calls time."+sel.Sel.Name+" at "+fset.Position(call.Pos()).String())
			}
			return true
		})
		return nil
	})
	require.NoError(t, err)

	for _, v := range violations {
		assert.Fail(t, "domain/application code calls a time function directly instead of receiving a clock.Clock", v)
	}
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
