package chart

import (
	"bytes"
	"testing"

	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func sampleEpisodeInput(t *testing.T) EpisodeInput {
	t.Helper()
	bars := episodeBars(t)
	sma := []LevelPoint{
		{Time: bars[0].Time, Price: num.MustParsePrice("100")},
		{Time: bars[4].Time, Price: num.MustParsePrice("102")},
	}
	probationStop := []LevelPoint{
		{Time: bars[1].Time, Price: num.MustParsePrice("99")},
		{Time: bars[2].Time, Price: num.MustParsePrice("99.5")},
	}
	trailingStop := []LevelPoint{
		{Time: bars[3].Time, Price: num.MustParsePrice("103")},
		{Time: bars[4].Time, Price: num.MustParsePrice("103.5")},
	}
	markers := []Marker{
		{Time: bars[0].Time, Price: num.MustParsePrice("100"), Kind: MarkerEntry},
		{Time: bars[2].Time, Price: num.MustParsePrice("99.5"), Kind: MarkerExit},
		{Time: bars[3].Time, Price: num.MustParsePrice("98"), Kind: MarkerTrough},
		{Time: bars[4].Time, Price: num.MustParsePrice("102"), Kind: MarkerReentry},
	}
	return EpisodeInput{
		Title: "Test Episode", Bars: bars, SMA: sma,
		ProbationStop: probationStop, TrailingStop: trailingStop, Markers: markers,
	}
}

func episodeBars(t *testing.T) []marketdata.Bar {
	t.Helper()
	return []marketdata.Bar{
		mustBar(t, 0, "100", "101", "99", "100"),
		mustBar(t, 1, "101", "103", "100", "102"),
		mustBar(t, 2, "102", "106", "101", "105"),
		mustBar(t, 3, "105", "107", "103", "104"),
		mustBar(t, 4, "104", "105", "98", "99"),
	}
}

func TestRenderEpisode_PNGAndSVGSucceedWithAllOverlays(t *testing.T) {
	in := sampleEpisodeInput(t)

	var png bytes.Buffer
	require.NoError(t, RenderEpisode(&png, in, FormatPNG))
	assert.NotEmpty(t, png.Bytes())

	var svg bytes.Buffer
	require.NoError(t, RenderEpisode(&svg, in, FormatSVG))
	assert.Contains(t, svg.String(), "<svg")
}

func TestRenderEpisode_DeterministicAcrossRuns(t *testing.T) {
	in := sampleEpisodeInput(t)
	var first, second bytes.Buffer
	require.NoError(t, RenderEpisode(&first, in, FormatPNG))
	require.NoError(t, RenderEpisode(&second, in, FormatPNG))
	assert.Equal(t, first.Bytes(), second.Bytes(), "identical EpisodeInput must render byte-identical PNG output")

	first.Reset()
	second.Reset()
	require.NoError(t, RenderEpisode(&first, in, FormatSVG))
	require.NoError(t, RenderEpisode(&second, in, FormatSVG))
	assert.Equal(t, first.Bytes(), second.Bytes(), "identical EpisodeInput must render byte-identical SVG output")
}

func TestRenderEpisode_RejectsEmptyBars(t *testing.T) {
	var buf bytes.Buffer
	err := RenderEpisode(&buf, EpisodeInput{}, FormatPNG)
	require.ErrorIs(t, err, ErrEmptyBars)
}

func TestRenderEpisode_WorksWithNoStopOverlaysAtAll(t *testing.T) {
	in := sampleEpisodeInput(t)
	in.ProbationStop = nil
	in.TrailingStop = nil
	in.SMA = nil
	var buf bytes.Buffer
	require.NoError(t, RenderEpisode(&buf, in, FormatPNG))
	assert.NotEmpty(t, buf.Bytes())
}

// TestRenderEpisode_MarkerLabelIsActuallyRendered proves Marker.Label
// is not merely a documented-but-ignored field (PR #358 review):
// SVG's own text-based format lets this be checked directly, rather
// than merely trusting that addMarkers's own label-drawing code path
// executed without error.
func TestRenderEpisode_MarkerLabelIsActuallyRendered(t *testing.T) {
	in := sampleEpisodeInput(t)
	in.Markers = []Marker{
		{Time: in.Bars[2].Time, Price: in.Bars[2].Close, Kind: MarkerExit, Label: "gap-through stop"},
	}
	var buf bytes.Buffer
	require.NoError(t, RenderEpisode(&buf, in, FormatSVG))
	assert.Contains(t, buf.String(), "gap-through stop")
}
