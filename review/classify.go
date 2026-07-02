package review

// Classify assigns a triage bucket ("tradeable", "hot", or "watch") to a
// pair from its multi-timeframe snapshots, per the ordered gates in
// docs/Review.org. The first matching bucket wins. Also returns demotion
// notes surfaced for the tooltip (v1: informational only, not hard gates).
func Classify(d1 D1Snapshot, h4 H4Snapshot, w1 W1Snapshot, setup SetupSnapshot, d1Bias, w1Bias string) (string, []string) {
	hot := d1.ADX >= 25 && d1.CI < 55 && weeklyAgrees(w1Bias, d1Bias)

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

// weeklyAgrees reports whether the weekly bias matches (or is neutral
// relative to) the D1 bias — "weekly not fighting D1" per the doc.
func weeklyAgrees(w1Bias, d1Bias string) bool {
	if d1Bias == "neutral" || w1Bias == "neutral" {
		return true
	}
	return w1Bias == d1Bias
}

func demotionNotes(d1 D1Snapshot, w1Bias, d1Bias string) []string {
	var notes []string
	if d1.ADX < 20 {
		notes = append(notes, "ADX dropped below 20")
	}
	if d1.CI > 65 {
		notes = append(notes, "CI crossed above 65")
	}
	if d1Bias != "neutral" && w1Bias != "neutral" && w1Bias != d1Bias {
		notes = append(notes, "W1 EMA flipped against D1 bias")
	}
	return notes
}
