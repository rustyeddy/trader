package broker

import (
	"go/parser"
	"go/token"
	"testing"

	"github.com/stretchr/testify/require"
)

// commandFiles are the leaf command handlers this package registers:
// the transport adapters that must call service/broker for every
// simulated-broker operation, never adapters/broker/sim directly.
// service.go and listing.go are deliberately excluded -- they are this
// command group's own composition-root code (they construct
// *simbroker.Broker and its instrument.Listing/simbroker.
// FillPriceSource dependencies exactly once, the same role
// cmd/trader/data's own service.go plays for the data command group),
// not leaf commands.
var commandFiles = []string{
	"accounts.go",
	"submit.go",
}

// TestCommandHandlers_NeverImportSimBrokerDirectly is issue #155's own
// architectural guard (relocated from cmd/trader's own boundary_test.go
// by issue #201's command-family package split), mirroring
// cmd/trader/data's own TestCommandHandlers_NeverImportMarketdataDirectly:
// nothing prevents a broker leaf command from importing
// github.com/rustyeddy/trader/adapters/broker/sim directly and
// bypassing service/broker, so it is worth a real regression test
// rather than relying on convention alone.
func TestCommandHandlers_NeverImportSimBrokerDirectly(t *testing.T) {
	const forbidden = `"github.com/rustyeddy/trader/adapters/broker/sim"`

	fset := token.NewFileSet()
	for _, name := range commandFiles {
		f, err := parser.ParseFile(fset, name, nil, parser.ImportsOnly)
		require.NoError(t, err, name)

		for _, imp := range f.Imports {
			require.NotEqual(t, forbidden, imp.Path.Value,
				"%s must call the service layer (service/broker), not adapters/broker/sim directly", name)
		}
	}
}
