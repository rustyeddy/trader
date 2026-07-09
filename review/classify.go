package review

// Classify assigns a triage bucket ("tradeable", "hot", or "watch") to a
// pair from its multi-timeframe snapshots, per the ordered gates in
// docs/Review.org. The first matching bucket wins. Also returns demotion
// notes surfaced for the tooltip (v1: informational only, not hard gates).
func Classify(d1 D1Snapshot, h4 H4Snapshot, w1 W1Snapshot, setup SetupSnapshot, d1Bias, w1Bias string) (string, []string) {
	hot := d1.ADX >= 25 && d1.CI < 55 && WeeklyAgrees(w1Bias, d1Bias)

	var notes []string
	if hot &&
		setup.InValueZone &&
		setup.H4Aligned &&
		h4.CI < 60 {
		notes = append(notes, demotionNotes(d1, w1Bias, d1Bias)...)
		return "tradeable", notes
	}

	if hot {
		notes = append(notes, demotionNotes(d1, w1Bias, d1Bias)...)
		return "hot", notes
	}

	notes = append(notes, demotionNotes(d1, w1Bias, d1Bias)...)
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

func demotionNotes(d1 D1Snapshot, w1Bias, d1Bias string) []string {
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
	return notes
}
