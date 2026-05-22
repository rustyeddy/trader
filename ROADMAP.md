# Trader Roadmap

Roughly ordered by priority within each section.

---

## Deployment

- **GitHub Actions CI** — build `linux/amd64` and `linux/arm64` binaries on every tag; attach to GitHub Release with `checksums.txt`. Edge builds on `main` for demo deployments.
- **Ansible roles** — `trader-common` (install binary, user, dirs, NFS mount), `trader-live` (serve + live bot systemd units), `trader-data` (nightly sync cron), `trader-backtest` (on-demand). Separate `prod` and `demo` inventories with Ansible Vault for secrets.
- **prod vs demo pipeline** — tagged releases only to real-money nodes; edge builds to practice nodes. Separate vault passwords per environment.
- **Systemd units** — service templates per role; auto-restart, journal to journald.
- **Raspberry Pi support** — cross-compile targets in Makefile; `make deploy-pi PI_HOST=pi1.local`.

---

## Live Trading

- **Reconnect with existing trades** — on restart `tickCounts` resets so pre-existing positions appear as age=1. Fix: query `openTime` from OANDA and synthesise initial tick counts, or persist state to a JSON file.
- **Netting-aware mode** — add `netting: true` flag; runner closes opposite-side positions before opening a new one.
- **FOK retry** — configurable retry with jitter when a Fill-or-Kill order is cancelled.
- **Position restore on crash** — startup reconciliation to adopt orphaned positions.

---

## Strategy Engine

- **External / plugin strategies** — subprocess JSON stdio (preferred: any language, crash-safe), Starlark `.star` files (deterministic, good for backtesting), or Yaegi (interpreted Go). Go `plugin` package rejected.
- **Multi-instrument runner** — single loop managing several instruments concurrently with independent price feeds and position tracking.
- **Parameter optimisation** — grid/random search over strategy params; ranked result table.

---

## Backtesting

- **Walk-forward testing** — in-sample optimisation + out-of-sample validation; combined equity curve.
- **Portfolio backtest** — multiple instruments with shared capital and correlation-aware sizing.
- **Slippage / commission model** — configurable fixed-pip or spread-random slippage for more realistic P/L.

---

## UI

- **Update stop / take on open position** — side panel form is wired; verify end-to-end and add confirmation step.
- **Live bot controls** — start / stop / configure runner from UI (`POST /api/v1/bot/start`, `DELETE /api/v1/bot`).
- **Backtest equity curve chart** — wire `/charts` page to trade details from a selected backtest run.
- **Playwright UI tests** — third blackbox layer after REST and MCP.

---

## Infrastructure

- **Configurable logging** — structured log levels per subsystem; runtime level changes via API.
- **Docker image** — multi-stage build to distroless image for container-based deployments.
