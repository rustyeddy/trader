package broker

import (
	"testing"

	"github.com/stretchr/testify/require"
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
