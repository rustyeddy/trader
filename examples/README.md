# Trader CLI Examples

This directory collects practical command examples for `trader`. The generated
command reference lives in [docs/trader-cli.md](../docs/trader-cli.md); these
examples show common workflows and interesting flag combinations.

Most examples use `./bin/trader` from `make build`. Replace it with `trader`
if the binary is already on your `PATH`.

Commands marked "live OANDA" require an OANDA practice or live token. Commands
that place, close, or update orders are examples only; verify account, side,
units, stops, and environment before running them.

## Build and Inspect

```bash
make build
./bin/trader version
./bin/trader --help
./bin/trader backtest run --help
```

## Generate CLI Documentation

```bash
# Refresh the single-file Markdown reference.
./bin/trader docs --file docs/trader-cli.md

# Generate one Markdown file per command.
./bin/trader docs --single=false --out docs/cli

# Generate man pages.
./bin/trader docs --single=false --format man --out docs/man

# Generate reStructuredText files.
./bin/trader docs --single=false --format rst --out docs/rst
```

## Backtests

```bash
# Run one checked-in config and write reports under /tmp.
./bin/trader backtest run \
  --config testdata/configs/eurusd-h1-2024-ema-cross.yml \
  --out /tmp/trader-backtest-reports

# Equivalent positional config path.
./bin/trader backtest run \
  testdata/configs/usdjpy-h1-donchian.yml \
  --out /tmp/trader-backtest-reports

# Run a focused glob of configs.
./bin/trader backtest run \
  --config 'testdata/configs/*donchian*.yml' \
  --out /tmp/trader-backtest-reports

# List generated reports and filter by instrument or strategy.
./bin/trader backtest list --dir /tmp/trader-backtest-reports
./bin/trader backtest list --dir /tmp/trader-backtest-reports --instrument eurusd
./bin/trader backtest list --dir /tmp/trader-backtest-reports --strategy donchian

# Inspect a saved result. Use an exact report name from `backtest list`,
# without the .json suffix.
./bin/trader backtest get '<report-name>' --dir /tmp/trader-backtest-reports
./bin/trader backtest org '<report-name>' --dir /tmp/trader-backtest-reports
./bin/trader backtest candles '<report-name>' --dir /tmp/trader-backtest-reports

# Compare current results with committed regression baselines.
./bin/trader backtest regress \
  --config testdata/backtests/configs \
  --baselines testdata/backtests/reports

# Update baselines intentionally after accepting changed results.
./bin/trader backtest regress \
  --config testdata/backtests/configs \
  --baselines testdata/backtests/reports \
  --update
```

## Local Candle Data

These commands read or validate the local candle store. Use `--data-dir` to
point at a scratch or alternate store instead of the default operational path.

```bash
# Print local H1 candles as CSV.
./bin/trader --data-dir /srv/trading/data/candles \
  data candles \
  --instrument EURUSD \
  --timeframe H1 \
  --from 2024-01-01 \
  --to 2024-01-05

# Print statistics with a USD value column for a standard lot.
./bin/trader --data-dir /srv/trading/data/candles \
  data stats \
  --instrument USDJPY \
  --timeframe D1 \
  --from 2023-01-01 \
  --to 2023-12-31 \
  --units 100000

# Validate one instrument/timeframe quietly and save a JSON report.
./bin/trader --data-dir /srv/trading/data/candles \
  data validate-candles \
  --instruments EURUSD \
  --timeframe H1 \
  --from 2024-01 \
  --to 2024-12 \
  --quiet \
  --report /tmp/eurusd-h1-validation.json

# Build derived candles from already-downloaded raw Dukascopy tick files.
./bin/trader --data-dir /srv/trading/data/candles \
  data build-candles \
  --instruments EURUSD,USDJPY \
  --from 2024-01 \
  --to 2024-03

# Download missing Dukascopy ticks and build candles.
./bin/trader --data-dir /srv/trading/data/candles \
  data sync \
  --instruments EURUSD,USDJPY \
  --from 2024-01 \
  --to 2024-01
```

## OANDA Data Downloads

These examples hit OANDA. Use practice credentials unless you explicitly need
live data.

```bash
export OANDA_TOKEN='practice-token'

# Download a small H1 range into an alternate candle/raw store.
./bin/trader --data-dir /tmp/trader-data/candles \
  data oanda \
  --env practice \
  --instrument EUR_USD \
  --timeframe H1 \
  --from 2024-01-01 \
  --to 2024-01-07 \
  --raw-dir /tmp/trader-data/raw

# Preview catch-up work without downloading.
./bin/trader --data-dir /tmp/trader-data/candles \
  data update \
  --env practice \
  --instruments EUR_USD,GBP_USD \
  --timeframes H1,D \
  --from 2024-01-01 \
  --raw-dir /tmp/trader-data/raw \
  --dry-run

# Repair missing local candle months by re-downloading from OANDA.
./bin/trader --data-dir /tmp/trader-data/candles \
  data validate-candles \
  --env practice \
  --instruments EURUSD \
  --timeframe H1 \
  --from 2024-01 \
  --to 2024-03 \
  --repair \
  --raw-dir /tmp/trader-data/raw
```

## Position and Pip Calculators

```bash
# Fully offline: use explicit rates for USD-base pairs.
./bin/trader data pip-value \
  --units 100000 \
  --rates USDJPY=157.50,USDCHF=0.89,USDCAD=1.37

# Offline position sizing with explicit price.
./bin/trader data position \
  --instrument EURUSD \
  --price 1.0850 \
  --units 10000 \
  --pips 25

# Convert a USD notional target to units.
./bin/trader data position \
  --instrument GBPUSD \
  --price 1.2750 \
  --notional 25000 \
  --pips 10

# Live OANDA price lookup.
./bin/trader data position \
  --env practice \
  --instrument USDJPY \
  --units 10000 \
  --pips 50
```

