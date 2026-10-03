package data

import (
	"bytes"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	svc "github.com/rustyeddy/trader/internal/service/marketdata"
)

func TestSingleResult(t *testing.T) {
	boom := errors.New("boom")

	_, err := singleResult(svc.DatasetsResponse{}, boom)
	assert.ErrorIs(t, err, boom, "a call-level failure with no results")

	_, err = singleResult(svc.DatasetsResponse{Results: make([]svc.DatasetResult, 2)}, nil)
	assert.ErrorContains(t, err, "expected one dataset result")

	res, err := singleResult(svc.DatasetsResponse{Results: []svc.DatasetResult{{Status: svc.DatasetBuilt}}}, boom)
	require.NoError(t, err)
	assert.ErrorIs(t, res.Err, boom, "a call-level error after the result attaches to it")
	assert.Equal(t, svc.DatasetBuilt, res.Status)
}

func TestFormatDatasetResult(t *testing.T) {
	table, err := resolveFormatter(formatTable)
	require.NoError(t, err)

	var out bytes.Buffer
	res := svc.DatasetResult{Request: svc.InstrumentRequest{Symbol: "SPY"}, Status: svc.DatasetCurrent}
	require.NoError(t, formatDatasetResult(&out, table, res))
	assert.Equal(t, "SPY: current\n", out.String(), "nothing to show but the status")

	out.Reset()
	boom := errors.New("boom")
	jsonFormat, err := resolveFormatter(formatJSON)
	require.NoError(t, err)
	failed := svc.DatasetResult{Status: svc.DatasetFailed, Err: boom, Build: &svc.BuildResponse{}}
	assert.ErrorIs(t, formatDatasetResult(&out, jsonFormat, failed), boom)
	assert.NotEmpty(t, out.String(), "a failed build still prints its partial progress")

	out.Reset()
	failedUpdate := svc.DatasetResult{Status: svc.DatasetFailed, Err: boom, Update: &svc.UpdateResponse{}}
	assert.ErrorIs(t, formatDatasetResult(&out, jsonFormat, failedUpdate), boom)
	assert.NotEmpty(t, out.String(), "a failed update prints its progress")

	out.Reset()
	converted := svc.DatasetResult{Status: svc.DatasetBuilt, Convert: &svc.ConvertResponse{}}
	require.NoError(t, formatDatasetResult(&out, jsonFormat, converted))
	assert.NotEmpty(t, out.String(), "an archive conversion renders its build")

	out.Reset()
	assert.ErrorIs(t, formatDatasetResult(&out, table, svc.DatasetResult{Status: svc.DatasetFailed, Err: boom}), boom)
	assert.Empty(t, out.String(), "a failure with no step prints nothing")
}
