# Trader Agent Instructions

These instructions apply to Claude Code, GitHub Copilot, ChatGPT, and other contributors. Keep task context small: **do not read the entire architecture or ADR registry by default**.

## Before changing code

1. Read the linked GitHub issue and its acceptance criteria.
2. Read this file and `CONTRIBUTING.org` for the workflow and Definition of Done.
3. Inspect the affected packages and nearby tests.
4. **Selectively** consult architecture documentation and ADRs relevant to the change. Search the headings/index of `docs/arch/adr-decisions.org` by package, subject, or ADR number; read only matching sections. Consult `docs/arch/package-boundaries.org` when changing imports or public APIs, and the relevant sections of `docs/arch/trader-framework-architecture.org` or requirements when changing design or behavior.
5. Expand the reading scope only when dependencies, conflicts, or unresolved decisions require it. If design remains unclear, stop and flag `needs-design` rather than guessing.

Example: a historical-bar normalization change should inspect `marketdata`, `internal/marketdata`, their tests, and the relevant ADR-020 sections; it need not load broker or logging decisions.

## Development workflow

- Write idiomatic Go. Every change must correspond to a GitHub issue.
- For non-trivial work, post the implementation plan on the issue before coding when the issue workflow calls for plan review.
- Use a short-lived branch and open a PR; all changes require review before merge.
- Keep changes focused; document architecture changes and do not silently contradict accepted ADRs.
- Run `make check` (the canonical verification: tidy, fmt, vet, lint, test, race — same gates as CI) before pushing, or explicitly disclose any checks you could not run. Use narrower targets such as `go test ./pkg` only while iterating. Record each command and its exit status in the PR/handoff. See `docs/workflows/testing.org`. CI must pass.
- Preserve deterministic behavior and paper-trading-by-default safety. Strategies emit intents, not broker orders; risk and execution remain separate.
- See `CONTRIBUTING.org` for branch naming, testing, documentation, review, and Definition of Done. For multi-assistant handoffs and review roles, see `docs/workflows/ai-development.org`.

## GitHub issue and PR reviews

When asked to review a GitHub issue or pull request, **post the review/comments back to GitHub unless the user explicitly says not to**. Do not leave the substantive review only in chat.

For PR reviews:

1. Refresh the PR and record the exact current head SHA.
2. Read the linked issue and acceptance criteria.
3. Read the current diff and relevant implementation/tests; do not rely only on the PR description.
4. Read existing reviews, comments, and unresolved threads.
5. Check CI/workflow results for the exact current head.
6. Check mergeability.
7. Post findings directly on the PR.
8. Resolve threads only after verifying the fix on the current head.
9. If the head SHA changes after review, treat the prior merge-readiness decision as stale.

A PR is merge-ready only when all applicable gates pass:

- required CI is green on the current head;
- no unresolved blocking review threads remain;
- the branch is mergeable;
- linked issue requirements are satisfied;
- required human/owner review is complete.

Do not call a PR merge-ready while required CI is still running.

Prefer review comments over exercising merge authority unless explicitly asked to merge.

## Architecture and boundaries

Major subsystems should be capable of becoming separate services without requiring them to be separate services today.

Preferred layering. An arrow means "imports", as in `docs/arch/package-boundaries.org`:

```text
transport (CLI, MCP, REST)
    -> application service
    -> domain package
```

Executable composition roots (for example `cmd/internal/*cfg`) wire adapters, services and transports together; they are not a layer in this chain. Domain packages never import services, and services never import transports or composition roots.

CLI, MCP, REST, and other transports stay thin: parse input, call services, and format results. Do not put provider-domain logic, trade reconstruction, or financial derivation in command handlers when a service/domain boundary exists.

Provider-specific behavior belongs in adapters/configuration boundaries, not provider-neutral domain packages.

## Financial and provenance rules

- Never use binary floating point for canonical financial values.
- Preserve broker-reported values and their provenance.
- Derived values must identify how they were derived.
- Unknown is not zero; reported zero is known zero.
- Do not return a partial sum as a complete total.
- Use exact arithmetic where possible and round once using the documented rule.
- Tests for financial arithmetic must detect the bug they claim to prevent, including rounding-order and tie cases where relevant.
- Keep execution-quality/slippage analysis separate from realized trade P/L unless an ADR explicitly changes that rule.
- Spread cost is a first-class economic attribution but must not be double-counted when already embedded in broker-reported realized P/L.

## Review priorities

Prioritize, in order:

1. correctness and data integrity;
2. deterministic behavior and replayability;
3. financial arithmetic and sign conventions;
4. provenance and traceability;
5. unknown-vs-zero semantics;
6. idempotence and durable state transitions;
7. package/service boundaries;
8. provider-neutral domain modeling;
9. tests that prove the intended invariant rather than merely exercise a code path.

## Logging

- Use structured `log/slog` with standard levels error, warn, info, debug; no fatal logging helper. Process termination belongs to the composition root after logging.
- Default to stderr. Operator flags control destination (stderr, stdout, file), level, and text/JSON output.

## Testing

- New or changed behavior requires tests, including edge and failure cases; use Go `testify`.
- Target at least 85% coverage; exercise concurrency changes with the race detector.
- See `docs/workflows/workflows.org` for workflow details. `docs/workflows/testing.org` documents the verification commands.

## Reusable workflows

The canonical implement-issue and review-pr procedures are in `docs/workflows/ai-skills.org`.
