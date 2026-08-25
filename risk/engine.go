package risk

import (
	"context"
	"fmt"
)

// Rule is one composable risk policy: a pure evaluator over an Input,
// with no broker submission, network I/O, hidden configuration reads,
// or global randomness (issue #180's own acceptance criteria).
// Concrete rules — per-trade loss (#182), exposure/position-limit
// (#183), leverage/margin (#184) — each implement this interface; this
// package defines no concrete Rule of its own.
type Rule interface {
	// Name identifies this rule, used as Violation.Rule/Warning.Rule/
	// RuleResult.Rule so a Decision's findings are attributable to the
	// specific policy that produced them.
	Name() string

	// Evaluate reports in's outcome against this rule: an empty
	// RuleResult.Violations means in.Proposal passes. Evaluate returns
	// a non-nil error only when it cannot evaluate in at all (malformed
	// input), never merely because the proposal violates the rule —
	// that is RuleResult's own job to report.
	Evaluate(ctx context.Context, in Input) (RuleResult, error)
}

// Engine evaluates an Input against a fixed set of Rules and returns
// one aggregated Decision (ADR-029).
type Engine interface {
	Evaluate(ctx context.Context, in Input) (Decision, error)
}

// engine is the v0 reference Engine implementation (issue #180,
// M4-05): it evaluates every injected Rule, in the exact order given,
// and aggregates every rule's own findings into one Decision.
// Evaluation never stops at the first violation (ADR-029) and never
// re-sorts or parallelizes the rules — both for reproducibility.
type engine struct {
	rules []Rule
}

// NewEngine returns an Engine that evaluates rules, in the given
// order, on every call to Evaluate. rules may be empty — an Engine
// with no rules always allows.
func NewEngine(rules ...Rule) Engine {
	cp := make([]Rule, len(rules))
	copy(cp, rules)
	return &engine{rules: cp}
}

// Evaluate implements Engine.
func (e *engine) Evaluate(ctx context.Context, in Input) (Decision, error) {
	if err := ctx.Err(); err != nil {
		return Decision{}, err
	}

	decision := Decision{
		Allowed:     true,
		RuleResults: make([]RuleResult, 0, len(e.rules)),
	}

	for _, rule := range e.rules {
		if err := ctx.Err(); err != nil {
			return Decision{}, err
		}

		result, err := rule.Evaluate(ctx, in)
		if err != nil {
			return Decision{}, fmt.Errorf("risk: rule %q: %w", rule.Name(), err)
		}

		decision.RuleResults = append(decision.RuleResults, result)
		decision.Violations = append(decision.Violations, result.Violations...)
		decision.Warnings = append(decision.Warnings, result.Warnings...)
		if len(result.Violations) > 0 {
			decision.Allowed = false
		}
	}

	return decision, nil
}
