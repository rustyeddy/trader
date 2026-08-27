package execution

import (
	"go/parser"
	"go/token"
	"testing"

	"github.com/stretchr/testify/require"
)

// commandFiles are the leaf command handlers this package registers:
// the transport adapters that must call service/execution for every
// M4 use case, never adapters/broker/sim, pipeline, execution, or risk
// directly. command.go, listing.go, intentargs.go, and service.go are
// deliberately excluded — they are this command group's own
// composition-root code (they construct the full simulated
// Broker/execution.Planner/risk.Engine/risk.Sizer/pipeline.Pipeline
// stack exactly once, the same role cmd/trader/broker's own service.go
// plays for the broker command group), not leaf commands.
var commandFiles = []string{
	"evaluate.go",
	"submit.go",
}

// forbiddenImports names import paths a leaf command file must never
// import directly — the same reasoning cmd/trader/broker's own
// TestCommandHandlers_NeverImportSimBrokerDirectly and cmd/trader/
// data's own TestCommandHandlers_NeverImportMarketdataDirectly apply
// to their respective domains: nothing prevents a leaf command from
// bypassing service/execution and calling one of these packages
// itself, so it is worth a real regression test rather than relying on
// convention alone.
var forbiddenImports = []string{
	`"github.com/rustyeddy/trader/adapters/broker/sim"`,
	`"github.com/rustyeddy/trader/pipeline"`,
	`"github.com/rustyeddy/trader/execution"`,
	`"github.com/rustyeddy/trader/risk"`,
}

func TestCommandHandlers_NeverImportDomainOrSimulatorDirectly(t *testing.T) {
	fset := token.NewFileSet()
	for _, name := range commandFiles {
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
