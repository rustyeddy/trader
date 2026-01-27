# OANDA Historic Candles Download Example

This example demonstrates how to download historic candlestick data from your OANDA account using the trader library.

## Prerequisites

1. An OANDA account (practice or live)
2. An OANDA API access token

## Getting Your Access Token

### For Practice Account:
1. Go to https://www.oanda.com/demo-account/tpa/personal_token
2. Log in with your practice account
3. Generate a personal access token

### For Live Account:
1. Go to https://www.oanda.com/account/tpa/personal_token
2. Log in with your live account
3. Generate a personal access token

**Important**: Keep your access token secure and never commit it to version control.

## Usage

1. Set your OANDA access token as an environment variable:
   ```bash
   export OANDA_TOKEN="your-access-token-here"
   ```

2. Run the example:
   ```bash
   go run examples/oanda/main.go
   ```

## Features

The example demonstrates:

- Fetching candles with different time granularities (5-minute, 1-hour, daily)
- Using different price components (midpoint, bid, ask)
- Fetching by count (last N candles)
- Fetching by time range (from date to date)
- Working with multiple instruments (EUR_USD, USD_JPY)

## Available Granularities

- **Seconds**: S5, S10, S15, S30
- **Minutes**: M1, M2, M4, M5, M10, M15, M30
- **Hours**: H1, H2, H3, H4, H6, H8, H12
- **Days**: D
- **Weeks**: W
- **Months**: M

## Available Price Components

- **M (Midpoint)**: Average of bid and ask prices
- **B (Bid)**: Bid prices only
- **A (Ask)**: Ask prices only
- **BA (Bid & Ask)**: Not yet implemented

## Example Output

```
=== Example 1: Last 100 5-minute candles (midpoint) ===
Fetched 100 candles
First candle: Time=2024-01-20 10:00:00, O=1.0850, H=1.0860, L=1.0840, C=1.0855, V=100
Last candle:  Time=2024-01-20 18:15:00, O=1.0865, H=1.0875, L=1.0860, C=1.0870, V=150

=== Example 2: 1-hour candles for specific date range (bid prices) ===
Fetched 168 candles from 2024-01-13 to 2024-01-20
...
```

## Using in Your Own Code

```go
package main

import (
    "context"
    "fmt"
    "github.com/rustyeddy/trader/oanda"
)

func main() {
    // Create client
    client := oanda.NewClient("your-token", true) // true for practice, false for live

    // Fetch candles
    candles, err := client.GetCandles(context.Background(), oanda.CandlesRequest{
        Instrument:  "EUR_USD",
        Price:       oanda.MidPrice,
        Granularity: oanda.M5,
        Count:       100,
    })
    if err != nil {
        panic(err)
    }

    for _, candle := range candles {
        fmt.Printf("Time: %s, Open: %.4f, Close: %.4f\n",
            candle.Time, candle.Open, candle.Close)
    }
}
```

## Notes

- The maximum number of candles that can be fetched in a single request is 5000
- Incomplete candles (currently forming) are automatically filtered out
- All times are returned in UTC
- The practice environment uses the base URL: `https://api-fxpractice.oanda.com`
- The live environment uses the base URL: `https://api-fxtrade.oanda.com`
