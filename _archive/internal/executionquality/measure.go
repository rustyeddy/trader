package executionquality

import (
	"fmt"
	"math/big"
	"strings"

	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/internal/accountactivity"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
)

const (
	methodVWAP     = "units-weighted broker trade-effect prices"
	methodFallback = "provider fill-level aggregate fallback (non-authoritative)"
)

// Measure compares one fill with the benchmark the caller chose. A nil
// benchmark is allowed and gives unknown slippage. The result's
// unknowns are not errors: a fill with no usable execution price still
// returns a Result whose slippage is unknown and says why. Errors are for
// input that cannot be measured at all.
func Measure(a accountactivity.Activity, bench *Benchmark, o Options) (Result, error) {
	qty, ok := a.Quantity.Get()
	if a.Kind != accountactivity.KindFill || !ok || qty.IsZero() ||
		(a.Side != order.Buy && a.Side != order.Sell) {
		return Result{}, ErrNotAFill
	}
	if bench != nil {
		if err := bench.Validate(); err != nil {
			return Result{}, err
		}
	}
	if o.Conversion != nil {
		if err := o.Conversion.validate(); err != nil {
			return Result{}, err
		}
	}

	r := Result{
		Source: a.Key(), Instrument: a.Instrument, Side: a.Side, Quantity: qty,
	}
	if bench != nil {
		b := *bench
		r.Benchmark = &b
		if !b.At.IsZero() {
			r.BenchmarkAge, r.AgeKnown = a.OccurredAt.Sub(b.At), true
		}
	}
	exec, why := executionPrice(a, qty)
	if why != "" {
		r.Unresolved = append(r.Unresolved, why)
		return r, nil
	}
	r.ExecutionPrice = exec

	if bench == nil {
		// No benchmark is unknown slippage, never zero.
		r.Unresolved = append(r.Unresolved, "slippage unknown: no benchmark was supplied")
		return r, nil
	}
	b := *bench
	p, _ := r.ExecutionPrice.Get()

	execRaw, err := rawOf(p.String())
	if err != nil {
		return r, err
	}
	benchRaw, err := rawOf(b.Price.String())
	if err != nil {
		return r, err
	}
	slip := new(big.Int).Sub(execRaw, benchRaw)
	if a.Side == order.Buy {
		slip.Neg(slip)
	}
	rate, err := num.ParseRate(formatRaw(slip))
	if err != nil {
		return r, fmt.Errorf("executionquality: slippage: %w", err)
	}
	if r.Slippage, err = accountactivity.Derived(rate, slippageMethod(b.Kind)); err != nil {
		return r, err
	}

	r.costInto(a, slip, qty, o)
	return r, nil
}

func slippageMethod(k BenchmarkKind) string {
	return "side-signed (execution - benchmark), benchmark " + string(k)
}

// costInto fills SlippageCost and Conversion, or records why it cannot.
func (r *Result) costInto(a accountactivity.Activity, slip *big.Int, qty num.Quantity, o Options) {
	if !o.AccountCurrency.IsValid() {
		r.Unresolved = append(r.Unresolved, "slippage cost unknown: the account currency is not known")
		return
	}
	quote, ok := quoteCurrency(a.Instrument)
	if !ok {
		r.Unresolved = append(r.Unresolved, "slippage cost unknown: the instrument's quote currency is not known")
		return
	}
	qtyRaw, err := rawOf(qty.String())
	if err != nil {
		r.Unresolved = append(r.Unresolved, "slippage cost unknown: "+err.Error())
		return
	}

	// slippage x units, exact, in the quote currency; a conversion multiplies
	// in its rate before the single half-even rounding to 8 decimals.
	amount := new(big.Int).Mul(slip, qtyRaw)
	scale := big.NewInt(100_000_000)
	method := "slippage x units in the quote currency"
	switch {
	case quote.Equal(o.AccountCurrency):
	case o.Conversion != nil && o.Conversion.From.Equal(quote) && o.Conversion.To.Equal(o.AccountCurrency):
		rateRaw, err := rawOf(o.Conversion.Rate.String())
		if err != nil {
			r.Unresolved = append(r.Unresolved, "slippage cost unknown: "+err.Error())
			return
		}
		amount.Mul(amount, rateRaw)
		scale.Mul(scale, big.NewInt(100_000_000))
		c := *o.Conversion
		r.Conversion = &c
		method = "slippage x units x explicit conversion (" + c.Source + ")"
	case o.Conversion != nil:
		r.Unresolved = append(r.Unresolved, fmt.Sprintf(
			"slippage cost unknown: the conversion runs %s to %s, not %s to %s",
			o.Conversion.From, o.Conversion.To, quote, o.AccountCurrency))
		return
	default:
		r.Unresolved = append(r.Unresolved, fmt.Sprintf(
			"slippage cost unknown: no conversion from %s to %s was supplied", quote, o.AccountCurrency))
		return
	}

	money, err := num.ParseMoney(formatRaw(roundHalfEven(amount, scale)), o.AccountCurrency)
	if err != nil {
		r.Unresolved = append(r.Unresolved, "slippage cost unknown: "+err.Error())
		return
	}
	if r.SlippageCost, err = accountactivity.Derived(money, method); err != nil {
		r.Unresolved = append(r.Unresolved, "slippage cost unknown: "+err.Error())
	}
}

