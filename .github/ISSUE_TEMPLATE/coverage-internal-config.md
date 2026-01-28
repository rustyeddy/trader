---
name: "Increase coverage for internal/config"
about: "Create unit tests for internal/config to reach 80% coverage"
title: "Test coverage: internal/config below 80%"
---

## Coverage status
- Package: `internal/config`
- Coverage: 78.4% (from `go test ./... -cover`)
- Target: >= 80%

## Task
- Add or extend unit tests for `internal/config` to raise coverage to at least 80%.
- Use `testify` for all new unit tests.
- Do not change existing behavior.
- Assign this issue to `copilot`.

## Acceptance criteria
- `go test ./...` passes.
- `internal/config` coverage is >= 80%.
