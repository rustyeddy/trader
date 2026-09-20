package chart

import (
	"errors"
	"fmt"
)

// ErrEmptyBars reports that an Input's own Bars field was empty — a
// chart with no price series to draw is never a valid, silently
// degraded chart; it is an explicit error.
var ErrEmptyBars = errors.New("chart: bars must not be empty")

// ErrEmptyEquity is ErrEmptyBars' own EquityInput equivalent.
var ErrEmptyEquity = errors.New("chart: equity series must not be empty")

// ErrUnsupportedFormat reports a Format value that is neither
// FormatPNG nor FormatSVG.
var ErrUnsupportedFormat = errors.New("chart: unsupported format")

func errUnsupportedFormat(f Format) error {
	return fmt.Errorf("%w: %q", ErrUnsupportedFormat, string(f))
}
