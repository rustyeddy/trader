# Trader: instructions for AI agents and contributors

Trader is a Go trading framework (backtesting, market data, simulated broker; live
execution is not available). This file says how to work in the repository. The
process (issue, branch, pull request, merge) is in
[docs/CONTRIBUTING.org](docs/CONTRIBUTING.org); the Go testing rules are in
[docs/workflows/ai-skills.org](docs/workflows/ai-skills.org); the decisions behind
the structure are the ADRs in [docs/arch/](docs/arch/adr-decisions.org).

AI agents may propose architecture. They must not introduce or implement a new
architectural abstraction (a layer, a registry, a general interface, a new
package for its own sake) without the owner's approval. Keep a design question in
the issue until it is approved, then implement it.

## Principles

1. Simplicity is the primary architectural objective: one readable call path.
2. Design the API before the implementation; the API should be clear from its
   GoDoc alone.
3. Prefer concrete types, ordinary functions and small interfaces. No
   abstraction for a hypothetical future requirement, and every interface needs a
   present use.
4. Prefer deleting code to adding layers. Do not add a package for a single file;
   add one when the code is reasonably complex.
5. Preserve deterministic behavior and causal backtest timing: time, identifiers
   and data order are controllable; a bar is usable only from its `AvailableAt`.
6. Exact numbers: prices, quantities and money are `num` values (ADR-004), never
   floats on the authoritative path.
7. Never invent data: no synthetic or carried-forward bars, and a partition with
   bad records is not complete data (ADR-090).

## Layering (ADR-091, implemented)

This is the structure the code has and the tests enforce; preserve it. ADR-091
records it and is Accepted; ADR-092, which records the package moves against
ADR-089, is still *Proposed* until the owner accepts it (#643), and ADR-089 is
accepted and amended in part. A change to a boundary, or a new layer, needs the
owner's approval, and do not treat a Proposed ADR as Accepted.

```
cmd/trader            transport: flags, environment, config file; no domain logic
  service/            application services: coordinate providers and domain services
    <domain>/service  domain service (marketdata/service): the domain's rules
      <domain>/internal   private implementation (marketdata/internal/canonical)
provider/<name>       one external system's integration (provider/oanda)
store                 the only place data files are read and written
marketdata, num, instrument   public models and values
```

- Dependencies point down only. Domain services do not call each other; an
  application service coordinates them. Public models import no service,
  transport or provider.
- A provider parses and maps; it applies no domain rule (rejection, availability,
  mid prices belong to the domain service).
- Paths are built by `internal/marketdata/layout` and are relative to the store's
  root. Nothing outside `store` opens, renames or walks data files.
- Each boundary is an architecture test (`marketdata/marketdata_test.go`,
  `provider/oanda/oanda_test.go`, `service/marketdata_test.go`,
  `store/store_test.go`, `cmd/trader/build_test.go`, `cmd/trader/status_test.go`). When you add a boundary, add
  its test and check that it fails when violated.
- The root package (`trader`) is the engine: orders, broker, account, strategy,
  scanner, scenarios. It runs on `marketdata.Bar`.
- `_archive/` is a donor of earlier code and documents, not live code. Port from it
  by moving and adapting, one layer at a time; do not import it.

## Security (ADR-078)

- Secrets come only from the environment (`TRADER_OANDA_TOKEN`), never a flag or
  the config file. A secret never appears in an error, a log or rendered output.
- Domain packages never call `os.Getenv`. `cmd/trader` is the only reader of the
  environment and the config file.
- Tests write only to temporary directories, never to `/srv/data` or
  `/srv/trading`. Give each test package `TestMain` with `storetest.Main` and call
  `storetest.Root(t)` in tests that touch data.
- Do not run live-network commands for the owner and do not print or commit
  credentials or downloaded private data.

## Working rules

- Work from a GitHub issue: `gh issue develop N --name <type>/N-<short> --base main
  --checkout`, one branch per iteration, and a pull request whose body says
  `Closes #N` (partial work gets its own issue, or says "Part of #M (does not
  close it)"). Leave the owner's uncommitted edits alone.
- Every new or changed function has tests: error and boundary cases, and a
  regression test that fails without the fix. Test layout: one test file per
  source file, named after it; cross-file tests and shared helpers in
  `<package>_test.go`.
- Before every pull request: `make check`, and the six EMAX backtest summaries
  must be unchanged: `go run ./cmd/trader 2>/dev/null | md5sum` prints
  `195e457a21def1223e7181c86cb824a4`.
- Merge only when the owner says "merge", only after reading the reviews and inline
  comments, and not after a new commit has been pushed since the owner's review.
- Report outcomes faithfully: a check not run, a failure, a skipped step.

## Review questions

- Can the API be understood from GoDoc alone?
- Does the change reduce conceptual complexity, or only move it?
- Is every interface justified by an actual use?
- Does it preserve deterministic behavior and the layering above?
- Could it be done more simply?
