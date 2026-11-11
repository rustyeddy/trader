package numericstudy

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Storage range is not the binding constraint on scale choice — intermediate
// arithmetic is.  These tests measure where products overflow int64 before the
// descaling divide, which is the evidence #38 needs for its overflow policy.

// TestPriceTimesQuantity multiplies a scaled price by a whole quantity.  Only
// one operand is scaled, so the product carries the scale once and no descale
// is needed; the constraint is purely magnitude.
func TestPriceTimesQuantity(t *testing.T) {
	for _, sc := range Candidates {
		for _, n := range Notionals {
			t.Run(sc.Name+"/"+n.Name, func(t *testing.T) {
				p, err := ParseDecimal(n.Price, sc)
				if err != nil {
					t.Skipf("price not representable at %s: %v", sc.Name, err)
				}

				overflows := MulOverflows(p, n.Quantity)
				switch sc.Name {
				case "1e8":
					assert.False(t, overflows,
						"1e8 must hold every realistic notional")
				case "1e9":
					// Recorded, not required: 1e9 is expected to survive these
					// cases but with far less margin.  See TestScaleMargins.
					t.Logf("1e9 %s overflows=%v", n.Name, overflows)
				}
			})
		}
	}
}

// TestScaleMargins is the finding that separates 1e8 from 1e9: both represent
// every asset in the matrix, but 1e9 leaves the largest realistic notional
// close to the int64 ceiling while 1e8 leaves two orders of magnitude.
func TestScaleMargins(t *testing.T) {
	worst := Notionals[0] // BRK.A block: highest price x institutional size
	require.Equal(t, "BRK.A block", worst.Name)

	// Computed in integer arithmetic throughout: a study arguing against
	// binary floating point should not lean on it to reach its conclusion.
	product := func(name string) int64 {
		sc := scaleByName(t, name)
		p, err := ParseDecimal(worst.Price, sc)
		require.NoError(t, err)
		require.False(t, MulOverflows(p, worst.Quantity))
		return p * worst.Quantity
	}

	p8, p9 := product("1e8"), product("1e9")
	m8, m9 := MaxPriceCeil(p8), MaxPriceCeil(p9)
	t.Logf("%s margin to int64 ceiling: 1e8=%dx 1e9=%dx", worst.Name, m8, m9)

	assert.Greater(t, m8, int64(10), "1e8 must leave an order of magnitude spare")
	assert.Less(t, m9, int64(10), "1e9 is expected to be the tight option")

	// One decimal place apart, so the intermediates differ by exactly 10x.
	// Asserting on the products rather than the margins keeps this exact:
	// the margins are truncating quotients and would only agree approximately.
	assert.Equal(t, p8*10, p9, "the two scales differ by exactly 10x")
}

// TestPriceTimesRate is the decisive intermediate case: both operands are
// scaled, so the product carries the scale twice and must be descaled once.
// The double-scaled intermediate overflows int64 at EVERY candidate scale for
// realistic inputs, so a widened intermediate is mandatory regardless of which
// scale is chosen.  This is a policy finding for #38, not a scale-tuning knob.
func TestPriceTimesRate(t *testing.T) {
	anyOverflow := map[string]bool{}

	for _, sc := range Candidates {
		for _, a := range Assets {
			for _, r := range Rates {
				p, err := ParseDecimal(a.Price, sc)
				if err != nil {
					continue
				}
				rate, err := ParseDecimal(r.Rate, sc)
				if err != nil {
					continue
				}
				if MulOverflows(p, rate) {
					anyOverflow[sc.Name] = true
					t.Logf("%s: %s x %s overflows the double-scaled intermediate",
						sc.Name, a.Symbol, r.Name)
				}
			}
		}
	}

	// 1e8 and 1e9 are the only candidates that can represent the whole asset
	// matrix, and both overflow on realistic Price x Rate pairs.
	for _, name := range []string{"1e8", "1e9"} {
		assert.True(t, anyOverflow[name],
			"scale %s: expected at least one realistic Price x Rate to overflow "+
				"int64 without a widened intermediate", name)
	}

	// 1e5 escapes only because it cannot represent crypto or ZN at all, so its
	// operands never get large enough — that is a symptom of being too narrow,
	// not evidence that it is safe.
	assert.False(t, anyOverflow["1e5"],
		"1e5 avoids overflow only by failing to represent the wider assets")
}

