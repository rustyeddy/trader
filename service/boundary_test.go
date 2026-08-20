package service_test

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// forbiddenTransportImports names import paths a service subpackage must
// never depend on, directly or transitively (service/doc.go). None of
// them are imported today; this test exists to catch a future
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
	out, err := exec.Command("go", "list", "-f", "{{range .Imports}}{{.}}\n{{end}}", "./...").Output()
	require.NoError(t, err, "go list -f {{.Imports}} ./...")

	for imp := range strings.FieldsSeq(string(out)) {
		for _, forbidden := range forbiddenTransportImports {
			require.NotEqual(t, forbidden, imp,
				"service/... must not directly import transport package %q", forbidden)
		}
	}
}