## Replay

```bash
# Replay the included pricing-only CSV.
./bin/trader --db /tmp/trader-replay \
  replay pricing \
  --ticks examples/replay-pricing.csv \
  --starting-balance 25000 \
  --account SIM-DEMO

# Replay a time slice and force-close any open positions at the end.
./bin/trader --db /tmp/trader-replay-slice \
  replay pricing \
  --ticks examples/replay-pricing.csv \
  --from 2024-01-02T09:01:00Z \
  --to 2024-01-02T09:04:00Z \
  --close-end
```

`replay events` currently parses event rows, but `OPEN` and `CLOSE` execution
are not implemented yet. Prefer `replay pricing` until those semantics are
completed.

## Review Sweeps

```bash
# One historical date, table output.
./bin/trader review \
  --instruments EURUSD,GBPUSD,USDJPY \
  --asof 2026-05-15 \
  --output table

# Date range sweep as CSV.
./bin/trader review \
  --instruments EURUSD,GBPUSD \
  --from 2026-05-01 \
  --to 2026-05-10 \
  --output csv

# Wider step size and only the tradeable bucket.
./bin/trader review \
  --instruments EURUSD,USDJPY \
  --from 2026-01-01 \
  --to 2026-06-30 \
  --interval 168h \
  --tradeable \
  --output json
```

See [review-sweeps.sh](review-sweeps.sh) for a compact script version.

## Server, Health, and Bots

```bash
# Start the daemon with REST/UI on :9999.
./bin/trader serve \
  --addr :9999 \
  --reports-dir /tmp/trader-backtest-reports \
  --journal-trades /tmp/live-trades.jsonl \
  --journal-equity /tmp/live-equity.jsonl

# Check a running server.
./bin/trader health --server http://localhost:9999 check
./bin/trader health --server http://localhost:9999 version

# Server-managed bot lifecycle.
./bin/trader bot --server http://localhost:9999 list
./bin/trader bot --server http://localhost:9999 start \
  --instrument EUR_USD \
  --strategy pulse \
  --risk-pct 0.25 \
  --max-units 1000 \
  --tick-interval 5m
./bin/trader bot --server http://localhost:9999 get EUR_USD-pulse
./bin/trader bot --server http://localhost:9999 stop EUR_USD-pulse
./bin/trader bot --server http://localhost:9999 report \
  --all \
  --journal /tmp/live-trades.jsonl
```

## Live OANDA Account and Orders

These commands read from or write to an OANDA account. The `order new`,
`order close`, and `order update-stop` examples can affect real positions if
you run them against a live account.

```bash
export OANDA_TOKEN='practice-token'
export OANDA_ACCOUNT_ID='101-001-00000000-001'

# Read-only account and market state.
./bin/trader account --env practice list
./bin/trader account --env practice summary
./bin/trader order prices \
  --env practice \
  --account-id "$OANDA_ACCOUNT_ID" \
  --instruments EUR_USD,USD_JPY \
  --units 10000
./bin/trader account orders \
  --env practice \
  --account-id "$OANDA_ACCOUNT_ID"
./bin/trader order transactions \
  --env practice \
  --account-id "$OANDA_ACCOUNT_ID" \
  --limit 10

# Place a small practice market order sized from risk and stop distance.
./bin/trader order new \
  --env practice \
  --account-id "$OANDA_ACCOUNT_ID" \
  --instrument EUR_USD \
  --side long \
  --stop-pips 20 \
  --risk-pct 0.10

# Move or cancel exits on an existing trade.
./bin/trader order update-stop \
  --env practice \
  --account-id "$OANDA_ACCOUNT_ID" \
  --trade-id 12345 \
  --stop 1.0750 \
  --take 1.0950

# Full close, or partial close with --units.
./bin/trader order close \
  --env practice \
  --account-id "$OANDA_ACCOUNT_ID" \
  --trade-id 12345
./bin/trader order close \
  --env practice \
  --account-id "$OANDA_ACCOUNT_ID" \
  --trade-id 12345 \
  --units 500

# Stream transactions and journal closed trades.
./bin/trader live journal \
  --env practice \
  --account-id "$OANDA_ACCOUNT_ID" \
  --journal json \
  --trades-file /tmp/live-trades.jsonl \
  --equity-file /tmp/live-equity.jsonl \
  --backfill-from 0
```

## MCP

```bash
# Backtest-only MCP server over stdio.
./bin/trader mcp \
  --reports-dir /tmp/trader-backtest-reports

# Enable live read-only account/trade tools.
./bin/trader mcp \
  --env practice \
  --account-id "$OANDA_ACCOUNT_ID" \
  --token "$OANDA_TOKEN" \
  --reports-dir /tmp/trader-backtest-reports

# Enable write tools only when explicitly intended.
./bin/trader mcp \
  --env practice \
  --account-id "$OANDA_ACCOUNT_ID" \
  --token "$OANDA_TOKEN" \
  --enable-write
```

## Shell Completion

```bash
# Current shell session only.
source <(./bin/trader completion bash)
source <(./bin/trader completion zsh)

# Fish.
./bin/trader completion fish | source

# Bash completion without descriptions.
./bin/trader completion bash --no-descriptions > /tmp/trader.bash
```
