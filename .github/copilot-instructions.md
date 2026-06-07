# Copilot Instructions

## Build, test, and lint commands

```bash
make vet                 # go vet ./...
make test                # full Go test suite
make build               # build bin/trader using the current ui/dist assets
make build-full          # rebuild the Svelte UI, then rebuild bin/trader
make cover               # write coverage.out and print function coverage
make cover-html          # write coverage.out and coverage.html
make test-blackbox       # run tests tagged blackbox

# Run one test
go test ./... -run TestName

# Run one test in a specific package
go test ./service -run TestName

# Strategy sweep / broad runtime smoke for service strategies
make sweep

# Optional network-hitting Dukascopy tests
TRADER_RUN_DUKASCOPY_TESTS=1 go test ./...
```

If you touch the embedded UI, install deps in `ui/` first on fresh
clones (`cd ui && npm ci`) and then use `make build-full` or `cd ui &&
npm run build`. For UI-only checks, `cd ui && npm run check` runs the
Svelte/TypeScript checker.

## High-level architecture

- `cmd/main.go` is the Cobra entrypoint. It wires subcommands and
  blank-imports the data provider and strategy packages so their
  `init()` registration runs before config-driven execution.

- `service/` is the protocol-agnostic business layer shared by CLI
  commands, `api/rest`, and `api/mcp`. Keep trading, order, replay,
  journal, and bot logic here; presentation layers should stay thin.

- Backtest flow is: YAML config (`Config` / `RunConfig`) ->
  `GetBacktests()` merges defaults and resolves strategy/exit/regime
  -> `service.RunBacktest()` builds a fresh `Trader` + `Broker` +
  `Account` -> `Trader.Backtest()` iterates candle data and drains
  broker events -> summaries/reports are written by `cmd/backtest` or
  exposed through REST.

- The data pipeline is centered on `DataManager`: it scans store
  inventory, builds a wantlist, plans missing work, downloads
  Dukascopy ticks, and builds canonical M1/H1/D1 candle
  files. `service/data.go` can also import OANDA candles into the same
  store layout.

- `trader serve` is the long-running daemon path: it creates a
  `service.Service`, starts the REST API, mounts embedded UI assets
  from `ui/dist`, and runs the live journal stream with reconnect
  backoff.

- Live trading runs through `service.RunLiveStrategy()`: fetch current
  OANDA prices, load open trades, ask the strategy for a `LivePlan`,
  execute closes first, then place a new market order if
  requested. Portfolio mode runs one goroutine per instrument and
  wraps strategies with a shared drawdown circuit breaker.

## MCP server

- The repository already includes a local stdio MCP server entry in
  `.mcp.json` named `trader`, backed by `trader mcp serve`.

- `trader mcp serve` exposes typed trader tools over stdio. Without an
  OANDA token it is backtest-only; with `--token` it enables live
  account/trade tools; write operations additionally require
  `--enable-write`.

- When adding MCP guidance or examples, keep them aligned with the
  existing local config shape:

```json
{
  "mcpServers": {
    "trader": {
      "type": "stdio",
      "command": "trader",
      "args": ["mcp", "serve"]
    }
  }
}
```

## Key conventions

- Engine/accounting code uses fixed-point domain types (`Price`,
  `Money`, `Rate`, `Units`) instead of floats. Convert at boundaries;
  do not introduce float-based accounting in core logic.

- Strategies self-register with `RegisterStrategy(...)` in package
  `init()` functions. A new strategy is not usable until it is both
  registered in its package and blank-imported from `cmd/main.go`.

- Backtest configs are YAML with top-level `defaults` plus per-run
  `runs[]`. Defaults cascade into each run, and empty `exit` /
  `regime` sections intentionally resolve to `NoopExit` /
  `NoopRegime`.

- Backtest outputs are intentionally stable: report filenames are
  `<run-name>-<config-hash>`, and regression comparisons in
  `cmd/backtest/cmd_regress.go` use exact numeric equality because the
  engine is built on scaled integers.

- Configuration precedence is important. The root command merges
  `/etc/trader/*.yml`, `~/.config/trader/*.yml`, then an explicit root
  `--config` file. Most OANDA-facing commands then apply explicit
  flags over global config, then environment variables, with
  `~/.config/oanda/pat.txt` as the final token fallback.

- Preserve the service boundary: CLI handlers under `cmd/` and HTTP
  handlers under `api/rest` should parse inputs, call typed `service`
  methods, and map results/errors at the edge instead of
  reimplementing business logic.

- Use OANDA instrument names like `EUR_USD` in live/config
  surfaces. Some storage paths normalize instruments by removing
  underscores (`EURUSD`); reuse existing helpers instead of
  hand-rolling conversions.

- The Go binary embeds `ui/dist`. `make build` assumes those assets
  already exist; use `make build-full` when UI assets need to be
  refreshed.
