#!/usr/bin/env bash
# Convert extracted Stooq daily files into Trader managed raw and canonical data.
# The source tree is never modified; each .txt file is packaged temporarily
# because `trader data convert` accepts the native ZIP source shape.
set -euo pipefail

repo_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
source_root=${1:-/srv/trading/data/tmp/stooq/data/daily}
raw_root=${2:-/srv/trading/data/raw/stooq}
store_root=${3:-/srv/trading/data/canonical}
from=${STOOQ_FROM:-1900-01-01}
to=${STOOQ_TO:-2100-01-01}

if [[ ! -d "$source_root" ]]; then
    echo "source directory does not exist: $source_root" >&2
    exit 1
fi
command -v zip >/dev/null || { echo "zip is required" >&2; exit 1; }

trader_bin=$(mktemp)
tmp_archive=
cleanup() {
    rm -f "$trader_bin"
    if [[ -n "$tmp_archive" ]]; then
        rm -f "$tmp_archive"
    fi
}
trap cleanup EXIT

echo "building trader CLI..." >&2
(cd "$repo_dir" && go build -o "$trader_bin" ./cmd/trader)

converted=0
skipped=0
failed=0
while IFS= read -r -d '' source; do
    market_dir=$(basename "$(dirname "$(dirname "$source")")")
    symbol=$(basename "$source" .us.txt | tr '[:lower:]' '[:upper:]')
    exchange=
    kind=
    case "$market_dir" in
        "nyse etfs")
            # Stooq's NYSE ETF directory contains NYSE Arca listings such as SPY.
            exchange=ARCA; kind=etf ;;
        "nasdaq etfs")
            exchange=NASDAQ; kind=etf ;;
        "nyse stocks")
            exchange=NYSE; kind=equity ;;
        "nasdaq stocks")
            exchange=NASDAQ; kind=equity ;;
        *)
            echo "skipping $source (unrecognized market directory: $market_dir)" >&2
            skipped=$((skipped + 1))
            continue ;;
    esac

    tmp_archive=$(mktemp --suffix=.zip)
    rm -f "$tmp_archive"
    (cd "$(dirname "$source")" && zip -q -j "$tmp_archive" "$(basename "$source")")
    if "$trader_bin" data convert "$symbol" D1 \
        --provider stooq \
        --archive "$tmp_archive" \
        --raw-root "$raw_root" \
        --store-root "$store_root" \
        --exchange "$exchange" \
        --kind "$kind" \
        --from "$from" \
        --to "$to"; then
        converted=$((converted + 1))
    else
        echo "failed: $source" >&2
        failed=$((failed + 1))
    fi
    rm -f "$tmp_archive"
    tmp_archive=
done < <(find "$source_root" -type f -iname '*.us.txt' -print0 | sort -z)

echo "converted=$converted skipped=$skipped failed=$failed" >&2
if (( failed > 0 )); then
    exit 1
fi
