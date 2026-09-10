package chart

import (
	"bytes"
	"image/png"
	"testing"
	"time"

	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var testStart = time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC)

func mustBar(t *testing.T, dayOffset int, o, h, l, c string) marketdata.Bar {
	t.Helper()
	return marketdata.Bar{
		Time:  testStart.AddDate(0, 0, dayOffset),
		Open:  num.MustParsePrice(o),
		High:  num.MustParsePrice(h),
		Low:   num.MustParsePrice(l),
		Close: num.MustParsePrice(c),
	}
}

func sampleOverviewInput(t *testing.T) OverviewInput {
	t.Helper()
	bars := []marketdata.Bar{
		mustBar(t, 0, "100", "101", "99", "100"),
		mustBar(t, 1, "101", "103", "100", "102"),
		mustBar(t, 2, "102", "106", "101", "105"),
		mustBar(t, 3, "105", "107", "103", "104"),
		mustBar(t, 4, "104", "105", "98", "99"),
	}
	sma := []LevelPoint{
		{Time: bars[0].Time, Price: num.MustParsePrice("100")},
		{Time: bars[2].Time, Price: num.MustParsePrice("101")},
		{Time: bars[4].Time, Price: num.MustParsePrice("102")},
	}
	markers := []Marker{
		{Time: bars[1].Time, Price: num.MustParsePrice("101"), Kind: MarkerEntry},
		{Time: bars[3].Time, Price: num.MustParsePrice("104"), Kind: MarkerExit},
	}
	return OverviewInput{Title: "Test Overview", Bars: bars, SMA: sma, Markers: markers}
}

func TestRenderOverview_PNGProducesValidImageAtDefaultSize(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, RenderOverview(&buf, sampleOverviewInput(t), FormatPNG))

	img, err := png.Decode(bytes.NewReader(buf.Bytes()))
	require.NoError(t, err)
	b := img.Bounds()
	// gonum.org/v1/plot's own default raster DPI (96, confirmed
	// directly rather than assumed) — width/height in pixels should
	// match the requested inch size at that DPI.
	const dpi = 96
	assert.InDelta(t, DefaultOverviewSize.WidthInches*dpi, float64(b.Dx()), 3)
	assert.InDelta(t, DefaultOverviewSize.HeightInches*dpi, float64(b.Dy()), 3)
}

func TestRenderOverview_SVGProducesNonEmptyVectorOutput(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, RenderOverview(&buf, sampleOverviewInput(t), FormatSVG))
	out := buf.String()
	assert.Contains(t, out, "<svg", "SVG output must contain an <svg> root element")
	assert.NotEmpty(t, out)
}

func TestRenderOverview_DeterministicAcrossRuns(t *testing.T) {
	in := sampleOverviewInput(t)

	var first, second bytes.Buffer
	require.NoError(t, RenderOverview(&first, in, FormatPNG))
	require.NoError(t, RenderOverview(&second, in, FormatPNG))
	assert.Equal(t, first.Bytes(), second.Bytes(), "identical OverviewInput must render byte-identical PNG output")

	first.Reset()
	second.Reset()
	require.NoError(t, RenderOverview(&first, in, FormatSVG))
	require.NoError(t, RenderOverview(&second, in, FormatSVG))
	assert.Equal(t, first.Bytes(), second.Bytes(), "identical OverviewInput must render byte-identical SVG output")
}

func TestRenderOverview_RejectsEmptyBars(t *testing.T) {
	var buf bytes.Buffer
	err := RenderOverview(&buf, OverviewInput{Title: "empty"}, FormatPNG)
	require.ErrorIs(t, err, ErrEmptyBars)
}

func TestRenderOverview_RejectsUnsupportedFormat(t *testing.T) {
	var buf bytes.Buffer
	err := RenderOverview(&buf, sampleOverviewInput(t), Format("gif"))
	require.ErrorIs(t, err, ErrUnsupportedFormat)
}

func TestRenderOverview_WorksWithoutSMAOrMarkers(t *testing.T) {
	in := sampleOverviewInput(t)
	in.SMA = nil
	in.Markers = nil
	var buf bytes.Buffer
	require.NoError(t, RenderOverview(&buf, in, FormatPNG))
	assert.NotEmpty(t, buf.Bytes())
}

// TestRenderOverview_UnknownMarkerKindStillRendersWithADefaultStyle
// proves markerStyle's own default branch (an unrecognized
// MarkerKind, not one of the four this package defines) still
// produces a valid chart rather than a distinguishing panic or
// silently-dropped marker — a caller mistyping a Kind string gets a
// visibly-plotted, if unstyled, marker instead of losing data.
func TestRenderOverview_UnknownMarkerKindStillRendersWithADefaultStyle(t *testing.T) {
	in := sampleOverviewInput(t)
	in.Markers = append(in.Markers, Marker{Time: in.Bars[2].Time, Price: num.MustParsePrice("105"), Kind: MarkerKind("unknown")})
	var buf bytes.Buffer
	require.NoError(t, RenderOverview(&buf, in, FormatPNG))
	assert.NotEmpty(t, buf.Bytes())
}
