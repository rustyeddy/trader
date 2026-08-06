package id

import (
	"testing"
	"time"

	"github.com/rustyeddy/trader/clock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSourceIsZero(t *testing.T) {
	var s Source
	assert.True(t, s.IsZero())

	s = "strategy.macd_cross"
	assert.False(t, s.IsZero())
}

// TestMetadataRepresentsMultiStageWorkflow is the concrete version of the
// intent -> proposal -> order -> fill example from the package doc
// comment: one CorrelationID shared across every stage, each stage's
// CausationID pointing at the EventID immediately before it, and the first
// stage's CausationID left at the zero value since nothing caused it.
func TestMetadataRepresentsMultiStageWorkflow(t *testing.T) {
	c := clock.NewSimulated(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	g := NewGenerator(c, NewDeterministic(1, 2))

	workflow, err := GenerateCorrelationID(g)
	require.NoError(t, err)

	intentEvent, err := GenerateEventID(g)
	require.NoError(t, err)
	intent := Metadata{
		EventID:       intentEvent,
		CorrelationID: workflow,
		Source:        "strategy.macd_cross",
	}
	assert.True(t, intent.CausationID.IsZero(), "nothing caused the first event in a workflow")

	proposalEvent, err := GenerateEventID(g)
	require.NoError(t, err)
	proposal := Metadata{
		EventID:       proposalEvent,
		CorrelationID: workflow,
		CausationID:   intent.EventID,
		Source:        "execution.planner",
	}
	assert.True(t, proposal.CausationID.Equal(intent.EventID))

	orderEvent, err := GenerateEventID(g)
	require.NoError(t, err)
	order := Metadata{
		EventID:       orderEvent,
		CorrelationID: workflow,
		CausationID:   proposal.EventID,
		Source:        "broker.oanda",
	}
	assert.True(t, order.CausationID.Equal(proposal.EventID))

	fillEvent, err := GenerateEventID(g)
	require.NoError(t, err)
	fill := Metadata{
		EventID:       fillEvent,
		CorrelationID: workflow,
		CausationID:   order.EventID,
		Source:        "broker.oanda",
	}
	assert.True(t, fill.CausationID.Equal(order.EventID))

	// Every stage shares the one workflow correlation ID.
	for _, m := range []Metadata{intent, proposal, order, fill} {
		assert.True(t, m.CorrelationID.Equal(workflow))
	}

	// Each stage's causation chain is distinct from every other stage's
	// event, and no two events in the chain collide.
	seen := map[string]bool{}
	for _, ev := range []EventID{intent.EventID, proposal.EventID, order.EventID, fill.EventID} {
		assert.False(t, seen[ev.String()], "duplicate EventID in workflow chain")
		seen[ev.String()] = true
	}
}

func TestMetadataZeroValueFieldsAreZero(t *testing.T) {
	var m Metadata
	assert.True(t, m.EventID.IsZero())
	assert.True(t, m.CorrelationID.IsZero())
	assert.True(t, m.CausationID.IsZero())
	assert.True(t, m.Timestamp.IsZero())
	assert.True(t, m.Source.IsZero())
}
