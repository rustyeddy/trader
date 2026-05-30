#!/usr/bin/env bash
# Backfill OANDA candles 2005-01-01 → 2019-12-31 for all major pairs.
set -euo pipefail

BINARY="$(dirname "$0")/../bin/trader"
FROM="2005-01-01"
TO="2019-12-31"
ENV="practice"
LOG_DIR="$(dirname "$0")/../logs/backfill"
mkdir -p "$LOG_DIR"

INSTRUMENTS=(EUR_USD GBP_USD USD_CHF USD_JPY)
TIMEFRAMES=(D H1 M1)

for TF in "${TIMEFRAMES[@]}"; do
    for INSTR in "${INSTRUMENTS[@]}"; do
        LOG="$LOG_DIR/${INSTR}_${TF}.log"
        echo "[$(date -u +%H:%M:%S)] Starting $INSTR $TF ..."
        "$BINARY" data oanda \
            --instrument "$INSTR" \
            --timeframe "$TF" \
            --from "$FROM" \
            --to "$TO" \
            --env "$ENV" \
            2>&1 | tee "$LOG"
        echo "[$(date -u +%H:%M:%S)] Done $INSTR $TF"
    done
done

echo ""
echo "All backfills complete."
