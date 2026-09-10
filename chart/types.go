package chart

import (
	"time"

	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
)

// Format selects a renderer's output encoding.
type Format string

const (
	// FormatPNG renders a rasterized PNG image.
	FormatPNG Format = "png"
	// FormatSVG renders a vector SVG image.
	FormatSVG Format = "svg"
)

// vgFormat returns format's own gonum.org/v1/plot format string —
// the one place that vendor-specific string ever appears, so every
// other file in this package works with the Format type only.
func (f Format) vgFormat() (string, error) {
	switch f {
	case FormatPNG:
		return "png", nil
	case FormatSVG:
		return "svg", nil
	default:
		return "", errUnsupportedFormat(f)
	}
}

// Size is a chart's own physical output size, in inches — a plain
// Trader-owned value rather than exposing gonum.org/v1/plot's own
// vg.Length directly (see doc.go's own "do not expose vendor SDK
// types" note).
type Size struct {
	WidthInches, HeightInches float64
}

// LevelPoint is one (time, price) sample of a continuous or
// step-wise price-level series — an indicator value (for example
// SMA200) or a resting stop level (probation or trailing) at one
// point in time.
type LevelPoint struct {
	Time  time.Time
	Price num.Price
}

// Series is one named LevelPoint series plotted as a line — used for
// an indicator overlay (SMA) or a resting stop level (probation,
// trailing) on an overview or episode chart.
type Series struct {
	Name   string
	Points []LevelPoint
}

// MarkerKind identifies what a Marker represents on a chart.
type MarkerKind string

const (
	// MarkerEntry is a position-opening fill.
	MarkerEntry MarkerKind = "entry"
	// MarkerExit is a position-closing fill (a normal intraday stop,
	// a gap-through stop, or a direct SMA-cross exit — the marker
	// itself does not distinguish which; use Marker.Label for that).
	MarkerExit MarkerKind = "exit"
	// MarkerReentry is a fresh entry following a prior exit —
	// plotted distinctly from MarkerEntry so a chart can visually
	// answer "did this episode's re-entry happen sooner or later
	// than a fresh SMA cross would have" (issue #354's own question).
	MarkerReentry MarkerKind = "reentry"
	// MarkerTrough is the lowest price observed during a flat period
	// following an exit, before the next entry/re-entry — how far
	// price continued adversely after the strategy exited.
	MarkerTrough MarkerKind = "trough"
)

// Marker is one labeled (time, price) point plotted on a chart.
type Marker struct {
	Time  time.Time
	Price num.Price
	Kind  MarkerKind
	// Label is an optional short annotation drawn near the marker —
	// for example "probation stop" or "gap-through" — distinct from
	// Kind, which only ever selects the marker's own glyph/color.
	Label string
}

// OverviewInput is one full backtest run's own chart: the complete
// bar series, an optional single indicator overlay (SMA), and every
// entry/exit/re-entry marker across the whole run — issue #355's
// "full-run overview" chart type, revealing structural misses, long
// flat periods, and regime behavior over the whole run.
type OverviewInput struct {
	Title string
	// Bars is plotted as a Close-price line (issue #355 does not
	// require full OHLC/candlestick rendering; see doc.go's own
	// "deliberately the smallest renderer" note). Must be sorted by
	// Time, ascending, and non-empty.
	Bars []marketdata.Bar
	// SMA is an optional single indicator overlay; nil or empty means
	// no overlay is drawn.
	SMA []LevelPoint
	// Markers may be empty (a run with no completed trade yet still
	// produces a valid, marker-free overview chart).
	Markers []Marker
}

// EpisodeInput is one exit/re-entry episode's own bounded-window
// chart — issue #355's own primary visualization for issue #354's
// post-exit/whipsaw analysis: roughly 60 bars before the exit, the
// complete flat period, and 30-60 bars after re-entry (the exact
// window is the caller's own choice; this package only renders
// whatever Bars it is given).
type EpisodeInput struct {
	Title string
	// Bars is plotted as a Close-price line, the same as
	// OverviewInput.Bars. Must be sorted by Time, ascending, and
	// non-empty.
	Bars []marketdata.Bar
	// SMA, ProbationStop, and TrailingStop are each optional; nil or
	// empty means that overlay is not drawn. All three may be present
	// at once (a probation-trend episode transitioning phases within
	// the window).
	SMA           []LevelPoint
	ProbationStop []LevelPoint
	TrailingStop  []LevelPoint
	// Markers typically include the episode's own entry, exit,
	// post-exit trough, and re-entry, in that chronological order,
	// but this package does not require or validate any particular
	// set.
	Markers []Marker
}

// EquityPoint is one (time, value) sample of an equity curve.
// Deliberately a plain float64, not num.Price or num.Money: an
// account's equity is denominated in num.Money, and gluing a Money
// value into the same LevelPoint type SMA/stop-price series use would
// let a chart silently plot a currency amount on axes and overlays
// meant for a market price — exactly the "accidental use of a price
// as ... money" ADR-004 introduced separate exact types to prevent.
// This package is a display/research tier (like indicator's own
// analytical float64 convention), so the caller converts its own
// authoritative num.Money equity value to float64 explicitly (for
// example via Money.String() and strconv.ParseFloat, mirroring
// smatrend_probation_simple_fullarchive_test.go's own
// mustParseFloatWF/fieldsFirst pattern) before building an
// EquityPoint — never a silent, ADR-045-style sanctioned conversion,
// since Money has no such boundary method today.
type EquityPoint struct {
	Time  time.Time
	Value float64
}

// EquityInput is a strategy's own equity curve, optionally compared
// against a buy-and-hold series over the identical span — issue
// #355's "equity curve" chart type.
type EquityInput struct {
	Title string
	// Equity must be sorted by Time, ascending, and non-empty.
	Equity []EquityPoint
	// BuyAndHold is optional; nil or empty means no comparison series
	// is drawn. When present, it is drawn on the same value axis as
	// Equity — the caller is responsible for making the two series
	// genuinely comparable (for example both starting from the same
	// notional capital), the same discipline
	// smatrend_probation_simple_fullarchive_test.go's own buy-and-hold
	// comparison already established; this package performs no
	// normalization of its own.
	BuyAndHold []EquityPoint
}
