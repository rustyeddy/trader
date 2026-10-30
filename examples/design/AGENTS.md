# Trader Architecture Design

This directory is an experimental, executable design for
Trader's next architecture.

Read `/trader.org` and `todo.org` before making changes.

AI agents may propose architecture, but they should not independently
introduce or implement new architectural abstractions without our
approval.

## Principles

1. Simplicity is the primary architectural objective.
2. Design APIs before implementing functionality.
3. Prefer concrete types, ordinary functions, and small interfaces.
4. Never introduce abstractions for hypothetical future requirements.
5. Keep the prototype in one package until boundaries are agreed upon.
6. Keep CLI and transport logic outside domain functionality.
7. Preserve deterministic execution and causal backtest timing.
8. Use synthetic data to demonstrate proposed APIs.
9. Every iteration must compile, pass tests, and run.
10. Do not modify production packages during this design phase.

## Development Process

- Work on one TODO at a time.
- Discuss API changes before implementing them.
- Prefer deleting code over introducing additional layers.
- Add a small executable example or test for each new contract.
- Run `make check && make run` after each change.
- Do not migrate the design into production packages without approval.

## Review Criteria

Before accepting a change, ask:

- Can the API be understood from GoDoc alone?
- Does it reduce conceptual complexity?
- Is every interface justified by an actual use case?
- Can the same API serve backtesting and live trading?
- Does the change preserve deterministic behavior?
- Could this be implemented more simply?
