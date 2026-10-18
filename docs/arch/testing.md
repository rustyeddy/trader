# Testing architecture and fixture ownership

This document defines **where test fixtures and regression baselines live** in Trader. It complements [the testing workflow](../workflows/testing.org), which defines verification commands and CI gates.

## Fixture placement

Use a **hybrid** layout:

- Root `testdata/`: shared fixtures used across packages, integration scenarios, and end-to-end regression baselines.
- Package-local `testdata/`: fixtures owned and consumed by one package.
- Small unit-test inputs: prefer inline Go values or table-driven cases over external files.

Keep a fixture at the narrowest location that avoids duplication. Do not copy shared fixture files into several packages merely to simplify relative paths. Do not reorganize existing fixtures solely to make the tree uniform.

Go excludes directories named `testdata` from package discovery; they are not automatically found from every package's working directory. Tests must resolve paths deliberately and portably, without depending on the shell's current directory or developer-specific absolute paths.

## Repository versus research data

- **Regression tests:** checked-in configurations, small input fixtures, and stable expected values belong in the Trader repository.
- **Generated test artifacts:** write to isolated temporary directories where possible; otherwise use a documented gitignored workspace. Never modify checked-in fixtures.
- **Research and official backtests:** configurations and results live under `/srv/trading`, outside the Trader source repository.
- Tests and CI must not read from, write to, or require `/srv/trading`. Avoid live provider/network dependencies in deterministic tests.

## End-to-end backtest regression

The EMA-cross regression is the reference example; see [issue #542](https://github.com/rustyeddy/trader/issues/542).

Proposed fixture layout:

```text
testdata/
├── raw/oanda/                 # Existing synthetic market-data fixture
└── backtest/ema-cross/
    ├── config.yml            # Complete regression run configuration
    └── expected.json         # Stable, verified baseline expectations
```

Required developer invocation, from the repository root:

```sh
go run ./cmd/trader backtest run --config ./testdata/backtest/ema-cross/config.yml
```

For this regression, **`--config` is the only CLI flag**: strategy, period, account/risk settings, fixture paths, output location/format, and other required settings come from the YAML. Paths must resolve consistently and remain portable; the run must not fall back to production data roots.

The intended `make test-backtest` target runs the regression unattended and compares actual results against `expected.json`. These files/target are **requirements of #542**, not claims that they already exist.

## Baseline assertions and updates

Assert behavior, not just successful execution. Include strategy identity, ordered trade direction and counts, fill prices, position sizes, realized P/L, and final equity where available. Capture expected financial numbers from a verified pre-refactor execution; never invent them.

Do not compare volatile run IDs, timestamps, or other incidental metadata as if they were deterministic financial outcomes. Keep output snapshots small and reviewable.

When a deliberate change (for example, bid/ask modeling in [#531](https://github.com/rustyeddy/trader/issues/531)) alters financial results:

1. Document why the expected behavior changed in the PR.
2. Independently verify the new numbers and invariants.
3. Update the committed baseline in the same reviewed change.
4. Preserve visibility into the old-versus-new financial behavior.

Do not silently regenerate expectations merely to make a failing test pass.

## Agent guidance

Before adding a fixture, decide whether it is shared or package-owned. Before changing backtest or market-data orchestration, run the applicable regression and preserve its established behavior unless the linked issue explicitly changes the contract. Follow [AGENTS.md](../../AGENTS.md) and [the testing workflow](../workflows/testing.org) for broader contribution and verification requirements.
