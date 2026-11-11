package account

import (
	"fmt"
	"strings"
	"time"

	"github.com/rustyeddy/trader/internal/accountactivity"
)

// parseTime reads a range bound: RFC 3339, or a date meaning 00:00 UTC. An
// empty string is an unbounded end.
func parseTime(flag, s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC(), nil
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t.UTC(), nil
	}
	return time.Time{}, fmt.Errorf("--%s %q: want RFC 3339 or YYYY-MM-DD (UTC)", flag, s)
}

// parseRange reads --from and --to and checks them as a half-open range.
func parseRange(from, to string) (time.Time, time.Time, error) {
	f, err := parseTime("from", from)
	if err != nil {
		return f, f, err
	}
	t, err := parseTime("to", to)
	if err != nil {
		return f, t, err
	}
	if !f.IsZero() && !t.IsZero() && !t.After(f) {
		return f, t, fmt.Errorf("--to %s must be after --from %s", timeText(t), timeText(f))
	}
	return f, t, nil
}

var kindsByName = map[string]accountactivity.Kind{
	"fill": accountactivity.KindFill, "financing": accountactivity.KindFinancing,
	"fee": accountactivity.KindFee, "deposit": accountactivity.KindDeposit,
	"withdrawal": accountactivity.KindWithdrawal, "dividend": accountactivity.KindDividend,
	"adjustment": accountactivity.KindAdjustment, "other": accountactivity.KindOther,
}

// parseKinds reads a comma-separated --kind list.
func parseKinds(s string) ([]accountactivity.Kind, error) {
	if strings.TrimSpace(s) == "" {
		return nil, nil
	}
	var out []accountactivity.Kind
	for _, name := range strings.Split(s, ",") {
		k, ok := kindsByName[strings.ToLower(strings.TrimSpace(name))]
		if !ok {
			return nil, fmt.Errorf("--kind %q: unknown kind (want fill, financing, fee, deposit, withdrawal, dividend, adjustment or other)", name)
		}
		out = append(out, k)
	}
	return out, nil
}
