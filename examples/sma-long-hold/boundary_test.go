package main

import (
	"go/parser"
	"go/token"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// forbiddenRoots mirrors examples/m5's own identical list — issue
// #383's explicit constraint: "No broker/risk/execution access may be
// added to the external side." sdk's own boundary_test.go
// already enforces the deeper "never imports strategy/backtest/
// service/cmd/adapters/broker/execution/risk/pipeline" rule for
// sdk itself; this test enforces the identical rule for this
// binary's own code, which imports sdk but is not part of it.
var forbiddenRoots = []string{
	"github.com/rustyeddy/trader/strategy",
	"github.com/rustyeddy/trader/backtest",
	"github.com/rustyeddy/trader/service",
	"github.com/rustyeddy/trader/cmd",
	"github.com/rustyeddy/trader/adapters",
	"github.com/rustyeddy/trader/broker",
	"github.com/rustyeddy/trader/execution",
	"github.com/rustyeddy/trader/risk",
	"github.com/rustyeddy/trader/pipeline",
}

// TestSMALongHoldNeverImportsRuntimeOrTransportPackages parses
// main.go's own import block directly and fails if it names any
// forbidden root or subpackage.
func TestSMALongHoldNeverImportsRuntimeOrTransportPackages(t *testing.T) {
	const file = "main.go"

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, file, nil, parser.ImportsOnly)
	require.NoError(t, err)

	for _, imp := range f.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		require.NoError(t, err)

		for _, root := range forbiddenRoots {
			require.False(t, isForbiddenImport(path, root),
				"%s must not import %s or any of its subpackages — an external strategy must never gain broker/risk/execution access (issue #383)", file, root)
		}
	}
}

func isForbiddenImport(path, root string) bool {
	return path == root || strings.HasPrefix(path, root+"/")
}
