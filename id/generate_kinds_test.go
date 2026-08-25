package id

import (
	"testing"
	"time"

	"github.com/rustyeddy/trader/clock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestConcreteGenerateFunctionsMatchTheirKind exercises every one of the
// seven Generate<Kind> wrappers and confirms each produces an identifier
// carrying its own kind's prefix — the same defense against a copy-paste
// wiring mistake that TestConcreteParseFunctionsMatchTheirKind provides for
// the Parse<Kind> wrappers.
func TestConcreteGenerateFunctionsMatchTheirKind(t *testing.T) {
	tests := []struct {
		name     string
		prefix   string
		generate func(*Generator) (string, error)
	}{
		{"RunID", "run", func(g *Generator) (string, error) { v, err := GenerateRunID(g); return v.String(), err }},
		{"OrderID", "ord", func(g *Generator) (string, error) { v, err := GenerateOrderID(g); return v.String(), err }},
		{"FillID", "fil", func(g *Generator) (string, error) { v, err := GenerateFillID(g); return v.String(), err }},
		{"EventID", "evt", func(g *Generator) (string, error) { v, err := GenerateEventID(g); return v.String(), err }},
		{"CorrelationID", "cor", func(g *Generator) (string, error) { v, err := GenerateCorrelationID(g); return v.String(), err }},
		{"AccountID", "acc", func(g *Generator) (string, error) { v, err := GenerateAccountID(g); return v.String(), err }},
		{"IntentID", "int", func(g *Generator) (string, error) { v, err := GenerateIntentID(g); return v.String(), err }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := clock.NewSimulated(time.Now())
			g := NewGenerator(c, NewDeterministic(1, 2))

			got, err := tt.generate(g)
			require.NoError(t, err)
			require.Len(t, got, len(tt.prefix)+1+26)
			assert.Equal(t, tt.prefix, got[:len(tt.prefix)])
		})
	}
}
