package review

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestClassify(t *testing.T) {
	hotD1 := D1Snapshot{ADX: 30, CI: 40}
	coldD1 := D1Snapshot{ADX: 15, CI: 70}
	// h4Tradeable satisfies the consolidation guard added in classify.go
	// (h4ADXFloor=20, h4MinEMASep=0.3) so cases meant to reach "tradeable"
	// don't fall through on those gates.
	h4Tradeable := H4Snapshot{CI: 40, ADX: 25, EMASepATR: 0.5}

	tests := []struct {
		name       string
		d1         D1Snapshot
		h4         H4Snapshot
		w1Bias     string
		d1Bias     string
		setup      SetupSnapshot
		wantBucket string
	}{
		{
			name:       "hot gate fails on low ADX falls to watch",
			d1:         coldD1,
			h4:         H4Snapshot{CI: 40},
			w1Bias:     "long",
			d1Bias:     "long",
			setup:      SetupSnapshot{InValueZone: true, H4Aligned: true},
			wantBucket: "watch",
		},
		{
			name:       "hot gate passes but not in value zone stays hot",
			d1:         hotD1,
			h4:         H4Snapshot{CI: 40},
			w1Bias:     "long",
			d1Bias:     "long",
			setup:      SetupSnapshot{InValueZone: false, H4Aligned: true},
			wantBucket: "hot",
		},
		{
			name:       "hot gate passes but H4 not aligned stays hot",
			d1:         hotD1,
			h4:         H4Snapshot{CI: 40},
			w1Bias:     "long",
			d1Bias:     "long",
			setup:      SetupSnapshot{InValueZone: true, H4Aligned: false},
			wantBucket: "hot",
		},
		{
			name:       "hot gate passes but H4 CI too high stays hot",
			d1:         hotD1,
			h4:         H4Snapshot{CI: 65},
			w1Bias:     "long",
			d1Bias:     "long",
			setup:      SetupSnapshot{InValueZone: true, H4Aligned: true},
			wantBucket: "hot",
		},
		{
			name:       "all gates pass -> tradeable",
			d1:         hotD1,
			h4:         h4Tradeable,
			w1Bias:     "long",
			d1Bias:     "long",
			setup:      SetupSnapshot{InValueZone: true, H4Aligned: true},
			wantBucket: "tradeable",
		},
		{
			name:       "H4 ADX below floor keeps tradeable-eligible pair at hot",
			d1:         hotD1,
			h4:         H4Snapshot{CI: 40, ADX: 15, EMASepATR: 0.5},
			w1Bias:     "long",
			d1Bias:     "long",
			setup:      SetupSnapshot{InValueZone: true, H4Aligned: true},
			wantBucket: "hot",
		},
		{
			name:       "H4 EMA separation below floor (merged EMAs) keeps tradeable-eligible pair at hot",
			d1:         hotD1,
			h4:         H4Snapshot{CI: 40, ADX: 25, EMASepATR: 0.1},
			w1Bias:     "long",
			d1Bias:     "long",
			setup:      SetupSnapshot{InValueZone: true, H4Aligned: true},
			wantBucket: "hot",
		},
		{
			name:       "weekly fighting D1 demotes hot pair to watch",
			d1:         hotD1,
			h4:         h4Tradeable,
			w1Bias:     "short",
			d1Bias:     "long",
			setup:      SetupSnapshot{InValueZone: true, H4Aligned: true},
			wantBucket: "watch",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bucket, _ := Classify(tt.d1, tt.h4, W1Snapshot{}, tt.setup, tt.d1Bias, tt.w1Bias, DefaultThresholds())
			assert.Equal(t, tt.wantBucket, bucket)
		})
	}
}

func TestClassify_DemotionNotes(t *testing.T) {
	d1 := D1Snapshot{ADX: 15, CI: 70}
	_, notes := Classify(d1, H4Snapshot{}, W1Snapshot{}, SetupSnapshot{}, "long", "short", DefaultThresholds())

	assert.Contains(t, notes, "ADX dropped below 20")
	assert.Contains(t, notes, "CI crossed above 65")
	assert.Contains(t, notes, "W1 EMA flipped against D1 bias")
}

func TestClassify_NoDemotionNotesWhenHealthy(t *testing.T) {
	d1 := D1Snapshot{ADX: 30, CI: 40}
	h4 := H4Snapshot{CI: 40, ADX: 25, EMASepATR: 0.5}
	_, notes := Classify(d1, h4, W1Snapshot{}, SetupSnapshot{}, "long", "long", DefaultThresholds())
	assert.Empty(t, notes)
}

// TestClassify_CustomThresholds proves the config/CLI-facing knobs actually
// change classification outcomes: a pair that stays "watch" under
// DefaultThresholds (D1 ADX=22 is below the 25.0 default Hot floor) is
// promoted to "hot" once a caller lowers HotD1ADXFloor. See issue #165.
func TestClassify_CustomThresholds(t *testing.T) {
	d1 := D1Snapshot{ADX: 22, CI: 40}
	h4 := H4Snapshot{CI: 40}
	setup := SetupSnapshot{InValueZone: true, H4Aligned: true}

	bucket, _ := Classify(d1, h4, W1Snapshot{}, setup, "long", "long", DefaultThresholds())
	assert.Equal(t, "watch", bucket, "sanity check: default threshold does not classify this pair as hot")

	lenient := DefaultThresholds()
	lenient.HotD1ADXFloor = 20.0
	bucket, _ = Classify(d1, h4, W1Snapshot{}, setup, "long", "long", lenient)
	assert.Equal(t, "hot", bucket, "lowering HotD1ADXFloor should promote the same inputs to hot")
}
