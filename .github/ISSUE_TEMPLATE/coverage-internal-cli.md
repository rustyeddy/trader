---
name: "Add coverage for internal/cli"
about: "Create unit tests for internal/cli to reach 80% coverage"
title: "Test coverage: internal/cli below 80%"
assignees: ["copilot"]
---

## Coverage status
- Package: `internal/cli`
- Coverage: no unit tests (coverage below 80%)
- Target: >= 80%

## Task
- Add unit tests for `internal/cli` to raise coverage to at least 80%.
- Use `testify` for all new unit tests.
- Do not change existing behavior.
  - Prefer testing cobra command wiring, subcommand presence, and flag defaults.

## Acceptance criteria
- `go test ./...` passes.
- `internal/cli` coverage is >= 80%.
