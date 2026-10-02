package marketdata

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCtxReaderStopsOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	r := ctxReader{ctx: ctx, r: strings.NewReader("abcdef")}

	buf := make([]byte, 3)
	n, err := r.Read(buf)
	require.NoError(t, err)
	assert.Equal(t, "abc", string(buf[:n]))

	cancel()
	n, err = r.Read(buf)
	assert.Zero(t, n)
	assert.ErrorIs(t, err, context.Canceled)
	_, err = io.Copy(io.Discard, r)
	assert.ErrorIs(t, err, context.Canceled)
}
