---
name: "Increase coverage for sim"
about: "Create unit tests for sim to reach 80% coverage"
title: "Test coverage: sim below 80%"
assignees: ["copilot"]
---

## Coverage status
- Package: `sim`
- Coverage: 54.6% (from `go test ./... -cover`)
- Target: >= 80%

## Task
- Add or extend unit tests for `sim` to raise coverage to at least 80%.
- Use `testify` for all new unit tests.
- Do not change existing behavior.
- Assign this issue to `copilot`.

## Acceptance criteria
- `go test ./...` passes.
- `sim` coverage is >= 80%.
