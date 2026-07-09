package review

// h4ADXFloor and h4MinEMASep are the Tradeable gate's consolidation guard.
// Added after repeated chart-review false positives (NZDUSD, AUDUSD,
// USDCAD) where the scanner called Tradeable on price sitting near a
// flattened, merged H4 EMA20/50 during a multi-week range — not a pullback
// within a live trend. H4 CI < 60 alone doesn't reliably catch this: a
// mature consolidation can score under 60 despite being a range. These two
// checks were the consistent signature across every observed false
// positive; both thresholds are first-pass estimates, not yet calibrated
// against a larger sample.
const (
	h4ADXFloor  = 20.0 // below this, H4 momentum has faded; entry timeframe isn't trending
	h4MinEMASep = 0.3  // below this (ATR multiples), H4 EMA20/50 are effectively merged
)

// weekUsedCaution is the demotion-note threshold for weekly ATR
// consumption. Kept as a note, not a hard gate: a pair at 90%+ week-used
// can still be a structurally valid setup, just with a degraded
// reward:risk profile for the remainder of the week.
const weekUsedCaution = 0.90

// Classify assigns a triage bucket ("tradeable", "hot", or "watch") to a
// pair from its multi-timeframe snapshots, per the ordered gates in
// docs/Review.org. The first matching bucket wins. Also returns demotion
// notes surfaced for the tooltip (v1: informational only, not hard gates,
// except h4ADXFloor/h4MinEMASep which are enforced directly in the
// Tradeable gate below).
func Classify(d1 D1Snapshot, h4 H4Snapshot, w1 W1Snapshot, setup SetupSnapshot, d1Bias, w1Bias string) (string, []string) {
	hot := d1.ADX >= 25 && d1.CI < 55 && WeeklyAgrees(w1Bias, d1Bias)

	var notes []string
	if hot &&
		setup.InValueZone &&
		setup.H4Aligned &&
		h4.CI < 60 &&
		h4.ADX >= h4ADXFloor &&
		absF(h4.EMASepATR) >= h4MinEMASep {
		notes = append(notes, demotionNotes(d1, h4, w1, w1Bias, d1Bias)...)
		return "tradeable", notes
	}

	if hot {
		notes = append(notes, demotionNotes(d1, h4, w1, w1Bias, d1Bias)...)
		return "hot", notes
	}

	notes = append(notes, demotionNotes(d1, h4, w1, w1Bias, d1Bias)...)
	return "watch", notes
}

// Alignment classifies how a lower-timeframe bias relates to a
// higher-timeframe bias (e.g. D1 vs W1). Three states, not two: a neutral
// higher-timeframe reading is genuinely different information from a
// confirmed same-direction reading, even though neither one blocks
// promotion — collapsing them into a single "aligned" bool erases that
// distinction for anyone reading the CLI output.
type Alignment string

const (
	Aligned  Alignment = "aligned"  // both directional and matching
	Neutral  Alignment = "neutral"  // higher timeframe is flat; not fighting, not confirming
	Conflict Alignment = "conflict" // both directional and opposed
)

// WeeklyAlignment classifies the W1-vs-D1 relationship. This is the single
// source of truth for W1/D1 agreement: the Hot gate in Classify, the
// demotion note, and SetupSnapshot.W1Alignment (pair.go) all derive from
// this, so the CLI's W1 column always reflects the same logic that gated
// the pair's bucket.
func WeeklyAlignment(w1Bias, d1Bias string) Alignment {
	if d1Bias == "neutral" || w1Bias == "neutral" {
		return Neutral
	}
	if w1Bias == d1Bias {
		return Aligned
	}
	return Conflict
}

// WeeklyAgrees reports whether the weekly bias is not fighting the D1
// bias — "weekly not fighting D1" per the doc. True for Aligned and
// Neutral; false only for Conflict.
func WeeklyAgrees(w1Bias, d1Bias string) bool {
	return WeeklyAlignment(w1Bias, d1Bias) != Conflict
}

func demotionNotes(d1 D1Snapshot, h4 H4Snapshot, w1 W1Snapshot, w1Bias, d1Bias string) []string {
	var notes []string
	if d1.ADX < 20 {
		notes = append(notes, "ADX dropped below 20")
	}
	if d1.CI > 65 {
		notes = append(notes, "CI crossed above 65")
	}
	if WeeklyAlignment(w1Bias, d1Bias) == Conflict {
		notes = append(notes, "W1 EMA flipped against D1 bias")
	}
	if h4.ADX < h4ADXFloor {
		notes = append(notes, "H4 ADX below 20 — momentum fading at entry timeframe")
	}
	if absF(h4.EMASepATR) < h4MinEMASep {
		notes = append(notes, "H4 EMA20/50 compressed — likely consolidation, not a pullback")
	}
	if w1.WeekUsedPct >= weekUsedCaution {
		notes = append(notes, "Weekly ATR budget ≥90% consumed — poor reward:risk this week")
	}
	return notes
}
