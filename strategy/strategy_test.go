package strategy

import (
	"encoding/json"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/rustyeddy/trader/marketdata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The transport must not import the engine (package trader, the module root), or
// the engine could not use it.
func TestPackage_DoesNotImportTheEngine(t *testing.T) {
	files, err := filepath.Glob("*.go")
	require.NoError(t, err)
	require.NotEmpty(t, files)
	for _, name := range files {
		f, err := parser.ParseFile(token.NewFileSet(), name, nil, parser.ImportsOnly)
		require.NoError(t, err)
		for _, imp := range f.Imports {
			path, err := strconv.Unquote(imp.Path.Value)
			require.NoError(t, err)
			assert.NotEqual(t, "github.com/rustyeddy/trader", path, name)
		}
	}
}

func TestRunRequest_Validate(t *testing.T) {
	good := RunRequest{Instruments: []string{"EURUSD"}, Interval: "h1", From: utc(2024, 1), To: utc(2024, 2)}
	require.NoError(t, good.Validate())

	for name, mutate := range map[string]func(*RunRequest){
		"no instruments":      func(r *RunRequest) { r.Instruments = nil },
		"an empty instrument": func(r *RunRequest) { r.Instruments = []string{"EURUSD", ""} },
		"no interval":         func(r *RunRequest) { r.Interval = "" },
		"no from":             func(r *RunRequest) { r.From = time.Time{} },
		"no to":               func(r *RunRequest) { r.To = time.Time{} },
		"from after to":       func(r *RunRequest) { r.From, r.To = r.To, r.From },
		"from equals to":      func(r *RunRequest) { r.To = r.From },
	} {
		t.Run(name, func(t *testing.T) {
			r := good
			mutate(&r)
			assert.Error(t, r.Validate())
		})
	}
}

// The messages are what the package doc says they are, byte for byte.
func TestMessages_WireShape(t *testing.T) {
	req := RunRequest{Instruments: []string{"EURUSD"}, Interval: "h1", From: utc(2024, 1), To: utc(2024, 4)}
	for want, v := range map[string]any{
		`{"run":{"instruments":["EURUSD"],"interval":"h1","from":"2024-01-01T00:00:00Z","to":"2024-04-01T00:00:00Z"}}`: runMessage{Run: req},
		`{"accepted":{"run_id":"run_X"}}`: startMessage{Accepted: &acceptedRun{RunID: "run_X"}},
		`{"error":"no"}`:                  startMessage{Error: "no"},
		`{"result":{"run_id":"run_X","status":"completed","summary":{"bars":3}}}`: resultMessage{Result: Result{RunID: "run_X", Status: StatusCompleted, Summary: []byte(`{"bars":3}`)}},
		`{"result":{"run_id":"run_X","status":"failed","error":"boom"}}`:          resultMessage{Result: Result{RunID: "run_X", Status: StatusFailed, Error: "boom"}},
		`{"bars":[],"events":[]}`: barsMessage{Bars: []marketdata.Bar{}, Events: []Event{}},
	} {
		got, err := json.Marshal(v)
		require.NoError(t, err)
		assert.JSONEq(t, want, string(got))
	}
}

func utc(y int, m time.Month) time.Time { return time.Date(y, m, 1, 0, 0, 0, 0, time.UTC) }
