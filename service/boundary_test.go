package service_test

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// forbiddenTransportImports names import paths a service subpackage must
// never import directly (service/doc.go). This is a direct-import rule,
// not a transitive one — see TestServiceHasNoTransportDependencies's own
// doc comment for why a transitive ban would be wrong here. None of
// these are imported today; this test exists to catch a future
// regression as issues #105-#113 add real use cases and, eventually, a
// CLI adapter that must stay separate from the service layer it calls.
var forbiddenTransportImports = []string{
	"net/http",
	"net/rpc",
	"google.golang.org/grpc",
	"github.com/spf13/cobra",
	"github.com/gorilla/websocket",
	"nhooyr.io/websocket",
	"github.com/gorilla/mux",
	"github.com/gin-gonic/gin",
}

// TestServiceHasNoTransportDependencies asserts that no package under
// service/... directly imports a transport framework. This checks each
// package's own Imports, not its full transitive Deps: a domain package
// service wraps (marketdata, via its OANDA provider) legitimately uses
// net/http as an HTTP *client*, and that transitive dependency is not
// what this rule guards against. What must never happen is a service
// subpackage itself importing net/http, Cobra, gRPC, or another
// transport/server framework directly, which is what would let
// transport concerns leak into application use-case code.
//
// marketdata/internal is deliberately not checked here: service/
// marketdata lives outside the marketdata package's own subtree, so
// Go's internal/ visibility rule already makes importing
// marketdata/internal a compile error, the same reasoning
// docs/arch/package-boundaries.org applies to every other internal/
// boundary in this module.
func TestServiceHasNoTransportDependencies(t *testing.T) {
	// The package pattern is the fully qualified
	// github.com/rustyeddy/trader/service/... rather than a relative
	// ./... so this check is unambiguously scoped to the service layer
	// regardless of the test binary's working directory: a relative
	// pattern here would happen to also work today (go test runs each
	// package's tests with that package's own source directory as cwd),
	// but that is an implicit dependency on go test's invocation
	// behavior this test should not rely on to stay correctly scoped.
	const pattern = "github.com/rustyeddy/trader/service/..."

	out, err := exec.Command("go", "list", "-f", "{{.ImportPath}}\t{{range .Imports}}{{.}} {{end}}", pattern).Output()
	require.NoError(t, err, "go list -f ... %s", pattern)

	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	require.NotEmpty(t, lines, "expected at least one package under %s", pattern)

	for _, line := range lines {
		pkg, imports, ok := strings.Cut(line, "\t")
		require.True(t, ok, "unexpected go list output line: %q", line)
		require.True(t, strings.HasPrefix(pkg, "github.com/rustyeddy/trader/service"),
			"go list returned a package outside the service layer: %q", pkg)

		for imp := range strings.FieldsSeq(imports) {
			for _, forbidden := range forbiddenTransportImports {
				require.NotEqual(t, forbidden, imp,
					"%s must not directly import transport package %q", pkg, forbidden)
			}
		}
	}
}
