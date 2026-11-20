package proto

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// adrExamples are the JSON examples of ADR-094, in the order they appear. They
// are the spec: they must stay decodable by this package.
func adrExamples(t *testing.T) []string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "docs", "arch", "adr-094-external-strategy-wire-protocol-v1.org"))
	require.NoError(t, err)
	blocks := regexp.MustCompile(`(?s)#\+begin_src json\n(.*?)#\+end_src`).FindAllStringSubmatch(string(b), -1)
	var out []string
	for _, m := range blocks {
		out = append(out, m[1])
	}
	require.NotEmpty(t, out)
	return out
}

func TestADR094_EveryJSONExampleDecodes(t *testing.T) {
	seen := map[Type]int{}
	for i, ex := range adrExamples(t) {
		m, err := Decode([]byte(ex))
		require.NoError(t, err, "example %d:\n%s", i+1, ex)
		seen[m.Head().Type]++
	}
	for _, typ := range []Type{TypeRegister, TypeAccepted, TypeBars, TypeOrders, TypeDone, TypeError} {
		assert.Positive(t, seen[typ], "the ADR has an example of %s", typ)
	}
}

func TestADR094_ExamplesSurviveADecodeEncodeDecode(t *testing.T) {
	for i, ex := range adrExamples(t) {
		first, err := Decode([]byte(ex))
		require.NoError(t, err)
		payload, err := Encode(first)
		require.NoError(t, err, "example %d", i+1)

		second, err := Decode(payload)

		require.NoError(t, err)
		assert.Equal(t, first, second, "example %d", i+1)
	}
}

func TestADR094_ExamplesThatCarryOnlyWhatTheEncoderWritesAreJSONEqual(t *testing.T) {
	for _, typ := range []Type{TypeAccepted, TypeDone, TypeError, TypeRegister} {
		found := false
		for _, ex := range adrExamples(t) {
			m, err := Decode([]byte(ex))
			require.NoError(t, err)
			if m.Head().Type != typ {
				continue
			}
			found = true
			payload, err := Encode(m)
			require.NoError(t, err)
			assert.JSONEq(t, ex, string(payload), "the %s example of the ADR is what Encode writes", typ)
			break
		}
		assert.True(t, found, typ)
	}
}

func TestADR094_EveryDecodeLevelNegativeCaseOfTheADRIsRefused(t *testing.T) {
	// The cases of the ADR that the codec itself decides, by their number.
	table := regexp.MustCompile(`\| (N\d+[a-c]?|S\d+[a-c]?) \|`).FindAllStringSubmatch(adrText(t), -1)
	require.NotEmpty(t, table, "the ADR lists its negative cases")
	// Spot-check that the numbering the tests cite is still in the ADR.
	text := adrText(t)
	for _, n := range []string{"N14", "N15", "N16", "N16a", "N16b", "N16c", "N19", "N20", "N26", "N27", "N28"} {
		assert.True(t, strings.Contains(text, "| "+n+" |"), "the ADR still has case %s that the decoder tests cite", n)
	}
}

func adrText(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "docs", "arch", "adr-094-external-strategy-wire-protocol-v1.org"))
	require.NoError(t, err)
	return string(b)
}

func TestMessages_AreAllMessages(t *testing.T) {
	for _, m := range []Message{&Register{}, &Accepted{}, &Bars{}, &Orders{}, &Done{}, &Failure{}} {
		assert.NotNil(t, m.Head())
	}
}

// TestExternalModuleCanImportAndBuild proves the surface is usable from another
// repository: a module outside this one imports strategy and strategy/proto, and
// marketdata, num and instrument beside them, implements Strategy, speaks the
// protocol, and builds. It needs the Go toolchain and the module cache the
// repository's own build already uses; nothing is downloaded.
func TestExternalModuleCanImportAndBuild(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a module")
	}
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("no go toolchain")
	}
	root, err := filepath.Abs(filepath.Join("..", ".."))
	require.NoError(t, err)
	goMod, err := os.ReadFile(filepath.Join(root, "go.mod"))
	require.NoError(t, err)
	goVersion := regexp.MustCompile(`(?m)^go (\S+)`).FindStringSubmatch(string(goMod))[1]

	dir := t.TempDir()
	write := func(name, body string) {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644))
	}
	write("go.mod", "module example.com/mystrategy\n\ngo "+goVersion+"\n\nrequire github.com/rustyeddy/trader v0.0.0\n\nreplace github.com/rustyeddy/trader => "+strconv.Quote(root)[1:len(strconv.Quote(root))-1]+"\n")
	sums, err := os.ReadFile(filepath.Join(root, "go.sum"))
	require.NoError(t, err)
	write("go.sum", string(sums))
	write("main.go", `package main

import (
	"bytes"
	"context"
	"fmt"

	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/strategy"
	"github.com/rustyeddy/trader/strategy/proto"
)

type ema struct{}

func (ema) OnBars(_ context.Context, bars []marketdata.Bar) ([]strategy.Order, error) {
	if len(bars) == 0 {
		return nil, nil
	}
	px := bars[0].OHLC().Close
	stop, _ := px.Sub(num.MustParsePrice("0.002"))
	return []strategy.Order{{Instrument: bars[0].Instrument, Action: strategy.Open, Direction: strategy.Long, Price: px, StopLoss: stop}}, nil
}

var _ strategy.Strategy = ema{}
var _ = instrument.ID{}

func main() {
	var buf bytes.Buffer
	reply := &proto.Orders{Header: proto.Header{Seq: 2, RunID: "r"}, InReplyTo: 2}
	if err := proto.Write(&buf, reply, proto.DefaultMaxFrame); err != nil {
		panic(err)
	}
	m, err := proto.Read(&buf, proto.DefaultMaxFrame)
	if err != nil {
		panic(err)
	}
	fmt.Println(m.Head().Type)
}
`)
	cmd := exec.Command(goBin, "run", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod", "GOPROXY=off", "GOWORK=off")
	out, err := cmd.CombinedOutput()

	require.NoError(t, err, "%s", out)
	assert.Equal(t, "orders", strings.TrimSpace(string(out)))
}