// executionPrice is the price the fill executed at and how it was obtained.
// The second result is a reason when there is none.
func executionPrice(a accountactivity.Activity, qty num.Quantity) (accountactivity.Value[num.Price], string) {
	if p, ok := effectVWAP(a, qty); ok {
		if v, err := accountactivity.Derived(p, methodVWAP); err == nil {
			return v, ""
		}
	}
	if p, ok := a.Price.Get(); ok && !p.IsZero() {
		if v, err := accountactivity.Derived(p, methodFallback); err == nil {
			return v, ""
		}
	}
	return accountactivity.Value[num.Price]{}, "execution price unknown: no complete per-trade prices and no fill-level price"
}

// effectVWAP is sum(price x units) / sum(units) over the fill's trade
// effects, with exact integers and one half-even rounding. It exists only
// when every effect has units and a price and the units add up to the fill:
// a partial average is not presented as the fill's price.
func effectVWAP(a accountactivity.Activity, qty num.Quantity) (num.Price, bool) {
	if len(a.TradeEffects) == 0 {
		return num.Price{}, false
	}
	var weighted, units big.Int
	for _, e := range a.TradeEffects {
		u, uok := e.Units.Get()
		p, pok := e.Price.Get()
		if !uok || !pok || u.IsZero() || p.IsZero() {
			return num.Price{}, false
		}
		ur, err := rawOf(u.String())
		if err != nil {
			return num.Price{}, false
		}
		pr, err := rawOf(p.String())
		if err != nil {
			return num.Price{}, false
		}
		units.Add(&units, ur)
		weighted.Add(&weighted, new(big.Int).Mul(pr, ur))
	}
	want, err := rawOf(qty.String())
	if err != nil || units.Cmp(want) != 0 {
		return num.Price{}, false
	}
	p, err := num.ParsePrice(formatRaw(roundHalfEven(&weighted, &units)))
	if err != nil {
		return num.Price{}, false
	}
	return p, true
}

// BenchmarkFromFullVWAP builds the BrokerFullVWAP benchmark from the fill's
// own reported fullVWAP. It is the broker's counterfactual without
// guaranteed-price execution, not Trader's decision price.
func BenchmarkFromFullVWAP(a accountactivity.Activity) (Benchmark, error) {
	p, ok := a.FullVWAP.Get()
	if !ok {
		return Benchmark{}, fmt.Errorf("%w: the fill reports no fullVWAP", ErrInvalidBenchmark)
	}
	b := Benchmark{Kind: BrokerFullVWAP, Price: p, At: a.OccurredAt, Source: a.Provider + " fullVWAP"}
	return b, b.Validate()
}

// BenchmarkFromQuoteTouch builds the BrokerQuote benchmark from the touch of
// the fill's own execution quote: the best ask for a buy, the best bid for a
// sell. The deeper ladder is left alone; a fill larger than the touch is
// exactly what the benchmark then shows.
func BenchmarkFromQuoteTouch(a accountactivity.Activity) (Benchmark, error) {
	q, ok := a.ExecutionQuote.Get()
	if !ok {
		return Benchmark{}, fmt.Errorf("%w: the fill reports no execution quote", ErrInvalidBenchmark)
	}
	var side []accountactivity.PriceLevel
	switch a.Side {
	case order.Buy:
		side = q.Asks
	case order.Sell:
		side = q.Bids
	default:
		return Benchmark{}, ErrNotAFill
	}
	if len(side) == 0 {
		return Benchmark{}, fmt.Errorf("%w: the execution quote has no price on the side this fill traded", ErrInvalidBenchmark)
	}
	b := Benchmark{Kind: BrokerQuote, Price: side[0].Price, At: q.At, Source: a.Provider + " execution quote touch"}
	return b, b.Validate()
}

// quoteCurrency returns the currency prices on a currency pair such as
// fx:EUR/USD are denominated in.
func quoteCurrency(id instrument.ID) (num.Currency, bool) {
	rest, ok := strings.CutPrefix(id.String(), "fx:")
	if !ok {
		return num.Currency{}, false
	}
	_, quote, ok := strings.Cut(rest, "/")
	if !ok {
		return num.Currency{}, false
	}
	c, err := num.ParseCurrency(quote)
	if err != nil {
		return num.Currency{}, false
	}
	return c, true
}

// rawOf reads a decimal such as "-1.1005" as an integer count of 1e-8 units.
func rawOf(s string) (*big.Int, error) {
	s, _, _ = strings.Cut(s, " ")
	r, ok := new(big.Rat).SetString(s)
	if !ok {
		return nil, fmt.Errorf("%q is not a decimal", s)
	}
	r.Mul(r, big.NewRat(100_000_000, 1))
	if !r.IsInt() {
		return nil, fmt.Errorf("%q has more than 8 decimals", s)
	}
	return new(big.Int).Set(r.Num()), nil
}

// formatRaw renders an integer count of 1e-8 units as a decimal string.
func formatRaw(v *big.Int) string {
	neg := v.Sign() < 0
	abs := new(big.Int).Abs(v).String()
	for len(abs) <= 8 {
		abs = "0" + abs
	}
	out := abs[:len(abs)-8] + "." + abs[len(abs)-8:]
	if neg {
		out = "-" + out
	}
	return out
}

// roundHalfEven is n / d rounded to the nearest integer, ties to even, for
// positive d.
func roundHalfEven(n, d *big.Int) *big.Int {
	neg := n.Sign() < 0
	q, r := new(big.Int).QuoRem(new(big.Int).Abs(n), d, new(big.Int))
	switch c := new(big.Int).Lsh(r, 1).Cmp(d); {
	case c > 0, c == 0 && q.Bit(0) == 1:
		q.Add(q, big.NewInt(1))
	}
	if neg {
		q.Neg(q)
	}
	return q
}
