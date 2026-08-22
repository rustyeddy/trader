package broker

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// brokerImportPath is the import path a violating file would use.
const brokerImportPath = "github.com/rustyeddy/trader/broker"

// domainPackagesThatMustNotImportBroker names the trees that must remain
// broker-independent per ADR-007/ADR-008 and issue #145: account and
// order are lower-level domain packages broker itself depends on, and
// must never depend back on broker. This is deliberately narrower than
// "nothing outside broker/cmd/adapters may import broker" — a
// transport-neutral broker application service (M3-11, issue #154) is a
// legitimate, planned consumer of the public broker port, so this test
// must not block that by treating every non-broker, non-cmd package as
// forbidden.
var domainPackagesThatMustNotImportBroker = []string{"account", "order"}

// TestAccountAndOrderDoNotImportBroker walks the account/ and order/
// package trees and fails if any .go file there imports broker — the
// dependency direction ADR-007/ADR-008 and issue #145 require: broker
// may depend on account/order, never the reverse.
func TestAccountAndOrderDoNotImportBroker(t *testing.T) {
	repoRoot := findRepoRoot(t)

	var violations []string
	for _, pkg := range domainPackagesThatMustNotImportBroker {
		pkgDir := filepath.Join(repoRoot, pkg)
		err := filepath.WalkDir(pkgDir, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			if !strings.HasSuffix(d.Name(), ".go") {
				return nil
			}

			rel, relErr := filepath.Rel(repoRoot, path)
			if relErr != nil {
				return relErr
			}

			fset := token.NewFileSet()
			file, parseErr := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
			if parseErr != nil {
				return parseErr
			}
			for _, imp := range file.Imports {
				if strings.Trim(imp.Path.Value, `"`) == brokerImportPath {
					violations = append(violations, rel+": imports "+brokerImportPath)
				}
			}
			return nil
		})
		require.NoError(t, err)
	}

	for _, v := range violations {
		assert.Fail(t, "account/order must not import broker, violating the intended dependency direction", v)
	}
}

func findRepoRoot(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	require.NoError(t, err)

	for range 10 {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatal("broker: no go.mod found above " + dir)
	return ""
}
