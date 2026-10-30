---
name: go-testing
description: Write, review, and improve idiomatic deterministic Go tests in Trader using Testify, with strong boundary/error coverage and matching _test.go files.
---

# Trader Go Testing

Use this skill whenever creating, modifying, or reviewing Go tests. Follow applicable `AGENTS.md` instructions too. The test's job is to prove behavior and catch regressions, not merely increase a coverage number.

## Organization and assertions

- Use `github.com/stretchr/testify/require` for prerequisites and fatal assertions; use `github.com/stretchr/testify/assert` for independent checks.
- Put tests in the corresponding `*_test.go` file beside the source (`intent.go` → `intent_test.go`); create a separate integration test file only when behavior spans multiple files/packages.
- Prefer same-package tests for private implementation behavior; use external `_test` packages when testing the exported API as a consumer.
- Use descriptive `TestXxx_Scenario` names and table-driven subtests when they clarify related cases; don't force unrelated scenarios into tables.
- Test observable behavior and invariants; avoid brittle assertions against incidental implementation details.

## Coverage and cases

- Strive for as close to **100% meaningful coverage** as practical, with particular attention to domain logic and financial calculations. Respect repository-required coverage gates; never add meaningless assertions just to inflate coverage.
- Cover happy paths, invalid input, errors, zero values, first/last items, empty and single-element collections, boundaries, cancellation and early termination.
- Test both sides of a condition. Where relevant, cover overflow/rounding boundaries, known-zero vs unknown, positive/negative amounts, long/short and open/close/reversal transitions.
- Every bug fix needs a regression test that fails against the broken implementation. Test error wrapping/classification with `require.ErrorIs` or `require.ErrorAs` when appropriate.
- Favor explicit input/output expectations and small fixtures; compare exact monetary and price values without floats or approximate assertions when exact arithmetic is required.

## Deterministic, isolated tests

- Use synthetic bars with fixed timestamps and random seeds; avoid network access, real sleeps, wall-clock assumptions, and production data paths.
- Use `t.TempDir()` for file fixtures and `t.Setenv()` for controlled environment changes. Do not touch `/srv/trading` or rely on developer secrets or environment.
- Keep test state independent; use `t.Parallel()` only when safe. Use the race detector for concurrent changes.
- For iterators, test all items in order, empty input, cancellation, error propagation and early break. For market data, include gaps, duplicates, time ordering, and multi-instrument behavior where applicable.
- For trading/execution tests, verify intent validity, risk/rejection outcomes, no-look-ahead timing, fill price and costs, financial reconciliation, and deterministic repeatability when those behaviors are in scope.

## Verify each change

- During iteration run targeted `go test ./<package>`; before finishing run `go test ./...`, `go vet ./...`, and `go test -race ./...` when feasible. Report any check not run or failures observed.
- For `examples/design`, run `make check && make run` from that directory after every change. Keep the synthetic example executable throughout the design phase.
- Inspect coverage with `go test -cover ./...` or focused `-coverprofile` when useful; explain meaningful untested paths instead of concealing them.
- Keep scope tight: tests should support the approved API and TODO, not motivate new production abstractions.
