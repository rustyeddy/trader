package risk

import (
	"context"
	"errors"
	"testing"

	"github.com/rustyeddy/trader/num"
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
	e, err := NewEngine()
	require.NoError(t, err)
	decision, err := e.Evaluate(context.Background(), testInput(t))
	require.NoError(t, err)
	assert.True(t, decision.Allowed)
	assert.Empty(t, decision.Violations)
	assert.Empty(t, decision.RuleResults)
}

// TestEngineRejectsMalformedInputEvenWithNoRules is a regression for
// review feedback on PR #195: with zero rules, Evaluate previously
// returned Allowed: true for any Input, including a structurally
// invalid Proposal or an Account/Proposal mismatch, since nothing ever
// checked Input itself. Malformed input is not a policy violation and
// must be rejected before rule evaluation, independent of how many
// rules are configured.
func TestEngineRejectsMalformedInputEvenWithNoRules(t *testing.T) {
	e, err := NewEngine()
	require.NoError(t, err)

	_, err = e.Evaluate(context.Background(), Input{})
	require.ErrorIs(t, err, ErrInvalidInput)
}

func TestEngineRejectsAccountIDMismatch(t *testing.T) {
	e, err := NewEngine()
	require.NoError(t, err)

	listing := mustEurUsdListing(t)
	proposalAccount := mustAccountID(t)
	otherAccount := mustOtherAccountID(t)

	in := Input{
		Proposal: mustProposal(t, proposalAccount, listing),
		Account:  mustSnapshot(t, otherAccount, listing),
	}
	_, err = e.Evaluate(context.Background(), in)
	require.ErrorIs(t, err, ErrInvalidInput)
}

func TestEngineRejectsListingProviderAccountBrokerMismatch(t *testing.T) {
	e, err := NewEngine()
	require.NoError(t, err)

	accountID := mustAccountID(t)
	simListing := mustEurUsdListingForProvider(t, "sim")
	otherListing := mustEurUsdListingForProvider(t, "alpaca")

	in := Input{
		Proposal: mustProposal(t, accountID, otherListing),
		Account:  mustSnapshot(t, accountID, simListing),
	}
	_, err = e.Evaluate(context.Background(), in)
	require.ErrorIs(t, err, ErrInvalidInput)
}

func TestEngineAllRulesPassAllows(t *testing.T) {
	e, err := NewEngine(passingRule("a"), passingRule("b"))
	require.NoError(t, err)
	decision, err := e.Evaluate(context.Background(), testInput(t))
	require.NoError(t, err)
	assert.True(t, decision.Allowed)
	require.Len(t, decision.RuleResults, 2)
}

func TestEngineOneViolationRejects(t *testing.T) {
	e, err := NewEngine(passingRule("a"), violatingRule("b", "exceeds limit"))
	require.NoError(t, err)
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

	e, err := NewEngine(a, b, c)
	require.NoError(t, err)
	decision, err := e.Evaluate(context.Background(), testInput(t))
	require.NoError(t, err)

	assert.False(t, decision.Allowed)
	require.Len(t, decision.Violations, 2)
	assert.Equal(t, []string{"a", "b", "c"}, calls, "every rule must run, in order, regardless of earlier violations")
}

func TestEngineAggregatesWarningsEvenWhenAllowed(t *testing.T) {
	e, err := NewEngine(passingRule("a"), warningRule("b", "close to limit"))
	require.NoError(t, err)
	decision, err := e.Evaluate(context.Background(), testInput(t))
	require.NoError(t, err)
	assert.True(t, decision.Allowed, "a warning alone must not reject")
	require.Len(t, decision.Warnings, 1)
	assert.Equal(t, "b", decision.Warnings[0].Rule)
}

func TestEngineRuleResultsPreserveOrderAndIdentity(t *testing.T) {
	e, err := NewEngine(passingRule("first"), violatingRule("second", "bad"), passingRule("third"))
	require.NoError(t, err)
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

	e, err := NewEngine(a, b)
	require.NoError(t, err)
	_, err = e.Evaluate(context.Background(), testInput(t))
	require.NoError(t, err)
	assert.Equal(t, []string{"z", "a"}, calls, "Engine must not re-sort rules, e.g. alphabetically")
}

func TestEngineRulePropagatesEvaluationError(t *testing.T) {
	boom := errors.New("boom")
	e, err := NewEngine(passingRule("a"), &fakeRule{name: "broken", err: boom})
	require.NoError(t, err)
	_, err = e.Evaluate(context.Background(), testInput(t))
	require.ErrorIs(t, err, boom)
}

