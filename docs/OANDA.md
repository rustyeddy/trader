# OANDA Integration Guide

This document provides a comprehensive guide for using the OANDA API integration in the trader library.

## Overview

The OANDA integration allows you to download historic candlestick data from your OANDA account and use it with the trader library for backtesting, analysis, and research.

## Getting Started

### 1. Get Your OANDA Access Token

**Practice Account:**
- Visit: https://www.oanda.com/demo-account/tpa/personal_token
- Generate a personal access token

**Live Account:**
- Visit: https://www.oanda.com/account/tpa/personal_token
- Generate a personal access token

⚠️ **Security Note**: Never commit your access token to version control.

### 2. Basic Usage

```go
package main

import (
    "context"
    "fmt"
    "log"
    
    "github.com/rustyeddy/trader/oanda"
)

func main() {
    // Create client (true = practice, false = live)
    client := oanda.NewClient("your-token-here", true)
    
    // Fetch last 100 5-minute candles
    candles, err := client.GetCandles(context.Background(), oanda.CandlesRequest{
        Instrument:  "EUR_USD",
        Price:       oanda.MidPrice,
        Granularity: oanda.M5,
        Count:       100,
    })
    
    if err != nil {
        log.Fatal(err)
    }
    
    fmt.Printf("Downloaded %d candles\n", len(candles))
}
```

## Configuration Options

### Instruments

The OANDA API supports many instruments. Common examples:
- Currency pairs: `EUR_USD`, `USD_JPY`, `GBP_USD`, `AUD_USD`
- Metals: `XAU_USD` (Gold), `XAG_USD` (Silver)
- Indices: `SPX500_USD`, `US30_USD`

### Price Components

```go
oanda.MidPrice  // Average of bid and ask (default)
oanda.BidPrice  // Bid prices only
oanda.AskPrice  // Ask prices only
oanda.BidAsk    // Both bid and ask (not yet implemented)
```

### Granularities

**Seconds**: `S5`, `S10`, `S15`, `S30`
**Minutes**: `M1`, `M2`, `M4`, `M5`, `M10`, `M15`, `M30`
**Hours**: `H1`, `H2`, `H3`, `H4`, `H6`, `H8`, `H12`
**Days**: `D`
**Weeks**: `W`
**Months**: `M`

## Advanced Usage

### Fetching by Time Range

```go
from := time.Now().AddDate(0, 0, -30) // 30 days ago
to := time.Now()

candles, err := client.GetCandles(ctx, oanda.CandlesRequest{
    Instrument:  "EUR_USD",
    Granularity: oanda.H1,
    From:        &from,
    To:          &to,
})
```

### Using with Trading Engine

```go
import (
    "github.com/rustyeddy/trader/oanda"
    "github.com/rustyeddy/trader/sim"
)

func backtest() {
    // Download historic data
    client := oanda.NewClient(token, true)
    candles, err := client.GetCandles(ctx, oanda.CandlesRequest{
        Instrument:  "EUR_USD",
        Granularity: oanda.M5,
        Count:       1000,
    })
    
    // Use with simulation engine
    engine := sim.NewEngine(account, journal)
    
    // Feed candles into engine for backtesting
    for _, candle := range candles {
        engine.UpdatePrice(broker.Price{
            Instrument: "EUR_USD",
            Bid:        candle.Close - 0.0001, // Approximate bid
            Ask:        candle.Close + 0.0001, // Approximate ask
            Time:       candle.Time,
        })
    }
}
```

### Error Handling

```go
candles, err := client.GetCandles(ctx, req)
if err != nil {
    // Check for specific error types
    switch {
    case strings.Contains(err.Error(), "401"):
        log.Fatal("Invalid access token")
    case strings.Contains(err.Error(), "instrument is required"):
        log.Fatal("Missing instrument parameter")
    case strings.Contains(err.Error(), "cannot exceed 5000"):
        log.Fatal("Too many candles requested")
    default:
        log.Fatalf("API error: %v", err)
    }
}
```

## Best Practices

1. **Rate Limiting**: OANDA has rate limits. Implement exponential backoff for production use.

2. **Data Storage**: For large datasets, cache downloaded candles to avoid repeated API calls:
   ```go
   // Save to file
   data, _ := json.Marshal(candles)
   os.WriteFile("candles_cache.json", data, 0644)
   ```

3. **Environment Variables**: Always use environment variables for tokens:
   ```bash
   export OANDA_TOKEN="your-token"
   export OANDA_PRACTICE="true"
   ```

4. **Context Timeouts**: Use timeouts for API calls:
   ```go
   ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
   defer cancel()
   candles, err := client.GetCandles(ctx, req)
   ```

5. **Incomplete Candles**: The library automatically filters incomplete candles. Current/forming candles are not included in results.

## Limitations

- **Max Count**: Maximum 5000 candles per request
- **Date Range**: Cannot use `Count` with both `From` and `To`
- **Historic Data**: OANDA limits how far back you can fetch data (varies by granularity)
- **Rate Limits**: Subject to OANDA's API rate limits

## Troubleshooting

### "Invalid access token"
- Verify your token is correct
- Check if token has expired
- Ensure you're using the right environment (practice vs live)

### "Unknown instrument"
- Verify instrument name format (e.g., `EUR_USD` not `EURUSD`)
- Check if instrument is available in your account type

### "Request timeout"
- Reduce the number of candles requested
- Check your internet connection
- OANDA API may be experiencing issues

## Related Documentation

- [OANDA REST API v20 Documentation](https://developer.oanda.com/rest-live-v20/introduction/)
- [Examples Directory](../examples/oanda/)
- [Market Package](../market/)

## Support

For issues with the trader library, please open an issue on GitHub.
For OANDA API issues, contact OANDA support or check their developer forum.
