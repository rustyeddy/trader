package marketdata_test

import (
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestImports_OnlyNumAndTheStandardLibrary enforces the package boundary of
// ADR-091: the public model imports num and the standard library and nothing
// else, so no consumer can be pulled into an import cycle through it and the
// model never depends on storage, providers, services or the root engine.
func TestImports_OnlyNumAndTheStandardLibrary(t *testing.T) {
	const num = "github.com/rustyeddy/trader/num"
	files, err := filepath.Glob("*.go")
	require.NoError(t, err)
	checked := 0
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		checked++
		src, err := os.ReadFile(name)
		require.NoError(t, err)
		f, err := parser.ParseFile(token.NewFileSet(), name, src, parser.ImportsOnly)
		require.NoError(t, err)
		for _, imp := range f.Imports {
			path, err := strconv.Unquote(imp.Path.Value)
			require.NoError(t, err)
			first, _, _ := strings.Cut(path, "/")
			stdlib := !strings.Contains(first, ".")
			assert.True(t, stdlib || path == num, "%s imports %s: marketdata may import only num and the standard library", name, path)
		}
	}
	assert.Positive(t, checked)
}

// A consumer outside the package builds a bar from a two-sided quote, lets the
// midpoint be derived once, and reads "the price" without choosing a basis.
func Example() {
	px := num.MustParsePrice
	bid := marketdata.OHLC{Open: px("1.10000"), High: px("1.10100"), Low: px("1.09900"), Close: px("1.10050")}
	ask := marketdata.OHLC{Open: px("1.10010"), High: px("1.10111"), Low: px("1.09911"), Close: px("1.10061")}
	mid, err := marketdata.MidOf(bid, ask)
	if err != nil {
		panic(err)
	}
	open := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	bar := marketdata.Bar{Time: open, AvailableAt: open.Add(time.Hour), Volume: 100, Bid: bid, Ask: ask, Mid: mid}

	fmt.Println(bar.Validate())
	fmt.Println(bar.OHLC().Close, bar.Bid.Close, bar.Ask.Close)
	fmt.Println(bar.IsAvailable(open.Add(30*time.Minute)), bar.IsAvailable(open.Add(time.Hour)))
	// Output:
	// <nil>
	// 1.100555 1.1005 1.10061
	// false true
}

// A single-price source writes the price to the series it is; nothing else is
// invented.
func Example_singlePriceSource() {
	px := num.MustParsePrice
	open := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	bar := marketdata.Bar{Time: open, AvailableAt: open.Add(24 * time.Hour),
		Trade: marketdata.OHLC{Open: px("187.10"), High: px("188.00"), Low: px("186.50"), Close: px("187.90")}}

	fmt.Println(bar.Validate(), bar.OHLC().Close, bar.Bid.IsZero(), bar.Ask.IsZero())
	// Output: <nil> 187.9 true true
}
