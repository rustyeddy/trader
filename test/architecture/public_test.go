package architecture_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/importer"
	"go/token"
	"go/types"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const modulePath = "github.com/rustyeddy/trader"

var publicPackages = []string{"analysis", "indicator", "instrument", "marketdata", "num", "order", "protocol/strategy/v1", "sdk", "version"}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("../..")
	require.NoError(t, err)
	return root
}

func runGo(t *testing.T, dir string, args ...string) ([]byte, error) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "go", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off")
	return cmd.CombinedOutput()
}

// TestExternalConsumer exercises the actual guest example from an unrelated
// module path, where Go enforces Trader's internal-package visibility.
func TestExternalConsumer(t *testing.T) {
	root := repositoryRoot(t)
	dir := t.TempDir()
	module := fmt.Sprintf("module example.com/external-strategy\n\ngo 1.25.1\n\nrequire %s v0.0.0\nreplace %s => %s\n", modulePath, modulePath, filepath.ToSlash(root))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte(module), 0600))
	guest, err := os.ReadFile(filepath.Join(root, "examples/sdk-minimal/main.go"))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), guest, 0600))
	// Named uses prove the intended contracts exist, rather than merely checking
	// that packages can be blank-imported.
	contracts := `package main
import (
 "time"
 "github.com/rustyeddy/trader/analysis"
 "github.com/rustyeddy/trader/indicator"
 "github.com/rustyeddy/trader/instrument"
 "github.com/rustyeddy/trader/marketdata"
 "github.com/rustyeddy/trader/num"
 "github.com/rustyeddy/trader/order"
 "github.com/rustyeddy/trader/sdk"
 "github.com/rustyeddy/trader/version"
 v1 "github.com/rustyeddy/trader/protocol/strategy/v1"
)
type guestClock struct{}
func (guestClock) Now() time.Time { return time.Time{} }
var _ sdk.Clock = guestClock{}
var _ = sdk.Environment{Clock: guestClock{}}
var _ = analysis.RunEventStudy
var _ = indicator.NewSMA
var _ instrument.ID
var _ = marketdata.H1
var _ num.Price
var _ = order.Buy
var _ = order.Long
var _ = order.IntentEnter
var _ = version.Current
var _ v1.BarEvent
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "contracts.go"), []byte(contracts), 0600))
	out, err := runGo(t, dir, "build", "-mod=mod", "-o", filepath.Join(dir, "guest"), ".")
	require.NoError(t, err, "%s", out)

	for _, name := range []string{"account", "portfolio", "backtest", "broker", "execution", "risk", "pipeline", "strategy", "service", "adapters/strategy/external", "journal", "report", "chart", "config", "logging", "clock", "id", "tradertest", "marketdata", "order"} {
		t.Run("reject/"+name, func(t *testing.T) {
			path := modulePath + "/internal/" + name
			source := fmt.Sprintf("package main\nimport _ %q\nfunc main() {}\n", path)
			require.NoError(t, os.WriteFile(filepath.Join(dir, "forbidden.go"), []byte(source), 0600))
			out, err := runGo(t, dir, "build", "-mod=mod", "forbidden.go")
			require.Error(t, err, "%s", out)
			require.Contains(t, string(out), "use of internal package "+path+" not allowed")
		})
	}
}

type listedPackage struct {
	ImportPath string
	Export     string
	Deps       []string
}

// TestPublicSurface checks the inventory, transitive runtime independence, and
// exported type graph using compiler export data, including methods and aliases.
func TestPublicSurface(t *testing.T) {
	root := repositoryRoot(t)
	out, err := runGo(t, root, "list", "-deps", "-export", "-json", "./...")
	require.NoError(t, err, "%s", out)
	all := map[string]listedPackage{}
	decoder := json.NewDecoder(bytes.NewReader(out))
	for decoder.More() {
		var p listedPackage
		require.NoError(t, decoder.Decode(&p))
		all[p.ImportPath] = p
	}
	for path := range all {
		if !strings.HasPrefix(path, modulePath+"/") {
			continue
		}
		rel := strings.TrimPrefix(path, modulePath+"/")
		if strings.HasPrefix(rel, "internal/") || strings.Contains(rel, "/internal/") || strings.HasPrefix(rel, "cmd/") || strings.HasPrefix(rel, "examples/") || strings.HasPrefix(rel, "test/") || strings.HasPrefix(rel, "research-runs/") {
			continue
		}
		require.Contains(t, publicPackages, rel, "new external package needs an explicit API decision")
	}
	imp := importer.ForCompiler(token.NewFileSet(), "gc", func(path string) (io.ReadCloser, error) {
		return os.Open(all[path].Export)
	})
	for _, name := range publicPackages {
		t.Run(name, func(t *testing.T) {
			path := modulePath + "/" + name
			p, ok := all[path]
			require.True(t, ok, "public package missing: %s", path)
			for _, dep := range p.Deps {
				require.False(t, strings.HasPrefix(dep, modulePath+"/internal/"), "%s depends on runtime %s", path, dep)
			}
			pkg, err := imp.Import(path)
			require.NoError(t, err)
			seen := map[types.Type]bool{}
			for _, name := range pkg.Scope().Names() {
				obj := pkg.Scope().Lookup(name)
				if obj.Exported() {
					checkPublicType(t, obj.Type(), seen)
				}
			}
			if name == "order" {
				for _, symbol := range pkg.Scope().Names() {
					if token.IsExported(symbol) {
						require.Contains(t, []string{"Side", "Buy", "Sell", "PositionSide", "Flat", "Long", "Short", "IntentKind", "IntentEnter", "IntentExit", "IntentAdjustStop", "IntentTargetExposure", "IntentEnterWithStop"}, symbol)
					}
				}
			}
			if name == "marketdata" {
				for _, symbol := range []string{"Manager", "Config", "New", "BarReader", "BarQuery", "Plan", "SyncResult", "BuildResult"} {
					require.Nil(t, pkg.Scope().Lookup(symbol), "runtime API leaked: %s", symbol)
				}
			}
			if name == "sdk" {
				clock := pkg.Scope().Lookup("Clock").Type().Underlying().(*types.Interface)
				require.Equal(t, 1, clock.NumMethods())
				require.Equal(t, "Now", clock.Method(0).Name())
			}
		})
	}
}

func checkPublicType(t *testing.T, typ types.Type, seen map[types.Type]bool) {
	t.Helper()
	if typ == nil || seen[typ] {
		return
	}
	seen[typ] = true
	check := func(next types.Type) { checkPublicType(t, next, seen) }
	object := func(obj *types.TypeName) {
		if obj.Pkg() != nil {
			path := obj.Pkg().Path()
			require.False(t, strings.HasPrefix(path, modulePath+"/") && slices.Contains(strings.Split(path, "/"), "internal"), "internal type in public signature: %s", obj)
		}
	}
	switch v := typ.(type) {
	case *types.Named:
		object(v.Obj())
		check(v.Underlying())
		for i := 0; i < v.NumMethods(); i++ {
			if v.Method(i).Exported() {
				check(v.Method(i).Type())
			}
		}
		for i := 0; i < v.TypeArgs().Len(); i++ {
			check(v.TypeArgs().At(i))
		}
		for i := 0; i < v.TypeParams().Len(); i++ {
			check(v.TypeParams().At(i))
		}
	case *types.Alias:
		object(v.Obj())
		check(types.Unalias(v))
	case *types.Pointer:
		check(v.Elem())
	case *types.Slice:
		check(v.Elem())
	case *types.Array:
		check(v.Elem())
	case *types.Map:
		check(v.Key())
		check(v.Elem())
	case *types.Chan:
		check(v.Elem())
	case *types.Struct:
		for i := 0; i < v.NumFields(); i++ {
			f := v.Field(i)
			if f.Exported() || f.Embedded() {
				check(f.Type())
			}
		}
	case *types.Interface:
		for i := 0; i < v.NumMethods(); i++ {
			check(v.Method(i).Type())
		}
		for i := 0; i < v.NumEmbeddeds(); i++ {
			check(v.EmbeddedType(i))
		}
	case *types.Signature:
		check(v.Params())
		check(v.Results())
		for i := 0; i < v.TypeParams().Len(); i++ {
			check(v.TypeParams().At(i))
		}
	case *types.Tuple:
		for i := 0; i < v.Len(); i++ {
			check(v.At(i).Type())
		}
	case *types.TypeParam:
		check(v.Constraint())
	case *types.Union:
		for i := 0; i < v.Len(); i++ {
			check(v.Term(i).Type())
		}
	}
}
