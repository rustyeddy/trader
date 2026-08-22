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

// dependencyDirectionExemptPrefixes names trees allowed to depend on
// broker even though they are not application/adapter code themselves:
// broker's own tree (obviously), and composition roots (cmd/) and
// adapters/ that are expected to wire a broker implementation into an
// application. Nothing under account/ or order/ is exempt — issue #145
// (M3-02) requires broker may depend on account/order, never the
// reverse.
var dependencyDirectionExemptPrefixes = []string{"broker", "cmd", "adapters", ".git"}

// TestAccountAndOrderDoNotImportBroker walks the whole module and fails
// if any .go file under account/ or order/ imports broker — the
// dependency direction ADR-007/ADR-008 and issue #145 require. It also
// fails, more generally, if anything outside the exempt prefixes above
// imports broker, since only application/adapter code is expected to
// choose a concrete broker implementation.
func TestAccountAndOrderDoNotImportBroker(t *testing.T) {
	repoRoot := findRepoRoot(t)

	var violations []string
	err := filepath.WalkDir(repoRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(repoRoot, path)
		if relErr != nil {
			return relErr
		}
		if d.IsDir() {
			if rel != "." && isDependencyDirectionExempt(rel) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".go") {
			return nil
		}
		if isDependencyDirectionExempt(rel) {
			return nil
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

	for _, v := range violations {
		assert.Fail(t, "domain package outside broker/cmd/adapters imports broker, violating the intended dependency direction", v)
	}
}

func isDependencyDirectionExempt(rel string) bool {
	for _, p := range dependencyDirectionExemptPrefixes {
		if rel == p || strings.HasPrefix(rel, p+string(filepath.Separator)) {
			return true
		}
	}
	return false
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
