package risk

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testInput(t *testing.T) Input {
	t.Helper()
	listing := mustEurUsdListing(t)
	accountID := mustAccountID(t)
	return Input{
		Proposal: mustProposal(t, accountID, listing),
		Account:  mustSnapshot(t, accountID, listing),
	}
}

func TestEngineNoRulesAlwaysAllows(t *testing.T) {
	e := NewEngine()
	decision, err := e.Evaluate(context.Background(), testInput(t))
	require.NoError(t, err)
	assert.True(t, decision.Allowed)
	assert.Empty(t, decision.Violations)
	assert.Empty(t, decision.RuleResults)
}

func TestEngineAllRulesPassAllows(t *testing.T) {
	e := NewEngine(passingRule("a"), passingRule("b"))
	decision, err := e.Evaluate(context.Background(), testInput(t))
	require.NoError(t, err)
	assert.True(t, decision.Allowed)
	require.Len(t, decision.RuleResults, 2)
}

func TestEngineOneViolationRejects(t *testing.T) {
	e := NewEngine(passingRule("a"), violatingRule("b", "exceeds limit"))
	decision, err := e.Evaluate(context.Background(), testInput(t))
	require.NoError(t, err)
	assert.False(t, decision.Allowed)
	require.Len(t, decision.Violations, 1)
	assert.Equal(t, "b", decision.Violations[0].Rule)
	assert.Equal(t, "exceeds limit", decision.Violations[0].Message)
}

// TestEngineEvaluatesEveryRuleNotFailFast is ADR-029's own central
// guarantee: a violation from one rule does not stop later rules from
// running, so a Decision aggregates every violation a proposal
// triggered, not just the first.
func TestEngineEvaluatesEveryRuleNotFailFast(t *testing.T) {
	var calls []string
	a := violatingRule("a", "first violation")
	a.calls = &calls
	b := violatingRule("b", "second violation")
	b.calls = &calls
	c := passingRule("c")
	c.calls = &calls

	e := NewEngine(a, b, c)
	decision, err := e.Evaluate(context.Background(), testInput(t))
	require.NoError(t, err)

	assert.False(t, decision.Allowed)
	require.Len(t, decision.Violations, 2)
	assert.Equal(t, []string{"a", "b", "c"}, calls, "every rule must run, in order, regardless of earlier violations")
}

func TestEngineAggregatesWarningsEvenWhenAllowed(t *testing.T) {
	e := NewEngine(passingRule("a"), warningRule("b", "close to limit"))
	decision, err := e.Evaluate(context.Background(), testInput(t))
	require.NoError(t, err)
	assert.True(t, decision.Allowed, "a warning alone must not reject")
	require.Len(t, decision.Warnings, 1)
	assert.Equal(t, "b", decision.Warnings[0].Rule)
}

func TestEngineRuleResultsPreserveOrderAndIdentity(t *testing.T) {
	e := NewEngine(passingRule("first"), violatingRule("second", "bad"), passingRule("third"))
	decision, err := e.Evaluate(context.Background(), testInput(t))
	require.NoError(t, err)

	require.Len(t, decision.RuleResults, 3)
	assert.Equal(t, "first", decision.RuleResults[0].Rule)
	assert.Equal(t, "second", decision.RuleResults[1].Rule)
	assert.Equal(t, "third", decision.RuleResults[2].Rule)
	assert.Empty(t, decision.RuleResults[0].Violations)
	require.Len(t, decision.RuleResults[1].Violations, 1)
	assert.Empty(t, decision.RuleResults[2].Violations)
}

func TestEngineRulesEvaluatedInGivenOrder(t *testing.T) {
	var calls []string
	a := passingRule("z")
	a.calls = &calls
	b := passingRule("a")
	b.calls = &calls

	e := NewEngine(a, b)
	_, err := e.Evaluate(context.Background(), testInput(t))
	require.NoError(t, err)
	assert.Equal(t, []string{"z", "a"}, calls, "Engine must not re-sort rules, e.g. alphabetically")
}

func TestEngineRulePropagatesEvaluationError(t *testing.T) {
	boom := errors.New("boom")
	e := NewEngine(passingRule("a"), &fakeRule{name: "broken", err: boom})
	_, err := e.Evaluate(context.Background(), testInput(t))
	require.ErrorIs(t, err, boom)
}

func TestEngineStopsAtEvaluationError(t *testing.T) {
	var calls []string
	broken := &fakeRule{name: "broken", err: errors.New("boom")}
	after := passingRule("after")
	after.calls = &calls

	e := NewEngine(broken, after)
	_, err := e.Evaluate(context.Background(), testInput(t))
	require.Error(t, err)
	assert.Empty(t, calls, "a rule after one that errors must not run — an evaluation error is not a domain outcome to aggregate past")
}

func TestEnginePropagatesCancelledContext(t *testing.T) {
	e := NewEngine(passingRule("a"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := e.Evaluate(ctx, testInput(t))
	require.ErrorIs(t, err, context.Canceled)
}

// TestNewEngineCopiesRulesDefensively proves NewEngine is not aliased
// to the caller's own backing slice: mutating the slice after
// construction must not change what a later Evaluate call sees.
func TestNewEngineCopiesRulesDefensively(t *testing.T) {
	rules := []Rule{passingRule("a")}
	e := NewEngine(rules...)

	rules[0] = violatingRule("a", "mutated after construction")

	decision, err := e.Evaluate(context.Background(), testInput(t))
	require.NoError(t, err)
	assert.True(t, decision.Allowed, "Engine must evaluate the rule it was constructed with, not a later mutation of the caller's slice")
}