func TestEngineStopsAtEvaluationError(t *testing.T) {
	var calls []string
	broken := &fakeRule{name: "broken", err: errors.New("boom")}
	after := passingRule("after")
	after.calls = &calls

	e, err := NewEngine(broken, after)
	require.NoError(t, err)
	_, err = e.Evaluate(context.Background(), testInput(t))
	require.Error(t, err)
	assert.Empty(t, calls, "a rule after one that errors must not run — an evaluation error is not a domain outcome to aggregate past")
}

func TestEnginePropagatesCancelledContext(t *testing.T) {
	e, err := NewEngine(passingRule("a"))
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = e.Evaluate(ctx, testInput(t))
	require.ErrorIs(t, err, context.Canceled)
}

// TestNewEngineCopiesRulesDefensively proves NewEngine is not aliased
// to the caller's own backing slice: mutating the slice after
// construction must not change what a later Evaluate call sees.
func TestNewEngineCopiesRulesDefensively(t *testing.T) {
	rules := []Rule{passingRule("a")}
	e, err := NewEngine(rules...)
	require.NoError(t, err)

	rules[0] = violatingRule("a", "mutated after construction")

	decision, err := e.Evaluate(context.Background(), testInput(t))
	require.NoError(t, err)
	assert.True(t, decision.Allowed, "Engine must evaluate the rule it was constructed with, not a later mutation of the caller's slice")
}

// TestEnginePreservesAdverseDistanceThroughValidation is a regression
// found while implementing #182 (per-trade loss): Engine.Evaluate
// rebuilds a validated Input from checkInput's own revalidated
// Proposal, and an earlier version of that rebuild
// (Input{Proposal: validProposal, Account: in.Account}) silently
// dropped every other Input field, including AdverseDistance, before
// any Rule ever saw it.
func TestEnginePreservesAdverseDistanceThroughValidation(t *testing.T) {
	seen := make(chan *num.Price, 1)

	e, err := NewEngine(captureAdverseDistanceRule{seen: seen})
	require.NoError(t, err)

	dist := num.MustParsePrice("0.005")
	in := testInput(t)
	in.AdverseDistance = &dist

	_, err = e.Evaluate(context.Background(), in)
	require.NoError(t, err)

	select {
	case got := <-seen:
		require.NotNil(t, got, "AdverseDistance must reach the Rule, not be silently dropped by Engine's own input revalidation")
		assert.True(t, got.Equal(dist))
	default:
		t.Fatal("rule was never invoked")
	}
}

// captureAdverseDistanceRule records the Input.AdverseDistance it was
// actually called with, for TestEnginePreservesAdverseDistanceThroughValidation.
type captureAdverseDistanceRule struct {
	seen chan *num.Price
}

func (captureAdverseDistanceRule) Name() string { return "capture" }

func (r captureAdverseDistanceRule) Evaluate(ctx context.Context, in Input) (RuleResult, error) {
	r.seen <- in.AdverseDistance
	return RuleResult{}, nil
}

// TestNewEngineRejectsNilRule and TestNewEngineRejectsEmptyRuleName
// are regressions for review feedback (Copilot + Rusty) on PR #195:
// NewEngine previously accepted a nil Rule, which would panic the
// first time Evaluate called Name()/Evaluate() on it, and accepted a
// Rule with an empty Name(), which would silently produce
// unattributable findings.
func TestNewEngineRejectsNilRule(t *testing.T) {
	_, err := NewEngine(passingRule("a"), nil)
	require.ErrorIs(t, err, ErrInvalidRule)
}

func TestNewEngineRejectsEmptyRuleName(t *testing.T) {
	_, err := NewEngine(passingRule(""))
	require.ErrorIs(t, err, ErrInvalidRule)
}

// TestEngineNormalizesRuleAttribution proves Engine, not the concrete
// Rule, is authoritative for RuleResult/Violation/Warning.Rule: even a
// Rule that reports the wrong name (or none) for its own findings has
// its output corrected to match Rule.Name() (review feedback on PR
// #195).
func TestEngineNormalizesRuleAttribution(t *testing.T) {
	misattributed := &fakeRule{
		name: "real-name",
		result: RuleResult{
			Rule:       "wrong-name",
			Violations: []Violation{{Rule: "wrong-name", Message: "bad"}},
			Warnings:   []Warning{{Rule: "wrong-name", Message: "heads up"}},
		},
	}
	e, err := NewEngine(misattributed)
	require.NoError(t, err)

	decision, err := e.Evaluate(context.Background(), testInput(t))
	require.NoError(t, err)

	require.Len(t, decision.RuleResults, 1)
	assert.Equal(t, "real-name", decision.RuleResults[0].Rule)
	require.Len(t, decision.Violations, 1)
	assert.Equal(t, "real-name", decision.Violations[0].Rule)
	require.Len(t, decision.Warnings, 1)
	assert.Equal(t, "real-name", decision.Warnings[0].Rule)
}
