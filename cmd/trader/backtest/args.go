package backtest

import (
	"fmt"
	"strings"
	"time"

	"github.com/rustyeddy/trader/marketdata"
)

// intervalsByName is the CLI's own string vocabulary for
// marketdata.Interval, matching cmd/trader/data's own
// intervalsByName — duplicated rather than shared (issue #201: each
// CLI command family package is deliberately independent).
var intervalsByName = map[string]marketdata.Interval{
	"M1": marketdata.M1,
	"H1": marketdata.H1,
	"H4": marketdata.H4,
	"D1": marketdata.D1,
	"W1": marketdata.W1,
}

func parseInterval(s string) (marketdata.Interval, error) {
	iv, ok := intervalsByName[strings.ToUpper(strings.TrimSpace(s))]
	if !ok {
		return marketdata.Interval{}, fmt.Errorf("invalid --interval %q: expected one of M1, H1, H4, D1, W1", s)
	}
	return iv, nil
}

// parseDate accepts a bare date (assumed UTC midnight) or a full
// RFC3339 timestamp, matching cmd/trader/data's own parseDate.
func parseDate(s string) (time.Time, error) {
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t.UTC(), nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC(), nil
	}
	return time.Time{}, fmt.Errorf("invalid date %q: expected YYYY-MM-DD or RFC3339", s)
}
