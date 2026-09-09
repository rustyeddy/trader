package smatrend

// crossState implements EQS-01's own cross-above rule: a signal fires
// only when the previous completed close was less than or equal to its
// SMA and the current completed close is strictly greater than its
// SMA — a genuine cross, not merely "close is currently above the
// SMA." Continuously remaining above the SMA after the initial cross
// produces no further signal, and this is exactly what makes the "no
// automatic re-entry without a fresh cross" acceptance criterion true
// by construction: update is fed unconditionally every ready bar,
// regardless of the strategy's own current position side, so a stop
// exit while price remains above the SMA leaves crossState already
// "above" — no new bullish signal can fire until price first returns
// to at-or-below the SMA and then crosses back above it.
//
// Unlike strategy/emacross's three-state relation (which distinguishes
// an exact tie to protect a floating-point EMA comparison from
// spurious reversals), an equality between a close and its own SMA
// groups with "at or below" per the issue's own rule text — there is
// no separate tie state to protect here, so a plain two-state boolean
// is sufficient and unambiguous.
//
// The zero value is ready to use: have starts false, so the very first
// bar observed can never itself report a cross (there is nothing to
// compare it against yet).
type crossState struct {
	have     bool
	aboveSMA bool
}

// update advances s with current's raw above/at-or-below relation and
// reports whether a cross-above (previous at-or-below, current above)
// just occurred.
func (s *crossState) update(currentAbove bool) (crossedAbove bool) {
	if s.have {
		crossedAbove = currentAbove && !s.aboveSMA
	}
	s.aboveSMA = currentAbove
	s.have = true
	return crossedAbove
}
