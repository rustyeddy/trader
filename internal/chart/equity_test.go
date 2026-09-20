package chart

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func sampleEquityInput(t *testing.T) EquityInput {
	t.Helper()
	equity := []EquityPoint{
		{Time: testStart, Value: 100000},
		{Time: testStart.AddDate(0, 0, 1), Value: 101500},
		{Time: testStart.AddDate(0, 0, 2), Value: 99800},
		{Time: testStart.AddDate(0, 0, 3), Value: 103200},
	}
	buyHold := []EquityPoint{
		{Time: testStart, Value: 100000},
		{Time: testStart.AddDate(0, 0, 1), Value: 100800},
		{Time: testStart.AddDate(0, 0, 2), Value: 99500},
		{Time: testStart.AddDate(0, 0, 3), Value: 102000},
	}
	return EquityInput{Title: "Test Equity", Equity: equity, BuyAndHold: buyHold}
}

func TestRenderEquity_PNGAndSVGSucceedWithBuyAndHold(t *testing.T) {
	in := sampleEquityInput(t)

	var png bytes.Buffer
	require.NoError(t, RenderEquity(&png, in, FormatPNG))
	assert.NotEmpty(t, png.Bytes())

	var svg bytes.Buffer
	require.NoError(t, RenderEquity(&svg, in, FormatSVG))
	assert.Contains(t, svg.String(), "<svg")
}

func TestRenderEquity_WorksWithoutBuyAndHold(t *testing.T) {
	in := sampleEquityInput(t)
	in.BuyAndHold = nil
	var buf bytes.Buffer
	require.NoError(t, RenderEquity(&buf, in, FormatPNG))
	assert.NotEmpty(t, buf.Bytes())
}

func TestRenderEquity_DeterministicAcrossRuns(t *testing.T) {
	in := sampleEquityInput(t)
	var first, second bytes.Buffer
	require.NoError(t, RenderEquity(&first, in, FormatPNG))
	require.NoError(t, RenderEquity(&second, in, FormatPNG))
	assert.Equal(t, first.Bytes(), second.Bytes(), "identical EquityInput must render byte-identical PNG output")

	first.Reset()
	second.Reset()
	require.NoError(t, RenderEquity(&first, in, FormatSVG))
	require.NoError(t, RenderEquity(&second, in, FormatSVG))
	assert.Equal(t, first.Bytes(), second.Bytes(), "identical EquityInput must render byte-identical SVG output")
}

func TestRenderEquity_RejectsEmptyEquity(t *testing.T) {
	var buf bytes.Buffer
	err := RenderEquity(&buf, EquityInput{}, FormatPNG)
	require.ErrorIs(t, err, ErrEmptyEquity)
}
