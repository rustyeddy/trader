package logging

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

// This file enforces, mechanically, the architectural constraint issue #21
// states in prose: "Do not create a mutable global logger" and "Inject
// loggers into components that need them." slog itself keeps a mutable
// package-level default logger (slog.Default, changed by slog.SetDefault),
// and the package-level Debug/Info/Warn/Error/Log/LogAttrs/With functions
// all read it implicitly. A component that calls any of those, or that
// reassigns the default, has a hidden dependency on global state instead of
// an injected *slog.Logger — exactly what this constraint forbids.
//
// logging itself, cmd/ composition roots, and test/ harnesses are exempt:
// logging.New legitimately builds handlers without an injected logger to
// wrap, and a cmd/ binary is where the one intentional
// slog.SetDefault-equivalent choice (if any) belongs, not scattered through
// domain code.

var globalLoggerExemptPrefixes = []string{"logging", "cmd", "test", ".git"}

// slog identifiers that read or mutate the mutable package-level default
// logger. slog.New, slog.NewTextHandler, slog.NewJSONHandler,
// slog.DiscardHandler, and the handler/level/attr constructors are not
// included: they build values, they don't touch global state.
var globalLoggerCalls = map[string]bool{
	"Default":      true,
	"SetDefault":   true,
	"Debug":        true,
	"DebugContext": true,
	"Info":         true,
	"InfoContext":  true,
	"Warn":         true,
	"WarnContext":  true,
	"Error":        true,
	"ErrorContext": true,
	"Log":          true,
	"LogAttrs":     true,
	"With":         true,
}

func TestNoPackageUsesSlogGlobalDefaultLogger(t *testing.T) {
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
			if rel != "." && isGlobalLoggerExempt(rel) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".go") || isGlobalLoggerExempt(rel) {
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
			if !ok || pkg.Name != "slog" {
				return true
			}
			if globalLoggerCalls[sel.Sel.Name] {
				violations = append(violations,
					rel+": calls slog."+sel.Sel.Name+" at "+fset.Position(call.Pos()).String())
			}
			return true
		})
		return nil
	})
	require.NoError(t, err)

	for _, v := range violations {
		assert.Fail(t, "package outside logging/cmd/test depends on slog's mutable global default logger", v)
	}
}

func isGlobalLoggerExempt(rel string) bool {
	for _, p := range globalLoggerExemptPrefixes {
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
	t.Fatal("logging: no go.mod found above " + dir)
	return ""
}
