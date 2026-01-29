---
name: "Increase coverage for internal/strategies"
about: "Create unit tests for internal/strategies to reach 80% coverage"
title: "Test coverage: internal/strategies below 80%"
assignees: ["copilot"]
---

## Coverage status
- Package: `internal/strategies`
- Coverage: 45.5% (from `go test ./... -cover`)
- Target: >= 80%

## Task
- Add or extend unit tests for `internal/strategies` to raise coverage to at least 80%.
- Use `testify` for all new unit tests.
- Do not change existing behavior.

## Acceptance criteria
- `go test ./...` passes.
- `internal/strategies` coverage is >= 80%.
