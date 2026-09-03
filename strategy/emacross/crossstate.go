package emacross

// relation is one bar's raw fast-vs-slow EMA comparison. relationTie is
// the zero value, deliberately doubling as "no relation yet" before
// both EMAs are ready — crossState never trusts a bare relation value
// without also consulting its own have flag, so this overlap is safe.
type relation uint8

const (
	relationTie relation = iota
	relationAbove
	relationBelow
)

// String returns relation's canonical, deterministic text — the exact
// vocabulary journal.Signal.Values records it under (issue #253,
// EMA-08), so it must stay stable once used in a real journal.
func (r relation) String() string {
	switch r {
	case relationAbove:
		return "above"
	case relationBelow:
		return "below"
	default:
		return "tie"
	}
}

// classifyRelation returns fast's raw relation to slow.
func classifyRelation(fast, slow float64) relation {
	switch {
	case fast > slow:
		return relationAbove
	case fast < slow:
		return relationBelow
	default:
		return relationTie
	}
}

// crossState implements docs/research/ema-01-experiment-definition.org's
// Decision 1 crossover state machine: it tracks the last non-tie
// relation observed (last), and whether one has ever been observed at
// all (have) — a relationTie input neither triggers a crossover nor
// updates last, so a real reversal immediately following a tie is
// still correctly detected against the pre-tie relation, and a
// tie-then-return-to-the-same-side sequence never appears to cross.
//
// The zero value is ready to use: have starts false, so the very first
// non-tie relation observed can never itself report a crossover (there
// is nothing to compare it against yet).
type crossState struct {
	have bool
	last relation
}

// update advances s with current's raw relation and reports whether a
// bullish (last non-tie relation was Below, current is Above) or
// bearish (last non-tie relation was Above, current is Below)
// crossover just occurred. At most one of bullish/bearish is ever
// true.
func (s *crossState) update(current relation) (bullish, bearish bool) {
	if s.have {
		bullish = current == relationAbove && s.last == relationBelow
		bearish = current == relationBelow && s.last == relationAbove
	}
	if current != relationTie {
		s.last = current
		s.have = true
	}
	return bullish, bearish
}
