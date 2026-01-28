---
name: "Increase coverage for internal/cli"
about: "Create unit tests for internal/cli to reach 80% coverage"
title: "Test coverage: internal/cli below 80%"
---

## Coverage status
- Package: `internal/cli`
- Coverage: 0% (no unit tests)
- Target: >= 80%

## Task
- Add unit tests for `internal/cli` to raise coverage to at least 80%.
- Use `testify` for all new unit tests.
- Do not change existing behavior.
- Prefer testing cobra command wiring, subcommand presence, and flag defaults.
- Assign this issue to `copilot`.

## Acceptance criteria
- `go test ./...` passes.
- `internal/cli` coverage is >= 80%.
