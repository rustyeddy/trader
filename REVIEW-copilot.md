# Software Review: `rustyeddy/trader`

## Executive Summary
`trader` is a well-structured Go trading platform that combines backtesting, paper/live execution (OANDA), a REST API, an embedded Svelte UI, and MCP tooling in a single repository. The core design is modular and pragmatic, with clear domain modeling (fixed-point numeric types), extensive automated tests, and practical operational documentation.

Overall assessment: **strong engineering foundation**, with the biggest opportunities in **coverage depth for some subsystems**, **UI build/tooling consistency**, and **operational hardening for live-trading workflows**.

## What Is Working Well

### 1) Clear architecture and module boundaries
- CLI entrypoints are organized under `cmd/` with focused command packages.
- Business logic is separated into `service/` and core engine packages.
- API presentation (`api/rest`) keeps HTTP concerns separate from service logic.
- Strategy and data-provider extensibility is cleanly implemented via registries/factories.

### 2) Good domain modeling for trading correctness
- Fixed-point numeric types (`Price`, `Money`, `Rate`, `Units`) avoid float drift.
- Core accounting invariants are explicit in docs and reflected in test coverage.
- Backtest lifecycle is cohesive: config → data → strategy → broker → account → reporting.

### 3) Strong testing culture and CI readiness
- Large test surface across core engine components, strategies, and APIs.
- `Makefile` provides standard developer workflows (`vet`, `test`, `cover`, `test-blackbox`).
- Current baseline checks pass locally for vet/test/build.

### 4) Practical product packaging
- Single binary plus embedded UI is convenient for deployment.
- Presence of deployment artifacts (`deploy/`, Docker/systemd examples) supports real usage.
- Useful docs set (`README`, architecture/configuration docs, roadmap) helps onboarding.

## Key Risks / Gaps

### 1) Uneven coverage across packages
- Total statement coverage is ~**49.2%**.
- Some important areas are lightly covered or uncovered in standard runs (notably several `cmd/*`, `api/mcp`, and parts of `service`).
- This creates regression risk in user-facing integration paths.

### 2) UI build/tooling fragility
- Building UI currently emits compatibility warnings (Svelte runtime export warnings).
- While build completes, these warnings indicate dependency/version drift risk.
- If unaddressed, future toolchain updates may cause hard build failures.

### 3) Live-trading resiliency still maturing
- Roadmap highlights meaningful live concerns (reconnect behavior, netting/FIFO handling, retry logic, crash recovery).
- These are important for production safety and should be prioritized relative to feature expansion.

### 4) Default runtime assumptions could surprise users
- Default data directory and external dependency assumptions are practical for server use but may be rough for first-time local usage.
- A more explicit “local dev defaults” profile could reduce friction.

## Recommended Next Priorities
1. **Increase integration coverage** for `api/mcp`, selected `cmd/*` flows, and live service paths.
2. **Stabilize UI dependency versions** and remove current build warnings.
3. **Harden live lifecycle behavior** (startup reconciliation, netting-aware mode, retry/backoff).
4. **Add a small smoke-test matrix** for critical end-to-end commands (`backtest`, `serve`, `live run --dry-run`).

## Final Verdict
This repository is in good shape and demonstrates thoughtful engineering for a trading system where correctness matters. The architecture and core domain logic are solid. The most valuable improvements now are in **coverage completeness** and **live-operational robustness**, rather than broad structural rewrites.
