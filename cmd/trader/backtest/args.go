package backtest

import (
	"fmt"
	"time"

	svcmarketdata "github.com/rustyeddy/trader/internal/service/marketdata"
	"github.com/rustyeddy/trader/marketdata"
)

// parseInterval and parseDate read the CLI's interval names and dates
// through the service layer's shared parsers (ADR-069), so every
// transport accepts exactly the same values. Only the interval error is
// reworded, to name this command's --interval flag.
func parseInterval(s string) (marketdata.Interval, error) {
	iv, err := svcmarketdata.ParseInterval(s)
	if err != nil {
		return marketdata.Interval{}, fmt.Errorf("invalid --interval %q: expected one of %s", s, svcmarketdata.IntervalNames)
	}
	return iv, nil
}

func parseDate(s string) (time.Time, error) { return svcmarketdata.ParseDate(s) }
