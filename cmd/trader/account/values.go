package account

import (
	"strings"
	"time"

	"github.com/rustyeddy/trader/internal/accountactivity"
	"github.com/rustyeddy/trader/num"
)

const unknownText = "unknown"

// decimal renders a canonical decimal with at least two fraction digits, so a
// known zero reads 0.00 and can never be mistaken for unknown.
func decimal(s string) string {
	s, _, _ = strings.Cut(s, " ")
	whole, frac, _ := strings.Cut(s, ".")
	for len(frac) < 2 {
		frac += "0"
	}
	return whole + "." + frac
}

func moneyText(v accountactivity.Value[num.Money]) string {
	m, ok := v.Get()
	if !ok {
		return unknownText
	}
	return decimal(m.String()) + " " + m.Currency().String()
}

func priceText(v accountactivity.Value[num.Price]) string {
	p, ok := v.Get()
	if !ok {
		return unknownText
	}
	return decimal(p.String())
}

func rateText(v accountactivity.Value[num.Rate]) string {
	r, ok := v.Get()
	if !ok {
		return unknownText
	}
	return decimal(r.String())
}

func qtyValueText(v accountactivity.Value[num.Quantity]) string {
	q, ok := v.Get()
	if !ok {
		return unknownText
	}
	return q.String()
}

// timeText renders the stored instant in UTC at full precision. RFC3339Nano
// drops only trailing zeros, so a whole second still reads 2024-03-01T10:00:00Z.
func timeText(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}
