package strategy

import (
	"context"
	"go/parser"
	"go/token"
	"net"
	"path/filepath"
	"strconv"
	"testing"

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

// A real Client against Serve over loopback TCP.
func TestClientAndServe(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = ln.Close() }()

	groups := 0
	done := make(chan error, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			done <- err
			return
		}
		defer func() { _ = conn.Close() }()
		done <- Serve(conn, func([]marketdata.Bar, []Event) ([]Order, error) { groups++; return nil, nil })
	}()

	r, err := Dial(ln.Addr().String())
	require.NoError(t, err)
	for i := 0; i < 5; i++ {
		_, err := r.OnBars(context.Background(), oneBar, nil)
		require.NoError(t, err)
	}
	require.NoError(t, r.Close())
	require.NoError(t, <-done)
	assert.Equal(t, 5, groups)
}