// TestPriceTimesRateWidened shows the same products are safe once the
// intermediate is widened, confirming the fix belongs in the arithmetic
// policy rather than in the choice of scale.
func TestPriceTimesRateWidened(t *testing.T) {
	sc := scaleByName(t, "1e8")
	for _, a := range Assets {
		for _, r := range Rates {
			p, err := ParseDecimal(a.Price, sc)
			require.NoError(t, err)
			rate, err := ParseDecimal(r.Rate, sc)
			require.NoError(t, err)

			// Widen to 128 bits, descale once, and require the result back in
			// int64 range.
			hi, lo := mul128(p, rate)
			got, ok := div128(hi, lo, sc.Factor)
			assert.True(t, ok,
				"%s x %s must fit int64 after descaling", a.Symbol, r.Name)

			if ok && !MulOverflows(p, rate) {
				assert.Equal(t, p*rate/sc.Factor, got,
					"widened path must agree with the narrow path when both work")
			}
		}
	}
}

// TestRollingSum covers accumulation, and is the second policy finding: a
// naive int64 price accumulator overflows far sooner than the storage range
// suggests.  At the scales that can represent our whole asset matrix, a sum of
// high-priced bars exhausts int64 well inside a single backtest, so rolling
// sums and averages need a widened accumulator or a running mean.
func TestRollingSum(t *testing.T) {
	const bars = 1_000_000 // ~10 years of M1 bars

	capacity := map[string]int64{}

	for _, sc := range Candidates {
		t.Run(sc.Name, func(t *testing.T) {
			p, err := ParseDecimal("750000.00", sc)
			require.NoError(t, err)

			var acc int64
			var n int64
			for range bars {
				if AddOverflows(acc, p) {
					break
				}
				acc += p
				n++
			}
			capacity[sc.Name] = n

			// The loop stops at either overflow or the requested bar count.
			assert.Equal(t, min(math.MaxInt64/p, int64(bars)), n,
				"the accumulator must hold exactly MaxInt64/price bars")
			t.Logf("%s: holds %d bars of 750000.00 (%d requested, ceiling %d)",
				sc.Name, n, bars, math.MaxInt64/p)
		})
	}

	assert.Less(t, capacity["1e8"], int64(bars),
		"1e8 must be shown to overflow inside a 1M-bar backtest, so #38 cannot "+
			"treat plain int64 accumulation as safe")
	assert.Equal(t, int64(bars), capacity["1e5"],
		"the narrow scales survive the full run only because they carry less "+
			"precision")
}

// mul128 returns the full 128-bit product of two int64 values as a
// (high, low) pair, sign-extended.  Study helper only: the real
// implementation belongs with the arithmetic policy in #38.
func mul128(a, b int64) (hi int64, lo uint64) {
	neg := (a < 0) != (b < 0)
	ua, ub := abs64(a), abs64(b)

	h, l := umul128(ua, ub)
	if !neg {
		return int64(h), l
	}
	// Two's-complement negate the 128-bit magnitude.
	l = ^l + 1
	h = ^h
	if l == 0 {
		h++
	}
	return int64(h), l
}

func umul128(a, b uint64) (hi, lo uint64) {
	const mask = 0xFFFFFFFF
	a0, a1 := a&mask, a>>32
	b0, b1 := b&mask, b>>32

	w0 := a0 * b0
	t := a1*b0 + w0>>32
	w1, w2 := t&mask, t>>32
	w1 += a0 * b1

	hi = a1*b1 + w2 + w1>>32
	lo = a * b
	return hi, lo
}

// div128 divides a 128-bit signed value by a positive int64 divisor,
// truncating toward zero, and reports whether the quotient fits in int64.
func div128(hi int64, lo uint64, d int64) (int64, bool) {
	neg := hi < 0
	uhi, ulo := uint64(hi), lo
	if neg {
		ulo = ^ulo + 1
		uhi = ^uhi
		if ulo == 0 {
			uhi++
		}
	}

	ud := uint64(d)
	if uhi >= ud {
		return 0, false // quotient needs more than 64 bits
	}

	// Long division of the 128-bit magnitude, bit by bit.
	var q, rem uint64
	for i := 127; i >= 0; i-- {
		var bit uint64
		if i >= 64 {
			bit = (uhi >> (i - 64)) & 1
		} else {
			bit = (ulo >> i) & 1
		}
		// rem cannot reach the top bit here because uhi < ud.
		rem = rem<<1 | bit
		if rem >= ud {
			rem -= ud
			if i < 64 {
				q |= 1 << i
			} else {
				return 0, false
			}
		}
	}

	if neg {
		if q > 1<<63 {
			return 0, false
		}
		return -int64(q), true
	}
	if q > math.MaxInt64 {
		return 0, false
	}
	return int64(q), true
}

func abs64(v int64) uint64 {
	if v < 0 {
		return uint64(-(v + 1)) + 1
	}
	return uint64(v)
}
