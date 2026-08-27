package execution

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/pipeline"
)

func TestResolveFormatter(t *testing.T) {
	table, err := resolveFormatter(formatTable)
	require.NoError(t, err)
	require.IsType(t, tableFormatter{}, table)

	jsonFmt, err := resolveFormatter(formatJSON)
	require.NoError(t, err)
	require.IsType(t, jsonFormatter{}, jsonFmt)

	_, err = resolveFormatter("xml")
	require.ErrorContains(t, err, "invalid --format")
}

func TestIsRejected(t *testing.T) {
	require.True(t, isRejected(pipeline.ErrRejected))
	require.False(t, isRejected(errors.New("wrapped: "+pipeline.ErrRejected.Error())),
		"a lookalike message must not be mistaken for the real sentinel")
	require.False(t, isRejected(errors.New("some other error")))
	require.False(t, isRejected(nil))
}
