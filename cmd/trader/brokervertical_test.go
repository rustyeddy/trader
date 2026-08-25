package main

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// runBroker executes newRootCmd()'s tree with args prefixed by
// "broker", returning stdout and any error -- the same real-command-
// tree harness runData uses for the data group (datacmd_test.go),
// driving the actual Cobra tree rather than calling package functions
// directly.
func runBroker(t *testing.T, args ...string) (string, error) {
	t.Helper()
	root, cleanup := newRootCmd()
	defer func() { _ = cleanup() }()

	full := append([]string{"broker"}, args...)
	root.SetArgs(full)

	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(new(discard))

	err := root.ExecuteContext(context.Background())
	return out.String(), err
}

// TestBrokerVerticalSlice is issue #155's own primary deliverable: one
// coherent proof that the full
//
//	trader (Cobra command) -> service/broker -> adapters/broker/sim
//
// path works end to end, driven entirely through the real command
// tree, mirroring TestM25VerticalSlice's own role for the data group.
func TestBrokerVerticalSlice(t *testing.T) {
	t.Run("accounts lists the one freshly built account", func(t *testing.T) {
		out, err := runBroker(t, "accounts",
			"--starting-cash", "10000", "--currency", "USD")
		require.NoError(t, err)
		require.Contains(t, out, "broker=sim")
	})

	t.Run("snapshot shows a freshly funded, flat account", func(t *testing.T) {
		out, err := runBroker(t, "snapshot",
			"--starting-cash", "10000", "--currency", "USD", "--format", "json")
		require.NoError(t, err)

		var decoded struct {
			Cash      string `json:"cash"`
			Positions []any  `json:"positions"`
		}
		require.NoError(t, json.Unmarshal([]byte(out), &decoded))
		require.Equal(t, "10000 USD", decoded.Cash)
		require.Empty(t, decoded.Positions)
	})

	t.Run("submit fills a market order and reports it filled", func(t *testing.T) {
		out, err := runBroker(t, "submit",
			"--symbol", "EURUSD", "--side", "buy", "--quantity", "1000",
			"--price", "1.10000", "--format", "json")
		require.NoError(t, err)

		var decoded struct {
			Status    string `json:"status"`
			FilledQty string `json:"filled_quantity"`
		}
		require.NoError(t, json.Unmarshal([]byte(out), &decoded))
		require.Equal(t, "filled", decoded.Status)
		require.Equal(t, "1000", decoded.FilledQty)
	})

	t.Run("submit accepts a limit order as working, without filling it", func(t *testing.T) {
		out, err := runBroker(t, "submit",
			"--symbol", "EURUSD", "--side", "buy", "--type", "limit",
			"--quantity", "100", "--limit-price", "1.05000", "--format", "json")
		require.NoError(t, err)

		var decoded struct {
			Status    string `json:"status"`
			FilledQty string `json:"filled_quantity"`
		}
		require.NoError(t, json.Unmarshal([]byte(out), &decoded))
		require.Equal(t, "working", decoded.Status)
		require.Equal(t, "0", decoded.FilledQty)
	})

	t.Run("submit rejects a market order missing --price", func(t *testing.T) {
		_, err := runBroker(t, "submit",
			"--symbol", "EURUSD", "--side", "buy", "--quantity", "100")
		require.ErrorContains(t, err, "--price is required")
	})

	t.Run("submit rejects an invalid --symbol", func(t *testing.T) {
		_, err := runBroker(t, "submit",
			"--symbol", "EUR", "--side", "buy", "--quantity", "100", "--price", "1.1")
		require.ErrorContains(t, err, "6-letter FX pair symbol")
	})

	t.Run("an already-cancelled context is rejected before any broker construction", func(t *testing.T) {
		root, cleanup := newRootCmd()
		defer func() { _ = cleanup() }()
		root.SetArgs([]string{"broker", "accounts"})
		root.SetOut(new(discard))
		root.SetErr(new(discard))

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err := root.ExecuteContext(ctx)
		require.ErrorIs(t, err, context.Canceled)
	})
}
