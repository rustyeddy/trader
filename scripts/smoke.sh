#!/usr/bin/env bash
# smoke.sh — end-to-end smoke test for the trader binary.
# Runs in ~30s; requires no OANDA credentials.
# Exit code 0 = all checks passed.
set -euo pipefail

REPO="$(cd "$(dirname "$0")/.." && pwd)"
BIN="$REPO/bin/trader"
PORT=19998
SERVER_PID=""

# ── helpers ──────────────────────────────────────────────────────────────────

pass() { echo "  PASS  $*"; }
fail() { echo "  FAIL  $*" >&2; exit 1; }

cleanup() {
    if [[ -n "$SERVER_PID" ]]; then
        kill "$SERVER_PID" 2>/dev/null || true
        wait "$SERVER_PID" 2>/dev/null || true
    fi
}
trap cleanup EXIT

require_cmd() {
    command -v "$1" >/dev/null 2>&1 || { echo "Required: $1"; exit 1; }
}

# ── preflight ────────────────────────────────────────────────────────────────

require_cmd curl
require_cmd jq

echo ""
echo "=== trader smoke test ==="
echo ""

# ── 1. build ─────────────────────────────────────────────────────────────────

echo "1. Build"
cd "$REPO"
make build -s
[[ -x "$BIN" ]] || fail "bin/trader not found after build"
pass "bin/trader built OK"

# ── 2. version subcommand ────────────────────────────────────────────────────

echo ""
echo "2. Version subcommand"
out=$("$BIN" version 2>&1)
[[ -n "$out" ]] || fail "version produced no output"
pass "$out"

# ── 3. global config loading (PersistentPreRunE must not crash) ──────────────

echo ""
echo "3. Global config loading"
TMPDIR_CFG=$(mktemp -d)
echo 'log: {level: warn}' > "$TMPDIR_CFG/smoke.yml"
HOME_BAK="$HOME"
export HOME="$TMPDIR_CFG"
mkdir -p "$TMPDIR_CFG/.config/trader"
echo 'log: {level: warn, format: text}' > "$TMPDIR_CFG/.config/trader/smoke.yml"
"$BIN" version >/dev/null 2>&1 || fail "crashed with global config present"
export HOME="$HOME_BAK"
rm -rf "$TMPDIR_CFG"
pass "global config search path does not crash"

# ── 4. backtest (core engine) ────────────────────────────────────────────────

echo ""
echo "4. Backtest — EURUSD H1 2024 (ema-cross strategy)"
out=$("$BIN" backtest run "$REPO/testdata/configs/eurusd-h1-2024-ema-cross.yml" \
    --log-level error \
    2>&1)
echo "$out" | grep -qi "panic\|fatal" && fail "backtest panicked: $out"
echo "$out" | grep -qi "Trades" || fail "backtest produced no trade summary: $out"
pass "backtest completed and produced trade summary"

# ── 5. serve + replay API ────────────────────────────────────────────────────

echo ""
echo "5. Serve + replay API"

"$BIN" serve --addr ":$PORT" --log-level error &
SERVER_PID=$!

# Wait up to 5s for the server to accept connections.
for i in $(seq 1 10); do
    curl -sf "http://localhost:$PORT/api/v1/backtests" -o /dev/null 2>/dev/null && break
    sleep 0.5
    [[ $i -eq 10 ]] && fail "server did not start within 5s"
done

# 5a. H1 replay
response=$(curl -sf -X POST "http://localhost:$PORT/api/v1/replay" \
    -H "Content-Type: application/json" \
    -d '{
        "instrument": "EURUSD",
        "timeframe": "H1",
        "from": "2024-01-02",
        "to": "2024-01-12",
        "warmup_bars": 20,
        "strategy": {"kind": "donchian-v6"}
    }')
echo "$response" | jq -e '.bars | length > 0' >/dev/null \
    || fail "H1 replay returned no bars: $response"
bar_count=$(echo "$response" | jq '.bars | length')
pass "H1 replay: $bar_count bars"

# 5b. D timeframe (regression: was 422 before the fix)
response=$(curl -sf -X POST "http://localhost:$PORT/api/v1/replay" \
    -H "Content-Type: application/json" \
    -d '{
        "instrument": "GBPUSD",
        "timeframe": "D",
        "from": "2024-01-01",
        "to": "2024-04-01",
        "warmup_bars": 50,
        "strategy": {"kind": "donchian-v6"}
    }')
echo "$response" | jq -e '.bars | length > 0' >/dev/null \
    || fail "D replay returned no bars: $response"
bar_count=$(echo "$response" | jq '.bars | length')
pass "D timeframe replay: $bar_count bars (regression check)"

# 5c. Bad timeframe must still return an error (not 200)
response=$(curl -s -X POST "http://localhost:$PORT/api/v1/replay" \
    -H "Content-Type: application/json" \
    -d '{"instrument":"EURUSD","timeframe":"W","from":"2024-01-01","to":"2024-03-01","strategy":{"kind":"donchian-v6"}}')
echo "$response" | jq -e '.error' >/dev/null \
    || fail "bad timeframe W should have returned error: $response"
pass "unsupported timeframe returns error (not silent 200)"

# ── 6. live command fails gracefully without OANDA token ─────────────────────

echo ""
echo "6. Live portfolio fails gracefully (no OANDA token)"
unset OANDA_TOKEN OANDA_ACCOUNT_ID 2>/dev/null || true
out=$("$BIN" live portfolio \
    --config /dev/null \
    --log-level error \
    2>&1 || true)
# Should error, not panic
echo "$out" | grep -qi "panic\|nil pointer\|segfault" \
    && fail "live portfolio panicked: $out"
pass "live portfolio exits cleanly without credentials"

# ── done ─────────────────────────────────────────────────────────────────────

echo ""
echo "=== All smoke checks passed ==="
echo ""
