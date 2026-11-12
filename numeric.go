package trader

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/rustyeddy/trader/num"
)

// The root expresses prices, rates, money and quantities through the exact
// types in num (ADR-004, ADR-086, ADR-087) and never holds a raw scaled
// integer. This file holds the few root-private helpers that bridge the gaps
// num leaves on purpose: ordering and signed differences of the non-negative
// Price, and presentation (cents) of Money.

// usdCurrency is the account and quote currency of every instrument for now.
// Converting other quote currencies is out of scope (ADR-087).
var usdCurrency = num.MustParseCurrency("USD")

var (
	// priceTick is the resolution of prices in the bars files: five decimals.
	// It is a data and instrument rule, not a num rule.
	priceTick = num.MustParsePrice("0.00001")

	// maxPrice is the largest price a bar or order may carry: a data-sanity
	// limit, not chosen to make one multiplication fit (ADR-087).
	maxPrice = num.MustParsePrice("10000")

	oneUnit = num.MustParseQuantity("1")
)

var (
	rateOnePercent  = num.MustParseRate("0.01")
	rateTwoPercent  = num.MustParseRate("0.02")
	rateFivePercent = num.MustParseRate("0.05")
	rateOne         = num.MustParseRate("1")
	rateHalf        = num.MustParseRate("0.5")
)

// defaultMaxOrderNotional is the largest notional one order may have, in USD:
// an account trading policy, separate from any provider constraint (ADR-087).
const defaultMaxOrderNotional = "50000000"

// zeroMoney is a zero amount in currency c, which must be valid: for constants
// and fixtures. Engine code uses zeroMoneyOf.
func zeroMoney(c num.Currency) num.Money { return num.MustParseMoney("0", c) }

// zeroMoneyOf is a zero amount in currency c, or num.ErrMissingCurrency if c is
// not valid, for currencies that come from a caller.
func zeroMoneyOf(c num.Currency) (num.Money, error) { return num.ParseMoney("0", c) }

// moneySign is -1, 0 or +1 for m. Money with no currency has no sign: that is
// an error, not a zero.
func moneySign(m num.Money) (int, error) {
	if !m.IsValid() {
		return 0, num.ErrMissingCurrency
	}
	return m.Cmp(zeroMoney(m.Currency()))
}

// priceMax and priceMin order Prices, which are not comparable with < or the
// builtin min and max.
func priceMax(first num.Price, rest ...num.Price) num.Price {
	for _, p := range rest {
		if p.Cmp(first) > 0 {
			first = p
		}
	}
	return first
}

func priceMin(first num.Price, rest ...num.Price) num.Price {
	for _, p := range rest {
		if p.Cmp(first) < 0 {
			first = p
		}
	}
	return first
}

// priceDiff is the distance between a and b, which is never negative, and
// whether a is at or above b.
func priceDiff(a, b num.Price) (dist num.Price, aboveOrEqual bool, err error) {
	if a.Cmp(b) >= 0 {
		dist, err = a.Sub(b)
		return dist, true, err
	}
	dist, err = b.Sub(a)
	return dist, false, err
}

func quantityMin(first num.Quantity, rest ...num.Quantity) num.Quantity {
	for _, q := range rest {
		if q.Cmp(first) < 0 {
			first = q
		}
	}
	return first
}

// floorCents is m in whole cents, rounded toward negative infinity, so a loss
// is never understated: -102.08316 is -10209. It reads m's canonical decimal
// text and never goes through a float. This is the presentation step of
// ADR-086; the engine itself never rounds money.
func floorCents(m num.Money) (int64, error) {
	text, _, _ := strings.Cut(m.String(), " ")
	negative := strings.HasPrefix(text, "-")
	whole, frac, _ := strings.Cut(strings.TrimPrefix(text, "-"), ".")
	frac += "00"
	cents, err := strconv.ParseInt(whole+frac[:2], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("money %q: %w", m, err)
	}
	if negative {
		cents = -cents
		if strings.Trim(frac[2:], "0") != "" {
			cents--
		}
	}
	return cents, nil
}

// formatCents prints whole cents as dollars: $33.00, -$102.09.
func formatCents(cents int64) string {
	sign, abs := "", uint64(cents)
	if cents < 0 {
		sign, abs = "-", -abs
	}
	return fmt.Sprintf("%s$%d.%02d", sign, abs/100, abs%100)
}

// formatPrice prints p with the five decimals of priceTick, truncating any
// further digits (a price is always at that resolution).
func formatPrice(p num.Price) string {
	whole, frac, _ := strings.Cut(p.String(), ".")
	return whole + "." + (frac + "00000")[:5]
}
