package oanda

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/internal/store/storetest"
	"github.com/rustyeddy/trader/num"
	"github.com/stretchr/testify/require"
)

// TestMain points the store at a temporary directory for the whole package, so
// a test that forgets storetest.Root still never touches the real data root.
func TestMain(m *testing.M) { os.Exit(storetest.Main(m)) }

// eurusdMeta is the Meta a valid EURUSD 2020-05 H1 partition resolves to.
func eurusdMeta() Meta {
	return Meta{
		Instrument: instrument.CurrencyPairID(num.MustParseCurrency("EUR"), num.MustParseCurrency("USD")),
		Interval:   RawH1,
		Year:       2020,
		Month:      time.May,
		Symbol:     "EURUSD",
	}
}

func at(y int, m time.Month, d, h int) time.Time { return time.Date(y, m, d, h, 0, 0, 0, time.UTC) }

// fixtureCase names an archive fixture and the partition it describes.
type fixtureCase struct {
	file     string
	symbol   string
	interval RawInterval
	year     int
	month    time.Month
}

var fixtures = []fixtureCase{
	{"EURUSD-2020-05-h1.csv", "EURUSD", RawH1, 2020, time.May},
	{"EURUSD-2020-05-d1.csv", "EURUSD", RawD1, 2020, time.May},
	{"USDJPY-2020-05-h1.csv", "USDJPY", RawH1, 2020, time.May},
}

func mustPath(t *testing.T, root string) string {
	t.Helper()
	p, err := PartitionKey{"EURUSD", RawH1, 2024, time.January}.Path()
	require.NoError(t, err)
	p = filepath.Join(root, p)
	return p
}

// TestProviderOanda_ImportsOnlyFoundationalPackages keeps the provider a leaf:
// within the module it may import only foundational packages (the public
// marketdata model Bar/OHLC, instrument, num, logging, layout and store), never
// marketdata/internal, a service, cmd, the root engine or another provider.
func TestProviderOanda_ImportsOnlyFoundationalPackages(t *testing.T) {
	const mod = "github.com/rustyeddy/trader"
	allowed := map[string]bool{
		mod + "/instrument":                 false,
		mod + "/marketdata":                 false,
		mod + "/num":                        false,
		mod + "/internal/logging":           false,
		mod + "/internal/marketdata/layout": false,
		mod + "/internal/store":             false,
	}
	files, err := filepath.Glob("*.go")
	require.NoError(t, err)
	checked := 0
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		checked++
		f, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.ImportsOnly)
		require.NoError(t, err)
		for _, imp := range f.Imports {
			path, err := strconv.Unquote(imp.Path.Value)
			require.NoError(t, err)
			if path == mod || strings.HasPrefix(path, mod+"/") {
				_, ok := allowed[path]
				require.True(t, ok, "%s imports %s", file, path)
			}
		}
	}
	require.Positive(t, checked)
}
