# Configuration facility

This project currently has two configuration entry points:

1. CLI/global runtime settings (`RootConfig`)
2. Simulation file settings (`AppConfig`)

## 1) Global runtime settings (`RootConfig`)

`RootConfig` is defined in `/home/runner/work/trader/trader/app_config.go` and is created in `/home/runner/work/trader/trader/cmd/main.go`.

### Default values

These values are wired as persistent CLI flags in `NewRootCmd()`:

| Field | CLI flag | Default |
|---|---|---|
| `ConfigPath` | `--config` | `""` (empty) |
| `DBPath` | `--db` | `"./trader.db"` |
| `LogLevel` | `--log-level` | `"info"` |
| `NoColor` | `--no-color` | `false` |
| `GlobalPath` | _(no CLI flag currently)_ | `""` (Go zero value) |

### How to access

- Root command builds one shared config pointer:
  - `rc := &trader.RootConfig{}`
- Subcommands receive that pointer:
  - `backtest.New(rc)`
  - `data.New(rc)`
  - `replay.New(rc)`
- Each command reads fields from that same `*RootConfig`.

### How to modify

- Preferred: pass flags at runtime, for example:
  - `trader --log-level debug --db ./my.db`
- In code: update fields on the shared pointer before using it.

## 2) Simulation file settings (`AppConfig`)

`AppConfig` is defined in `/home/runner/work/trader/trader/app_config.go`.

### Default values from `Default()`

`Default()` returns:

- `Account.ID`: `"SIM-001"`
- `Account.Currency`: `"USD"`
- `Account.Balance`: `100000`
- `Strategy.RiskPercent`: `0.01`
- `Strategy.Instrument`: `"EURUSD"`
- `Strategy.StopPips`: `20`
- `Strategy.TargetPips`: `40`
- `Simulation.InitialBid`: `1.0849`
- `Simulation.InitialAsk`: `1.0851`
- `Journal.Type`: `"csv"`
- `Journal.TradesFile`: `"./trades.csv"`
- `Journal.EquityFile`: `"./equity.csv"`

### How to access and modify

- Create defaults: `cfg := trader.Default()`
- Modify fields directly (for example `cfg.Strategy.RiskPercent = 0.02`)
- Load from file: `trader.LoadFromFile("config.yaml")`
- Save to file: `cfg.SaveToFile("config.yaml")`
- Validate: `cfg.Validate()`

## Current package-level global

There is also an internal package global in `/home/runner/work/trader/trader/trader_globals.go`:

- `store` defaults to base dir `"/home/rusty/src/trader/tmp"`

This variable is unexported. It is modified internally (for example, tests swap it to a temp store). External packages cannot set it directly.
