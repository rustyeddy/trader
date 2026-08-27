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
// with no rules always allows structurally valid input. Every rule
// must be non-nil with a non-empty Name(); NewEngine rejects the
// engine outright rather than deferring the panic/unattributable-
// finding this would otherwise cause to a later Evaluate call.
func NewEngine(rules ...Rule) (Engine, error) {
	cp := make([]Rule, len(rules))
	for i, r := range rules {
		if r == nil {
			return nil, fmt.Errorf("%w: rule %d is nil", ErrInvalidRule, i)
		}
		if r.Name() == "" {
			return nil, fmt.Errorf("%w: rule %d has an empty name", ErrInvalidRule, i)
		}
		cp[i] = r
	}
	return &engine{rules: cp}, nil
}

// Evaluate implements Engine. It validates in before any rule runs
// (checkInput) — malformed input is rejected outright rather than
// silently producing an allowed Decision merely because e has no
// rules, or leaving every Rule to re-implement the same structural
// checks. Every RuleResult/Violation/Warning's own Rule field is
// normalized to the producing Rule's Name(), overriding whatever the
// Rule itself set: attribution is this Engine's own authoritative
// responsibility (issue #180's own acceptance criteria), not something
// a concrete Rule can get wrong or omit.
func (e *engine) Evaluate(ctx context.Context, in Input) (Decision, error) {
	if err := ctx.Err(); err != nil {
		return Decision{}, err
	}

	validProposal, err := checkInput(in)
	if err != nil {
		return Decision{}, err
	}
	validInput := Input{Proposal: validProposal, Account: in.Account}

	decision := Decision{
		Allowed:     true,
		RuleResults: make([]RuleResult, 0, len(e.rules)),
	}

	for _, rule := range e.rules {
		if err := ctx.Err(); err != nil {
			return Decision{}, err
		}

		result, err := rule.Evaluate(ctx, validInput)
		if err != nil {
			return Decision{}, fmt.Errorf("risk: rule %q: %w", rule.Name(), err)
		}

		result.Rule = rule.Name()
		for i := range result.Violations {
			result.Violations[i].Rule = rule.Name()
		}
		for i := range result.Warnings {
			result.Warnings[i].Rule = rule.Name()
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
