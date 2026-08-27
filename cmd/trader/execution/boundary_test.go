package execution

import (
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// compositionRootFiles are this command group's own composition-root
// code: they construct the full simulated Broker/execution.Planner/
// risk.Engine/risk.Sizer/pipeline.Pipeline stack exactly once, the
// same role cmd/trader/broker's own service.go plays for the broker
// command group. Every other production .go file in this package —
// leaf commands, formatters, argument parsing — must depend only on
// service/execution (plus presentation/domain value types), never on
// pipeline, execution, risk, or adapters/broker/sim directly (#204
// review: "strengthen the CLI boundary test ... not just in
// evaluate.go/submit.go" — a hand-picked leaf-file list would have let
// this exact dependency migrate into a formatter/helper file
// unnoticed).
var compositionRootFiles = map[string]bool{
	"service.go": true,
}

// forbiddenImports names import paths a non-composition-root file in
// this package must never import directly — the same reasoning
// cmd/trader/broker's own TestCommandHandlers_NeverImportSimBrokerDirectly
// and cmd/trader/data's own
// TestCommandHandlers_NeverImportMarketdataDirectly apply to their
// respective domains, broadened here to the full set of lower
// orchestration/domain packages this command group's service.go
// composes: nothing prevents a non-composition-root file from
// bypassing service/execution and calling one of these packages
// itself, so it is worth a real regression test rather than relying on
// convention alone.
var forbiddenImports = []string{
	`"github.com/rustyeddy/trader/adapters/broker/sim"`,
	`"github.com/rustyeddy/trader/pipeline"`,
	`"github.com/rustyeddy/trader/execution"`,
	`"github.com/rustyeddy/trader/risk"`,
}

// TestCommandHandlers_NeverImportDomainOrSimulatorDirectly scans every
// production (non-_test.go) .go file in this package's own directory
// except compositionRootFiles, rather than a hand-picked list of leaf
// command files: any new file added to this package — a formatter, an
// argument-parsing helper, a future leaf command — is automatically
// covered without this test needing an update to remember it exists.
func TestCommandHandlers_NeverImportDomainOrSimulatorDirectly(t *testing.T) {
	entries, err := os.ReadDir(".")
	require.NoError(t, err)

	fset := token.NewFileSet()
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		if compositionRootFiles[name] {
			continue
		}

		f, err := parser.ParseFile(fset, name, nil, parser.ImportsOnly)
		require.NoError(t, err, name)

		for _, imp := range f.Imports {
			for _, forbidden := range forbiddenImports {
				require.NotEqual(t, forbidden, imp.Path.Value,
					"%s must call the service layer (service/execution), not %s directly", name, imp.Path.Value)
			}
		}
	}
}
