package main

import (
	"go/parser"
	"go/token"
	"testing"

	"github.com/stretchr/testify/require"
)

// commandFiles are every leaf command handler this package registers
// (databars.go, datacoverage.go, dataplan.go, datasync.go,
// databuild.go, dataupdate.go): the transport adapters that must call
// the service layer for every market-data operation, never
// *marketdata.Manager directly (ADR-022's own "cmd/trader -> service
// -> marketdata" dependency direction). Only dataservice.go -- the
// composition-root code that constructs *marketdata.Manager itself,
// exactly once, so it can hand it to svc.New -- is allowed to import
// marketdata at all.
var commandFiles = []string{
	"databars.go",
	"datacoverage.go",
	"dataplan.go",
	"datasync.go",
	"databuild.go",
	"dataupdate.go",
}

// TestCommandHandlers_NeverImportMarketdataDirectly is issue #112's
// architectural guard: unlike marketdata/internal (see this file's own
// package-level note below), nothing prevents a command handler from
// importing github.com/rustyeddy/trader/marketdata directly and
// calling a Manager method itself, bypassing service/marketdata
// entirely -- that boundary is convention, not something the Go
// compiler enforces, so it is the one worth a real regression test
// here, the same reasoning service/boundary_test.go (#104) already
// applied to service/marketdata's own transport-framework boundary.
//
// This parses each file's own import block directly (go/parser,
// ImportsOnly) rather than using `go list`: cmd/trader is one package
// (main), so `go list`'s own Imports for it merges every file's
// imports together and cannot attribute one back to the specific file
// that added it -- exactly the per-file distinction this guard needs
// (dataservice.go legitimately imports marketdata; no command handler
// may).
func TestCommandHandlers_NeverImportMarketdataDirectly(t *testing.T) {
	const forbidden = `"github.com/rustyeddy/trader/marketdata"`

	fset := token.NewFileSet()
	for _, name := range commandFiles {
		f, err := parser.ParseFile(fset, name, nil, parser.ImportsOnly)
		require.NoError(t, err, name)

		for _, imp := range f.Imports {
			require.NotEqual(t, forbidden, imp.Path.Value,
				"%s must call the service layer (service/marketdata), not marketdata directly", name)
		}
	}
}

// brokerCommandFiles are the leaf command handlers issue #155 (M3-12)
// added: the transport adapters that must call service/broker for
// every simulated-broker operation, never adapters/broker/sim
// directly. brokerservice.go and brokerlisting.go are deliberately
// excluded -- they are this command group's own composition-root code
// (they construct *simbroker.Broker and its instrument.Listing/
// simbroker.FillPriceSource dependencies exactly once, the same role
// dataservice.go plays for the marketdata command group above), not
// leaf commands.
var brokerCommandFiles = []string{
	"brokeraccounts.go",
	"brokersubmit.go",
}

// TestBrokerCommandHandlers_NeverImportSimBrokerDirectly is issue
// #155's own architectural guard, mirroring
// TestCommandHandlers_NeverImportMarketdataDirectly above: nothing
// prevents a broker leaf command from importing
// github.com/rustyeddy/trader/adapters/broker/sim directly and
// bypassing service/broker, so it is worth a real regression test
// rather than relying on convention alone.
func TestBrokerCommandHandlers_NeverImportSimBrokerDirectly(t *testing.T) {
	const forbidden = `"github.com/rustyeddy/trader/adapters/broker/sim"`

	fset := token.NewFileSet()
	for _, name := range brokerCommandFiles {
		f, err := parser.ParseFile(fset, name, nil, parser.ImportsOnly)
		require.NoError(t, err, name)

		for _, imp := range f.Imports {
			require.NotEqual(t, forbidden, imp.Path.Value,
				"%s must call the service layer (service/broker), not adapters/broker/sim directly", name)
		}
	}
}

// marketdata/internal itself is deliberately not checked by any test
// in this package (issue #112's own "boundary/import guard exists, or
// the completion review records why compiler enforcement is
// sufficient" acceptance criterion): cmd/trader lives entirely outside
// the marketdata package's own subtree, so Go's internal/ visibility
// rule already makes importing marketdata/internal a compile error for
// every file in this package, unconditionally -- verified by hand with
// a throwaway probe file across #109-#111 -- the same reasoning
// docs/arch/package-boundaries.org and service/boundary_test.go's own
// doc comment already record for this exact boundary elsewhere in the
// module. A hand-written test would only re-assert what the compiler
// cannot fail to enforce.
