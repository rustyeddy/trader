package id_test

import (
	"fmt"
	"time"

	"github.com/rustyeddy/trader/clock"
	"github.com/rustyeddy/trader/id"
)

// ExampleGenerator shows the deterministic-testing pattern: a Simulated
// clock paired with a seeded Deterministic entropy source produces the same
// identifier every time, with no wall-clock wait anywhere in the example.
func ExampleGenerator() {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c := clock.NewSimulated(start)
	g := id.NewGenerator(c, id.NewDeterministic(1, 2))

	run, err := id.GenerateRunID(g)
	if err != nil {
		fmt.Println("error:", err)
		return
	}

	fmt.Println(run.String()[:4]) // the "run_" prefix, deterministic regardless of entropy
	// Output:
	// run_
}

// ExampleParseRunID shows validating and round-tripping an identifier's
// text form.
func ExampleParseRunID() {
	c := clock.NewSimulated(time.Now())
	g := id.NewGenerator(c, id.NewDeterministic(1, 2))
	run, _ := id.GenerateRunID(g)

	parsed, err := id.ParseRunID(run.String())
	if err != nil {
		fmt.Println("error:", err)
		return
	}

	fmt.Println(parsed.Equal(run))
	// Output:
	// true
}

// ExampleMetadata shows tracing one workflow through several stages: every
// stage shares the same CorrelationID, and each stage's CausationID points
// at the EventID immediately before it.
func ExampleMetadata() {
	c := clock.NewSimulated(time.Now())
	g := id.NewGenerator(c, id.NewDeterministic(1, 2))

	workflow, _ := id.GenerateCorrelationID(g)
	intentEvent, _ := id.GenerateEventID(g)
	proposalEvent, _ := id.GenerateEventID(g)

	intent := id.Metadata{
		EventID:       intentEvent,
		CorrelationID: workflow,
		Source:        "strategy.macd_cross",
	}
	proposal := id.Metadata{
		EventID:       proposalEvent,
		CorrelationID: workflow,
		CausationID:   intent.EventID,
		Source:        "execution.planner",
	}

	fmt.Println(intent.CausationID.IsZero())
	fmt.Println(proposal.CausationID.Equal(intent.EventID))
	fmt.Println(proposal.CorrelationID.Equal(intent.CorrelationID))
	// Output:
	// true
	// true
	// true
}
