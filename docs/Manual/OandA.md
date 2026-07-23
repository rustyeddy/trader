# OANDA Integration

Trader uses OANDA v20 as its live broker and as one source of historical
bid/ask candles. The wire client lives in `brokers/oanda`; application code
normally accesses it through `service.Service`.

## Safety status

Only the OANDA **practice** environment is enabled. `oanda.BaseURL("live")`
returns `Not Live Trading Allowed`; changing a CLI flag to `--env live` does
not bypass that guard.

Order placement, trade closure, and stop changes are external side effects.
Use a practice account and explicit confirmation/write-enable controls.

## Credentials

Create a personal access token from the OANDA account portal and keep it out
of source control.

Common credential sources are:

1. A command-specific `--token` or `--account-id`
2. Merged trader global configuration
3. `OANDA_TOKEN` and `OANDA_ACCOUNT_ID`
4. `~/.config/oanda/pat.txt` as the token fallback

See [Configuration.md](Configuration.md) for command-specific precedence.

```bash
export OANDA_TOKEN='practice-token'
export OANDA_ACCOUNT_ID='101-...'
```

If a token exposes exactly one account, `service.ResolveAccount` can discover
it. Multiple accounts require an explicit account ID.

## Recommended CLI workflows

### Inspect the account

```bash
trader account list
trader account summary
trader order prices
trader account orders
trader order transactions --since 0 --limit 25
```

Use `trader <command> --help` for current flags.

### Download historical candles

```bash
trader data oanda \
  --instrument EUR_USD \
  --timeframe H1 \
  --from 2024-01-01 \
  --to 2024-12-31
```

This path downloads OANDA bid/ask candles, preserves raw source CSV, converts
values at the boundary, and writes canonical candles under the configured
store root. `--raw-dir` defaults to `/srv/trading/data/raw`.

For incremental maintenance:

```bash
trader data update --dry-run
trader data update
```

Validate stored results separately:

```bash
trader data validate-candles --source oanda
```

### Manage orders

```bash
# Preview is the default behavior.
trader order new \
  --instrument EUR_USD \
  --side long \
  --risk-pct 1 \
  --stop-pips 20

trader account orders
trader order update-stop --help
trader order close --help
```

Do not script live side effects from examples without retaining the command's
confirmation and environment checks.

### Journal transactions

```bash
trader live journal \
  --journal json \
  --trades-file live-trades.jsonl \
  --equity-file live-equity.jsonl
```

The standalone command streams until cancellation. `trader serve` runs the
same journal flow with reconnect backoff.

## Go client

The current package path is:

```go
import "github.com/rustyeddy/trader/brokers/oanda"
```

There is no `oanda.NewClient` constructor. Construct a client with the
validated base URL, or let `service.New` do it:

```go
baseURL, err := oanda.BaseURL("practice")
if err != nil {
    return err
}

client := &oanda.Client{
    BaseURL: baseURL,
    Token:   token,
    HTTP:    &http.Client{Timeout: 30 * time.Second},
}
```

For application use:

```go
svc, err := service.New(service.Config{
    Env:       "practice",
    Token:     token,
    AccountID: accountID,
    Log:       logger,
})
```

`Client.HTTP` is optional and falls back to `http.DefaultClient`, but callers
should inject a client with an appropriate timeout for non-streaming calls and
for tests.

## Implemented client surface

| Method                 | Purpose                                  |
|------------------------|------------------------------------------|
| `GetAccounts`          | List accounts visible to the token       |
| `GetAccountSummary`    | Balance, NAV, margin, and unrealized P/L |
| `GetPricing`           | Current bid/ask snapshots                |
| `StreamPricingToCSV`   | Pricing stream written as CSV            |
| `GetOpenTrades`        | List open OANDA trades                   |
| `SubmitMarketOrder`    | Submit a market order with optional stop |
| `CloseTrade`           | Full or partial trade close              |
| `UpdateTradeStop`      | Replace/cancel stop-loss or take-profit  |
| `GetTransactions`      | Poll transactions after an ID            |
| `StreamTransactions`   | Transaction and heartbeat stream         |
| `FetchCandles`         | Paginated bid/ask candle download        |
| `DownloadCandlesToCSV` | Single-request M/B/A candle export       |

The service layer wraps account, order, candle, transaction, pricing, live
runner, bot, and journal use cases. Transport handlers should call service
methods instead of using the client directly.

## Candle APIs

### Paginated bid/ask fetch

`FetchCandles` is the normal ingestion primitive:

```go
candles, err := client.FetchCandles(ctx, oanda.FetchCandlesOptions{
    Instrument:  "EUR_USD",
    Granularity: "H1",
    From:        from,
    To:          to,
    ChunkSize:   5000,
})
```

It requests `price=BA`, automatically paginates at up to 5000 candles per
request, and returns bid and ask OHLC values. The implementation treats the
end bound as exclusive.

Each returned `oanda.Candle` includes `Complete`. Consumers must decide
whether an incomplete/forming candle is valid for their use case; the low-level
client does not silently discard it.

### Direct CSV export

`DownloadCandlesToCSV` supports `M`, `B`, or `A` price components and writes
OANDA wire-format decimal values:

```go
n, err := client.DownloadCandlesToCSV(ctx, oanda.CandlesOptions{
    Instrument:  "EUR_USD",
    Granularity: "H1",
    Price:       "M",
    From:        from,
    To:          to,
}, writer)
```

`BA` is rejected by this CSV helper. Use `FetchCandles` for bid-and-ask data.
This wire CSV is not the same format as Trader's scaled canonical candle CSV.

## Instrument and granularity formats

OANDA requests use underscore-separated instruments such as `EUR_USD`.
Trader store keys use normalized symbols such as `EURUSD`. Normalize before
registry lookup or path construction, but preserve OANDA format on outbound
requests.

Granularity strings are OANDA values such as `M1`, `H1`, `H4`, and `D`.
Availability and history depend on the OANDA account and API.

## Error and stream handling

The client returns contextual Go errors containing the operation and HTTP
status/body excerpt. There are no stable typed errors for individual HTTP
statuses, so callers should add context rather than build control flow from
matching error strings.

All calls accept `context.Context`. Cancel contexts on shutdown. Transaction
and pricing streams use OANDA's streaming host and may emit heartbeats.
`trader serve` owns transaction-stream reconnection; direct client users own
their own retry policy.

Never log authorization headers, tokens, or full error bodies that may contain
sensitive account data.

## Known limitations

- Live-environment URLs are deliberately disabled.
- Internal live DTOs still contain floating-point broker values; fixed-point
  boundary migration is tracked in
  [issue #148](https://github.com/rustyeddy/trader/issues/148).
- Automatic margin-call liquidation in the simulation/backtest domain is not
  implemented; see [issue #147](https://github.com/rustyeddy/trader/issues/147).
- Broker API limits and instrument availability remain controlled by OANDA.

OANDA API reference:
[REST v20 introduction](https://developer.oanda.com/rest-live-v20/introduction/).
