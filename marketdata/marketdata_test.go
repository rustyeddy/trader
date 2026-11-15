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

const modulePath = "github.com/rustyeddy/trader"

// importsOf returns the import paths of one Go file.
func importsOf(t *testing.T, file string) []string {
	t.Helper()
	src, err := os.ReadFile(file)
	require.NoError(t, err)
	f, err := parser.ParseFile(token.NewFileSet(), file, src, parser.ImportsOnly)
	require.NoError(t, err)
	var out []string
	for _, imp := range f.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		require.NoError(t, err)
		out = append(out, path)
	}
	return out
}

// goFiles lists the Go files under dir (relative to this package), skipping the
// archive, build output, VCS data and test data.
func goFiles(t *testing.T, dir string) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "_archive", "bin", "testdata", "docs":
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, ".go") {
			files = append(files, path)
		}
		return nil
	})
	require.NoError(t, err)
	return files
}

// TestMarketdataTree_DependsOnlyOnNumLayoutAndItself enforces ADR-091's flow for
// the whole market-data tree, public package and internal implementation alike:
// within the module it may import num, the shared path resolver (storage, until
// it has its own home) and itself, and nothing else: not a provider, the
// transport, an application service, or the root engine. Go does not forbid
// those imports; this does.
func TestMarketdataTree_DependsOnlyOnNumLayoutAndItself(t *testing.T) {
	allowed := func(path string) bool {
		return path == modulePath+"/num" ||
			path == modulePath+"/internal/marketdata/layout" ||
			path == modulePath+"/marketdata" || strings.HasPrefix(path, modulePath+"/marketdata/")
	}
	checked := 0
	for _, file := range goFiles(t, ".") {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		checked++
		for _, path := range importsOf(t, file) {
			if path == modulePath || strings.HasPrefix(path, modulePath+"/") {
				assert.True(t, allowed(path), "%s imports %s: the marketdata tree may import only num, the layout resolver and itself", file, path)
			}
		}
	}
	assert.Positive(t, checked)
}

// TestMarketdataInternal_IsImportedOnlyFromMarketdata checks that nothing outside
// the marketdata tree reaches into marketdata/internal. Go's internal rule
// already makes that a compile error; this test keeps the guarantee visible, and
// keeps it if the implementation is ever moved out of an internal directory by
// mistake.
func TestMarketdataInternal_IsImportedOnlyFromMarketdata(t *testing.T) {
	internal := modulePath + "/marketdata/internal"
	checked := 0
	for _, file := range goFiles(t, "..") {
		rel, err := filepath.Rel("..", file)
		require.NoError(t, err)
		if strings.HasPrefix(rel, "marketdata"+string(filepath.Separator)) {
			continue
		}
		checked++
		for _, path := range importsOf(t, file) {
			assert.False(t, path == internal || strings.HasPrefix(path, internal+"/"),
				"%s imports %s: only the marketdata tree may import marketdata/internal", rel, path)
		}
	}
	assert.Positive(t, checked, "the walk found the rest of the module")
}
