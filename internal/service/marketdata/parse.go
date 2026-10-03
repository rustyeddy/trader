package marketdata

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/rustyeddy/trader/marketdata"
)

var (
	// ErrInvalidInterval reports an interval name ParseInterval does not
	// know.
	ErrInvalidInterval = errors.New("invalid interval")
	// ErrInvalidDate reports a date ParseDate cannot read.
	ErrInvalidDate = errors.New("invalid date")
)

// intervalsByName is the transports' shared string vocabulary for
// marketdata.Interval (ADR-069: the CLI and MCP parse the same names the
// same way). It is deliberately separate from Interval.String(), which is
// display-only and never parsed in core code (ADR-012): naming a fixed set
// of predefined intervals is a transport-facing vocabulary, and the
// service layer is where transports share it.
var intervalsByName = map[string]marketdata.Interval{
	"M1": marketdata.M1,
	"H1": marketdata.H1,
	"H4": marketdata.H4,
	"D1": marketdata.D1,
	"W1": marketdata.W1,
}

// IntervalNames lists the names ParseInterval accepts.
const IntervalNames = "M1, H1, H4, D1, W1"

// ParseInterval reads an interval name (M1, H1, H4, D1, or W1,
// case-insensitive), failing with ErrInvalidInterval.
func ParseInterval(s string) (marketdata.Interval, error) {
	iv, ok := intervalsByName[strings.ToUpper(strings.TrimSpace(s))]
	if !ok {
		return marketdata.Interval{}, fmt.Errorf("%w %q: expected one of %s", ErrInvalidInterval, s, IntervalNames)
	}
	return iv, nil
}

// ParseDate reads a bare date (UTC midnight) or an RFC3339 timestamp, in
// UTC, failing with ErrInvalidDate.
func ParseDate(s string) (time.Time, error) {
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t.UTC(), nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC(), nil
	}
	return time.Time{}, fmt.Errorf("%w %q: expected YYYY-MM-DD or RFC3339", ErrInvalidDate, s)
}

// ParseRange reads an optional from/to pair into a half-open range: both
// empty is the zero range (the operation's default), one alone fails with
// ErrInvalidRequest, and both must parse with to after from.
func ParseRange(from, to string) (marketdata.TimeRange, error) {
	if from == "" && to == "" {
		return marketdata.TimeRange{}, nil
	}
	if from == "" || to == "" {
		return marketdata.TimeRange{}, fmt.Errorf("%w: from and to must be given together", ErrInvalidRequest)
	}
	start, err := ParseDate(from)
	if err != nil {
		return marketdata.TimeRange{}, err
	}
	end, err := ParseDate(to)
	if err != nil {
		return marketdata.TimeRange{}, err
	}
	rng, err := marketdata.NewTimeRange(start, end)
	if err != nil {
		return marketdata.TimeRange{}, fmt.Errorf("%w: invalid range: %v", ErrInvalidRequest, err)
	}
	return rng, nil
}
