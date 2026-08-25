// Package risk defines Trader's risk-admission contract (ADR-006,
// ADR-029, issue #180, M4-05): the seam that admits or rejects one
// order.Proposal before it becomes an order.Request and reaches
// broker.Account.Submit.
//
// # Scope
//
// Engine.Evaluate consumes an Input (a Proposal plus the current
// Account state) and returns a Decision — a strict approve/reject
// result, never an adjusted proposal (ADR-029). Every injected Rule is
// evaluated, in the order given; a Decision aggregates every rule's
// violations and warnings rather than stopping at the first one, so
// reports and backtest manifests can show everything a proposal got
// wrong, not just the first thing.
//
// This package defines no concrete Rule: position sizing (#181, which
// per #179's own resolution happens before execution planning, not as
// a Rule this package evaluates) and every admission policy —
// per-trade loss (#182), exposure/position-limit (#183), leverage/
// margin (#184) — are each their own issue, implementing Rule as a
// pure evaluator.
//
// # Dependency direction
//
// risk depends only on order, account, and context — never on broker
// or execution (ADR-006's own "execution and risk are separate
// stages, neither depends on the other" rule, package-boundaries.org).
// See boundary_test.go for the mechanical guard.
package risk
