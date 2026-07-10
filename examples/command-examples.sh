#!/usr/bin/env bash
set -euo pipefail

# Copy-paste oriented command examples. Most lines are commented so this file
# can be read as a script without accidentally hitting OANDA or placing orders.

TRADER="${TRADER:-./bin/trader}"

if [[ "${RUN_EXAMPLES:-0}" != "1" ]]; then
  echo "Set RUN_EXAMPLES=1 to run the offline examples in this file."
  echo "Read examples/README.md for the full command catalog, including local-data and OANDA examples."
  exit 0
fi

# The following commands are offline, but some still write local output under
# docs/ or /tmp and may require the checked-in test candle fixtures. Review
# before running with RUN_EXAMPLES=1.

"$TRADER" version
"$TRADER" docs --file docs/trader-cli.md

"$TRADER" backtest run \
  --config testdata/configs/eurusd-h1-2024-ema-cross.yml \
  --out /tmp/trader-backtest-reports

"$TRADER" backtest list \
  --dir /tmp/trader-backtest-reports \
  --instrument eurusd

"$TRADER" data pip-value \
  --units 100000 \
  --rates USDJPY=157.50,USDCHF=0.89,USDCAD=1.37

"$TRADER" data position \
  --instrument EURUSD \
  --price 1.0850 \
  --units 10000 \
  --pips 25

"$TRADER" --db /tmp/trader-replay \
  replay pricing \
  --ticks examples/replay-pricing.csv \
  --starting-balance 25000 \
  --account SIM-DEMO

# Local candle store examples:
# "$TRADER" --data-dir /srv/trading/data/candles \
#   data candles \
#   --instrument EURUSD \
#   --timeframe H1 \
#   --from 2024-01-01 \
#   --to 2024-01-05
#
# "$TRADER" --data-dir /srv/trading/data/candles \
#   data validate-candles \
#   --instruments EURUSD \
#   --timeframe H1 \
#   --from 2024-01 \
#   --to 2024-12 \
#   --quiet

# Live OANDA read examples:
# export OANDA_TOKEN='practice-token'
# export OANDA_ACCOUNT_ID='101-001-00000000-001'
# "$TRADER" account --env practice summary
# "$TRADER" order prices \
#   --env practice \
#   --account-id "$OANDA_ACCOUNT_ID" \
#   --instruments EUR_USD,USD_JPY \
#   --units 10000

# Practice order examples. Review before uncommenting:
# "$TRADER" order new \
#   --env practice \
#   --account-id "$OANDA_ACCOUNT_ID" \
#   --instrument EUR_USD \
#   --side long \
#   --stop-pips 20 \
#   --risk-pct 0.10
