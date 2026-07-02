package review

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestClassify(t *testing.T) {
	hotD1 := D1Snapshot{ADX: 30, CI: 40}
	coldD1 := D1Snapshot{ADX: 15, CI: 70}

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
			h4:         H4Snapshot{CI: 40},
			w1Bias:     "long",
			d1Bias:     "long",
			setup:      SetupSnapshot{InValueZone: true, H4Aligned: true},
			wantBucket: "tradeable",
		},
		{
			name:       "weekly fighting D1 demotes hot pair to watch",
			d1:         hotD1,
			h4:         H4Snapshot{CI: 40},
			w1Bias:     "short",
			d1Bias:     "long",
			setup:      SetupSnapshot{InValueZone: true, H4Aligned: true},
			wantBucket: "watch",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bucket, _ := Classify(tt.d1, tt.h4, W1Snapshot{}, tt.setup, tt.d1Bias, tt.w1Bias)
			assert.Equal(t, tt.wantBucket, bucket)
		})
	}
}

func TestClassify_DemotionNotes(t *testing.T) {
	d1 := D1Snapshot{ADX: 15, CI: 70}
	_, notes := Classify(d1, H4Snapshot{}, W1Snapshot{}, SetupSnapshot{}, "long", "short")

	assert.Contains(t, notes, "ADX dropped below 20")
	assert.Contains(t, notes, "CI crossed above 65")
	assert.Contains(t, notes, "W1 EMA flipped against D1 bias")
}

func TestClassify_NoDemotionNotesWhenHealthy(t *testing.T) {
	d1 := D1Snapshot{ADX: 30, CI: 40}
	_, notes := Classify(d1, H4Snapshot{CI: 40}, W1Snapshot{}, SetupSnapshot{}, "long", "long")
	assert.Empty(t, notes)
}
