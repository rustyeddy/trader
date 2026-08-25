package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolveBrokerFormatter(t *testing.T) {
	table, err := resolveBrokerFormatter(formatTable)
	require.NoError(t, err)
	require.IsType(t, brokerTableFormatter{}, table)

	jsonFmt, err := resolveBrokerFormatter(formatJSON)
	require.NoError(t, err)
	require.IsType(t, brokerJSONFormatter{}, jsonFmt)

	_, err = resolveBrokerFormatter("xml")
	require.ErrorContains(t, err, "invalid --format")
}
