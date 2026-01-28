---
name: "Increase coverage for cmd/trader"
about: "Create unit tests for cmd/trader to reach 80% coverage"
title: "Test coverage: cmd/trader below 80%"
---

## Coverage status
- Package: `cmd/trader` (main wrapper)
- Coverage: 0% (no unit tests)
- Target: >= 80%

## Task
- Add unit tests for `cmd/trader` to raise coverage to at least 80%.
- Use `testify` for all new unit tests.
- Do not change existing behavior.
- Consider verifying `main` delegates to `cli.Execute` via minimal seams or refactors that preserve behavior.
- Assign this issue to `copilot`.

## Acceptance criteria
- `go test ./...` passes.
- `cmd/trader` coverage is >= 80%.
