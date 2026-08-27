package execution_test

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/cmd/trader/internal/rootcmd"
)

// discard is a minimal io.Writer that keeps test output out of `go
// test -v`'s own output without importing io/ioutil or os for a
// throwaway /dev/null handle.
type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }

// runExecution executes rootcmd.New()'s tree with args prefixed by
// "execution", returning stdout and any error -- the same
// real-command-tree harness cmd/trader/broker's own runBroker uses.
func runExecution(t *testing.T, args ...string) (string, error) {
	t.Helper()
	root, cleanup := rootcmd.New()
	defer func() { _ = cleanup() }()

	full := append([]string{"execution"}, args...)
	root.SetArgs(full)

	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(new(discard))

	err := root.ExecuteContext(context.Background())
	return out.String(), err
}

// TestExecutionVerticalSlice is issue #187's own primary deliverable:
// one coherent proof that the full
//
//	trader (Cobra command) -> service/execution -> pipeline.Pipeline
//	   -> execution.Planner / risk.Engine / risk.Sizer -> adapters/broker/sim
//
// path works end to end for both the read-only Evaluate and the
// mutating Submit use case, driven entirely through the real command
// tree, mirroring TestBrokerVerticalSlice's own role for the broker
// group.
func TestExecutionVerticalSlice(t *testing.T) {
	intentFlags := []string{
		"--symbol", "EURUSD", "--side", "buy",
		"--risk-fraction", "0.01", "--adverse-distance", "0.01000",
	}

	t.Run("evaluate shows an approved proposal, decision, and request", func(t *testing.T) {
		out, err := runExecution(t, append([]string{"evaluate", "--format", "json"}, intentFlags...)...)
		require.NoError(t, err)

		var decoded struct {
			Proposal struct {
				Instrument string `json:"instrument"`
				Side       string `json:"side"`
			} `json:"proposal"`
			Decision struct {
				Allowed bool `json:"allowed"`
			} `json:"decision"`
			Request struct {
				OrderID string `json:"order_id"`
			} `json:"request"`
		}
		require.NoError(t, json.Unmarshal([]byte(out), &decoded))
		require.Equal(t, "EURUSD", decoded.Proposal.Instrument)
		require.True(t, decoded.Decision.Allowed)
		require.NotEmpty(t, decoded.Request.OrderID)
	})

	t.Run("evaluate table format shows the proposal, decision, and request", func(t *testing.T) {
		out, err := runExecution(t, append([]string{"evaluate"}, intentFlags...)...)
		require.NoError(t, err)
		require.Contains(t, out, "instrument=EURUSD")
		require.Contains(t, out, "allowed=true")
		require.Contains(t, out, "violations: (none)")
		require.Contains(t, out, "request: order_id=")
	})

	t.Run("submit table format shows the resulting order", func(t *testing.T) {
		out, err := runExecution(t, append([]string{"submit", "--price", "1.10000"}, intentFlags...)...)
		require.NoError(t, err)
		require.Contains(t, out, "instrument=EURUSD")
		require.Contains(t, out, "order: broker_order_id=")
		require.Contains(t, out, "status=filled")
	})

	t.Run("submit shows the resulting order and matches evaluate's own request shape", func(t *testing.T) {
		out, err := runExecution(t, append([]string{"submit", "--price", "1.10000", "--format", "json"}, intentFlags...)...)
		require.NoError(t, err)

		var decoded struct {
			Decision struct {
				Allowed bool `json:"allowed"`
			} `json:"decision"`
			Request struct {
				OrderID string `json:"order_id"`
			} `json:"request"`
			Order struct {
				Status string `json:"status"`
			} `json:"order"`
		}
		require.NoError(t, json.Unmarshal([]byte(out), &decoded))
		require.True(t, decoded.Decision.Allowed)
		require.NotEmpty(t, decoded.Request.OrderID)
		require.Equal(t, "filled", decoded.Order.Status)
	})

	t.Run("submit without --price fails before any broker interaction", func(t *testing.T) {
		_, err := runExecution(t, append([]string{"submit"}, intentFlags...)...)
		require.ErrorContains(t, err, "--price")
	})

	t.Run("evaluate rejects an unsupported --side", func(t *testing.T) {
		_, err := runExecution(t, "evaluate", "--symbol", "EURUSD", "--side", "sideways",
			"--risk-fraction", "0.01", "--adverse-distance", "0.01000")
		require.ErrorContains(t, err, "invalid --side")
	})

	t.Run("a context already cancelled before ExecuteContext is rejected before any service call", func(t *testing.T) {
		root, cleanup := rootcmd.New()
		defer func() { _ = cleanup() }()
		root.SetArgs(append([]string{"execution", "evaluate"}, intentFlags...))
		root.SetOut(new(discard))
		root.SetErr(new(discard))

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err := root.ExecuteContext(ctx)
		require.ErrorIs(t, err, context.Canceled)
	})
}
