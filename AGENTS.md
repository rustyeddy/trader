# Trader Agent Instructions

These instructions apply to Claude Code, GitHub Copilot, ChatGPT, and other contributors. Keep task context small: **do not read the entire architecture or ADR registry by default**.

## Before changing code

1. Read the linked GitHub issue and its acceptance criteria.
2. Read this file and `CONTRIBUTING.org` for the workflow and Definition of Done.
3. Inspect the affected packages and nearby tests.
4. **Selectively** consult architecture documentation and ADRs relevant to the change. Search the headings/index of `docs/arch/adr-decisions.org` by package, subject, or ADR number; read only matching sections. Consult `docs/arch/package-boundaries.org` when changing imports or public APIs, and the relevant sections of `docs/arch/trader-framework-architecture.org` or requirements when changing design or behavior.
5. Expand the reading scope only when dependencies, conflicts, or unresolved decisions require it. If design remains unclear, stop and flag `needs-design` rather than guessing.

Example: a historical-bar normalization change should inspect `marketdata`, `internal/marketdata`, their tests, and the relevant ADR-020 sections; it need not load broker or logging decisions.

## Development and review

- Write idiomatic Go. Every change must correspond to a GitHub issue.
- Use a short-lived branch and open a PR; all changes require review before merge.
- Keep changes focused; document architecture changes and do not silently contradict accepted ADRs.
- Run `make check` before pushing, or explicitly disclose any checks you could not run. CI must pass.
- Preserve deterministic behavior and paper-trading-by-default safety. Strategies emit intents, not broker orders; risk and execution remain separate.
- See `CONTRIBUTING.org` for branch naming, testing, documentation, review, and Definition of Done.

## Logging

- Use structured `log/slog` with standard levels error, warn, info, debug; no fatal logging helper. Process termination belongs to the composition root after logging.
- Default to stderr. Operator flags control destination (stderr, stdout, file), level, and text/JSON output.

## Testing

- New or changed behavior requires tests, including edge and failure cases; use Go `testify`.
- Target at least 85% coverage; exercise concurrency changes with the race detector.
- See `docs/workflows/workflows.org` for workflow details. `docs/workflows/testing.org` is currently a placeholder.
