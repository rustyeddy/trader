package quantitystudy

import (
	"math"
	"testing"

	ns "github.com/rustyeddy/trader/test/internal/numericstudy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Checks 1 and 2: exact parse and format round trips ---------------------

// TestRoundTrip is the core evidence: every representative quantity, at every
// candidate scale, either round-trips exactly or fails for a documented
// reason.  Parsing goes straight from decimal text to scaled int64 with no
// float64 anywhere; TestNoFloatingPoint enforces that across the package.
func TestRoundTrip(t *testing.T) {
	for _, sc := range Candidates {
		for _, q := range Quantities {
			t.Run(sc.Name+"/"+q.Symbol+"/"+q.Quantity, func(t *testing.T) {
				v, err := ns.ParseDecimal(q.Quantity, sc)

				if q.Decimals > sc.Decimals {
					require.ErrorIs(t, err, ns.ErrTooManyDecimals,
						"%s needs %d decimals; scale %s holds %d",
						q.Quantity, q.Decimals, sc.Name, sc.Decimals)
					return
				}
				if err != nil {
					require.ErrorIs(t, err, ns.ErrOverflow,
						"the only other permitted failure is range")
					return
				}

				assert.Equal(t, ns.Canonical(q.Quantity), ns.FormatDecimal(v, sc),
					"round trip must be exact")
			})
		}
	}
}

// --- Check 3: maximum whole-unit quantity -----------------------------------

func TestMaxWholeUnits(t *testing.T) {
	want := map[string]int64{
		"1e6": 9_223_372_036_854,
		"1e7": 922_337_203_685,
		"1e8": 92_233_720_368,
		"1e9": 9_223_372_036,
	}

	for _, sc := range Candidates {
		t.Run(sc.Name, func(t *testing.T) {
			got := MaxWholeUnits(sc)
			require.Equal(t, want[sc.Name], got)

			// The stated maximum is representable and one more is not.
			_, err := ns.ParseDecimal(ns.MaxPriceText(sc), sc)
			assert.NoError(t, err)

			_, err = ns.ParseDecimal(wholeUnits(got+1), sc)
			assert.ErrorIs(t, err, ns.ErrOverflow)
		})
	}
}

// --- Check 4 and the central finding: the precision/range frontier ----------

// TestFrontier is this study's most consequential result.  #36 asks for one
// fixed Trader-wide Quantity scale that holds both satoshi precision and
// trillion-unit token positions.  No scaled int64 can do both, so the "single
// scale" premise cannot survive the matrix as written.
func TestFrontier(t *testing.T) {
	for _, f := range Frontiers() {
		t.Run(f.Scale.Name, func(t *testing.T) {
			t.Logf("smallest=%s maxWhole=%d finest=%v largest=%v",
				f.SmallestUnit, f.MaxWholeUnits, f.HoldsFinest, f.HoldsLargest)

			assert.False(t, f.HoldsFinest && f.HoldsLargest,
				"no int64 scale should hold both extremes")
		})
	}

	assert.False(t, SatisfiesBoth(),
		"if this ever passes, int64 gained bits and the #36 conclusion changes")

	// Name the two ends explicitly so the frontier is not an accident of which
	// candidates happen to be listed.
	finest := mustScaleHolding(t, FinestQuantity)
	largest := mustScaleHolding(t, LargestQuantity)
	assert.NotEqual(t, finest.Name, largest.Name,
		"the two requirements must be satisfied by different scales")

	// The arithmetic behind the impossibility, stated as a check rather than
	// as prose: satoshi precision needs 8 fraction digits, and a trillion
	// whole units at 8 digits needs more than int64 has.
	const trillion = int64(1_000_000_000_000)
	assert.True(t, ns.MulOverflows(trillion, SelectedScale.Factor),
		"1e12 units at satoshi precision must exceed int64")
}

// TestSelectedScale pins the #36 recommendation and the bound that comes with
// it: 1e8 Trader-wide, chosen on precision coverage plus an explicitly bounded
// supported range rather than on holding every quantity in the pressure matrix.
func TestSelectedScale(t *testing.T) {
	sc := SelectedScale

	require.Equal(t, PriceScale.Factor, sc.Factor,
		"Quantity should share Price's scale unless there is a reason not to")
	assert.Equal(t, int64(92_233_720_368), MaxSupportedWholeUnits)
	assert.Equal(t, "0.00000001", ns.FormatDecimal(1, sc))

	// Every intended asset class is representable at the selected scale.
	for _, q := range Quantities {
		if q.Quantity == LargestQuantity {
			continue // the out-of-domain pressure case, asserted below
		}
		t.Run(q.Symbol+"/"+q.Quantity, func(t *testing.T) {
			v, err := ns.ParseDecimal(q.Quantity, sc)
			require.NoError(t, err, "%s must be representable at the selected scale", q.Quantity)
			assert.Equal(t, ns.Canonical(q.Quantity), ns.FormatDecimal(v, sc))
		})
	}

	// The bound is exact: the maximum holds, one more unit does not.
	_, err := ns.ParseDecimal(wholeUnits(MaxSupportedWholeUnits), sc)
	assert.NoError(t, err)

	_, err = ns.ParseDecimal(wholeUnits(MaxSupportedWholeUnits+1), sc)
	assert.ErrorIs(t, err, ns.ErrOverflow)
}

// TestOutOfDomainQuantityIsRejected covers the retained pressure case.  A
// trillion-unit token position is outside the initial supported domain, and
// the important property is that it is rejected as out of range rather than
// silently truncated or wrapped into a plausible-looking small number.
func TestOutOfDomainQuantityIsRejected(t *testing.T) {
	sc := SelectedScale

	_, err := ns.ParseDecimal(LargestQuantity, sc)
	require.ErrorIs(t, err, ns.ErrOverflow,
		"the failure must be a range error, not a precision error")
	assert.NotErrorIs(t, err, ns.ErrTooManyDecimals)

	// It is a range problem, not a precision problem: the same value is
	// representable at a coarser scale.
	v, err := ns.ParseDecimal(LargestQuantity, scaleByName(t, "1e6"))
	require.NoError(t, err)
	assert.Equal(t, LargestQuantity, ns.FormatDecimal(v, scaleByName(t, "1e6")))

	// And the coarser scale that holds it cannot hold a satoshi, which is why
	// trading range for precision is not a free move.
	assert.False(t, Representable(FinestQuantity, scaleByName(t, "1e6")))
}

// TestSelectedScaleBeatsFinerCandidates records why 1e8 rather than 1e9: both
// cover every intended asset class, but 1e9 spends an unused decimal place on
// a tenfold smaller supported range.
func TestSelectedScaleBeatsFinerCandidates(t *testing.T) {
	fine := scaleByName(t, "1e9")

	assert.True(t, Representable(FinestQuantity, SelectedScale))
	assert.True(t, Representable(FinestQuantity, fine),
		"1e9 also covers satoshi precision")

	// Integer division truncates, so compare in the direction that stays
	// exact: the finer scale's range is the selected scale's, divided by ten.
	assert.Equal(t, MaxWholeUnits(SelectedScale)/10, MaxWholeUnits(fine),
		"1e9 costs a factor of ten in supported range")

	// Nothing in the matrix needs the ninth decimal place.
	for _, q := range Quantities {
		assert.LessOrEqual(t, q.Decimals, SelectedScale.Decimals,
			"%s would justify a finer scale", q.Quantity)
	}
}

// TestRepresentabilityIsMonotonic pins the shape of the trade-off: finer
// scales hold smaller quantities and fewer whole units, strictly.
func TestRepresentabilityIsMonotonic(t *testing.T) {
	frontiers := Frontiers()

	for i := 1; i < len(frontiers); i++ {
		prev, cur := frontiers[i-1], frontiers[i]
		require.Greater(t, cur.Scale.Decimals, prev.Scale.Decimals,
			"candidates must be ordered coarsest first")

		assert.Less(t, cur.MaxWholeUnits, prev.MaxWholeUnits,
			"a finer scale must hold fewer whole units")
	}
}

// --- Check 5: representation scale is independent of instrument rules -------

// TestRulesAreIndependentOfScale shows the same instrument rule set behaves
// identically at every scale that can represent it.  The scale says what can
// be stored; the rules say what may be traded.
func TestRulesAreIndependentOfScale(t *testing.T) {
	for _, inc := range Increments {
		for _, sc := range Candidates {
			rules, ok := scaleRules(inc, sc)
			if !ok {
				continue // rule not representable at this scale
			}

			t.Run(inc.Symbol+"/"+sc.Name, func(t *testing.T) {
				// A quantity of exactly one increment above the minimum is
				// valid at every scale that can express both.
				q := rules.Minimum
				if rules.Increment > 0 && q%rules.Increment != 0 {
					q = ((q / rules.Increment) + 1) * rules.Increment
				}
				assert.NoError(t, rules.Validate(q, sc))

				// Half an increment never is.  An integral-only instrument
				// rejects it as non-integral first, which is the same
				// judgement reached by the stricter of two applicable rules.
				if rules.Increment > 1 {
					err := rules.Validate(q+rules.Increment/2, sc)
					if rules.IntegralOnly {
						assert.ErrorIs(t, err, ErrNotIntegral)
					} else {
						assert.ErrorIs(t, err, ErrIncrement)
					}
				}
			})
		}
	}
}

// --- Check 6: exact increment validation by integer modulo ------------------

func TestIncrementValidation(t *testing.T) {
	sc := scaleByName(t, "1e8")

	rules := QuantityRules{
		Increment: mustParse(t, "0.00000001", sc),
		Minimum:   mustParse(t, "0.0001", sc),
	}

	assert.NoError(t, rules.Validate(mustParse(t, "0.0001", sc), sc))
	assert.NoError(t, rules.Validate(mustParse(t, "1.23456789", sc), sc))
	assert.ErrorIs(t, rules.Validate(mustParse(t, "0.00001", sc), sc), ErrBelowMinimum)

	// A coarser increment rejects anything off the step, exactly.
	coarse := QuantityRules{Increment: mustParse(t, "0.25", sc)}
	assert.NoError(t, coarse.Validate(mustParse(t, "0.75", sc), sc))
	assert.ErrorIs(t, coarse.Validate(mustParse(t, "0.8", sc), sc), ErrIncrement)
	assert.ErrorIs(t, coarse.Validate(mustParse(t, "0.7499999", sc), sc), ErrIncrement)
}

// --- Check 7: integral-only instruments -------------------------------------

func TestIntegralOnly(t *testing.T) {
	for _, sc := range Candidates {
		t.Run(sc.Name, func(t *testing.T) {
			rules := QuantityRules{IntegralOnly: true, Increment: sc.Factor}

			for _, whole := range []string{"1", "3", "10000"} {
				assert.NoError(t, rules.Validate(mustParse(t, whole, sc), sc),
					"%s contracts must validate", whole)
			}

			for _, frac := range []string{"0.5", "1.5", "2.000001"} {
				v, err := ns.ParseDecimal(frac, sc)
				if err != nil {
					continue
				}
				assert.ErrorIs(t, rules.Validate(v, sc), ErrNotIntegral,
					"%s must be rejected as non-integral", frac)
			}
		})
	}
}

// --- Check 8: zero is representable but not orderable -----------------------

// TestZeroPolicy separates two questions that are easy to conflate.  Zero is a
// perfectly good Quantity — a flat position, a fully closed lot — so the
// representation must hold it.  Only order construction rejects it.
func TestZeroPolicy(t *testing.T) {
	for _, sc := range Candidates {
		t.Run(sc.Name, func(t *testing.T) {
			v, err := ns.ParseDecimal("0", sc)
			require.NoError(t, err, "zero must be representable")
			assert.Zero(t, v)
			assert.Equal(t, "0", ns.FormatDecimal(v, sc))

			rules := QuantityRules{Increment: sc.Factor, Minimum: sc.Factor}

			// Instrument conformance does not reject zero...
			assert.NotErrorIs(t, rules.Validate(0, sc), ErrZero)

			// ...but constructing an order from it does.
			assert.ErrorIs(t, rules.ValidateOrder(0, sc), ErrZero)
		})
	}
}

// --- Check 9: public quantities are non-negative ----------------------------

// TestNonNegative pins the rule that direction is never encoded in the
// quantity sign.  A negative quantity is a programming error, not a sell: side
// belongs to the order, and conflating them makes every downstream sum
// silently wrong.
func TestNonNegative(t *testing.T) {
	for _, sc := range Candidates {
		t.Run(sc.Name, func(t *testing.T) {
			rules := QuantityRules{Increment: sc.Factor}

			for _, neg := range []string{"-1", "-0.5", "-10000"} {
				v, err := ns.ParseDecimal(neg, sc)
				if err != nil {
					continue
				}
				assert.ErrorIs(t, rules.Validate(v, sc), ErrNegative)
				assert.ErrorIs(t, rules.ValidateOrder(v, sc), ErrNegative)
			}

			// The negative bound is rejected as a quantity, not mishandled.
			assert.ErrorIs(t, rules.Validate(math.MinInt64, sc), ErrNegative)
		})
	}
}

// --- Check 10: Price x Quantity intermediates -------------------------------

// TestNotionalNeedsWidening is the arithmetic consequence #36 owes #38.  With
// Price fixed at 1e8 by #33 and Quantity scaled too, the product is
// double-scaled — the same shape as Price x Rate — so realistic notionals
// overflow int64 before the descaling divide.
func TestNotionalNeedsWidening(t *testing.T) {
	overflowed := map[string]bool{}

	for _, sc := range Candidates {
		for _, n := range NotionalCases {
			p, perr := ns.ParseDecimal(n.Price, PriceScale)
			q, qerr := ns.ParseDecimal(n.Quantity, sc)
			if perr != nil || qerr != nil {
				continue
			}
			if NotionalOverflows(p, q) {
				overflowed[sc.Name] = true
				t.Logf("%s: %s overflows the double-scaled intermediate", sc.Name, n.Name)
			}
		}
	}

	for _, sc := range Candidates {
		assert.True(t, overflowed[sc.Name],
			"scale %s: scaled Quantity must be shown to require widened "+
				"arithmetic for notional, per #38", sc.Name)
	}
}

// TestWholeUnitQuantityIsSafer contrasts the scaled case with the unscaled one
// #33 measured, showing precisely what scaling Quantity costs.
func TestWholeUnitQuantityIsSafer(t *testing.T) {
	sc := scaleByName(t, "1e8")

	price := mustParse(t, "750000.00", PriceScale)
	const blockSize = int64(10_000)

	// Whole unscaled quantity: single-scaled, comfortable.
	assert.False(t, ns.MulOverflows(price, blockSize),
		"unscaled quantity keeps the #33 margin")

	// The same position with a scaled quantity: double-scaled, overflows.
	scaled := mustParse(t, "10000", sc)
	assert.True(t, ns.MulOverflows(price, scaled),
		"scaling Quantity is what pushes this product past int64")
}

// --- helpers ----------------------------------------------------------------

func scaleByName(t *testing.T, name string) ns.Scale {
	t.Helper()
	for _, sc := range Candidates {
		if sc.Name == name {
			return sc
		}
	}
	t.Fatalf("unknown scale %q", name)
	return ns.Scale{}
}

func mustParse(t *testing.T, text string, sc ns.Scale) int64 {
	t.Helper()
	v, err := ns.ParseDecimal(text, sc)
	require.NoError(t, err, "%s at scale %s", text, sc.Name)
	return v
}

// mustScaleHolding returns the first candidate that can represent text.
func mustScaleHolding(t *testing.T, text string) ns.Scale {
	t.Helper()
	for _, sc := range Candidates {
		if Representable(text, sc) {
			return sc
		}
	}
	t.Fatalf("no candidate represents %s", text)
	return ns.Scale{}
}

// scaleRules converts an Increment's decimal text into scaled rules, reporting
// false when the scale cannot represent them.
func scaleRules(inc Increment, sc ns.Scale) (QuantityRules, bool) {
	step, err := ns.ParseDecimal(inc.Increment, sc)
	if err != nil {
		return QuantityRules{}, false
	}

	min, err := ns.ParseDecimal(inc.Minimum, sc)
	if err != nil {
		return QuantityRules{}, false
	}

	var max int64
	if inc.Maximum != "" {
		max, err = ns.ParseDecimal(inc.Maximum, sc)
		if err != nil {
			return QuantityRules{}, false
		}
	}

	return QuantityRules{
		Increment:    step,
		Minimum:      min,
		Maximum:      max,
		IntegralOnly: inc.IntegralOnly,
	}, true
}

// wholeUnits renders an int64 as plain digits for feeding back into the parser.
func wholeUnits(v int64) string {
	return ns.FormatDecimal(v, ns.Scale{Name: "1e0", Decimals: 0, Factor: 1})
}
