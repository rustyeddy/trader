# Trader Roadmap

Items are roughly ordered by priority within each section. Nothing here is
committed to a timeline — this is a living document of known gaps and future
directions.

---

## Deployment

### Ansible Roles
Four roles covering the multi-node setup: `trader-common` (install binary,
user, dirs, NFS mount), `trader-live` (serve + live bot systemd units),
`trader-data` (nightly candle sync cron), `trader-backtest` (on-demand).
Separate `prod` and `demo` inventories; secrets in Ansible Vault with
distinct passwords per environment.

### prod vs demo Pipeline
Tagged releases deploy to real-money nodes; `main` edge builds deploy to
practice nodes. `make deploy-prod version=v1.2.3` requires an explicit tag;
`make deploy-demo` pulls the latest edge artifact.

### Raspberry Pi
Cross-compile targets in Makefile (`build-arm64`); `make deploy-pi
PI_HOST=pi1.local` rsync the binary and restart the unit. Candle data on a
shared NFS volume; SQLite journal on local USB SSD to reduce SD card wear.

---

## Live Trading

### Reconnect with Existing Open Trades
When the live runner restarts, `tickCounts` resets to zero so pre-existing
broker positions appear as age=1. The strategy may hold them far longer than
intended (or never close them via `hold_bars`).

**Fix options:**
- Query `openTime` from OANDA on startup; divide elapsed time by `tick_interval`
  to synthesize initial tick counts (no state file needed).
- Persist `tickCounts` to a small JSON state file after each tick; restore on
  startup (exact, handles multi-day sessions).

### Netting-Aware Strategy Mode
OANDA practice accounts enforce netting/FIFO per instrument — simultaneous long
and short on the same instrument net out rather than creating two positions.
Strategies that alternate sides need to be aware of this constraint.

**Fix options:**
- Add a `netting: true` config flag; runner closes opposite-side positions
  before opening a new one.
- Multi-instrument runner: manage multiple instruments in one loop so separate
  longs and shorts go on different pairs.

### FOK Order Retry
When a Fill-or-Kill order is cancelled by OANDA (now surfaces as an error), the
runner logs and skips. A configurable retry with jitter would improve fill rate
on fast-moving markets.

### Position Restore on Crash
Related to reconnect — if the runner exits uncleanly, any open positions remain
unmanaged until the next restart. A startup reconciliation step should detect
and adopt orphaned positions.

---

## Strategy Engine

### External / Plugin Strategies
Allow strategies to be loaded at runtime without modifying core code or
recompiling the trader binary.

**Preferred approaches:**
1. **Subprocess JSON stdio** — strategy is any executable; the runner spawns it
   and communicates via stdin/stdout newline-delimited JSON. Any language, fully
   isolated, crash-safe. Ideal for live trading.
2. **Starlark scripting** — `.star` files loaded at runtime, no compilation.
   Deterministic by design; good for backtesting reproducibility.
   Library: `go.starlark.net`.
3. **Yaegi** — interpreted Go; strategies use the existing `Strategy` interface
   with no new concepts for Go contributors.

**Rejected:** Go `plugin` package — requires identical Go version/build flags,
Linux-only, cannot be unloaded, fragile shared memory model.

**Config sketch (subprocess):**
```yaml
strategy:
  kind: external
  cmd: "./strategies/my_rsi"   # or "python strategies/rsi.py"
  params:
    period: 14
    threshold: 30
```

### Multi-Instrument Runner
Single runner loop managing several instruments concurrently — each with its own
price feed, position tracking, and strategy instance. Enables independent longs
on EUR_USD + GBP_USD without running two separate processes.

### Strategy Parameter Optimisation
Grid/random search over strategy params against historical data; emit a ranked
result table. Builds on the existing backtest infrastructure.

### Stop Strategy Modes
Add configurable stop-sizing modes so risk can adapt to streaks and edge quality.

- **Martingale** — increase risk/size after a loss so one win can recover prior
  losses. Reset to base risk after a profitable trade.
- **Anti-martingale** — increase risk/size after wins and reduce after losses
  (pyramiding into momentum, de-risking during drawdowns).
- **Kelly method** — size risk as a fraction derived from estimated win rate and
  payoff ratio (`f* = p - (1-p)/b`), typically using fractional Kelly in
  production to reduce volatility.

---

## Backtesting

### Walk-Forward Testing
Divide the date range into in-sample and out-of-sample windows; run optimisation
on in-sample, validate on out-of-sample, report combined equity curve. Prevents
overfitting to a single period.

### Portfolio Backtest
Run multiple instruments simultaneously with shared capital, margin accounting,
and correlation-aware position sizing.

### Slippage and Commission Model
Current backtest fills at the exact bar price. A configurable slippage model
(fixed pips, or random within spread) would give more realistic P/L estimates.

---

## UI

### Update Stop / Take on Open Position
The trades page side panel already has the form wired up; verify end-to-end
with a live account and add a confirmation step for safety.

### Live Bot Controls
Start / stop / configure the live runner from the UI rather than the CLI.
Requires a runner lifecycle API (`POST /api/v1/bot/start`, `DELETE /api/v1/bot`).

### Backtest Equity Curve Chart
The `/charts` page exists but is unpopulated. Wire it to the trade details from
a selected backtest summary to render an equity curve.

### Playwright UI Tests
Third blackbox test layer (after REST and MCP). Requires a running server;
use `npx playwright test` against `httptest.NewServer` or a local dev instance.

---

## Infrastructure

### SQLite Journal for Live Trading
The CSV journal works but SQLite (`live.db`) supports richer queries and
concurrent reads from the UI. The backend already exists; needs wiring into the
default serve config.

### Configurable Logging
Structured log levels per subsystem (runner, broker, service) via the serve
config; runtime level changes via an API endpoint.

### Docker / Systemd Packaging
Single-binary deployment is already possible; add a `Dockerfile` and a systemd
unit file so the daemon can run as a managed service.
