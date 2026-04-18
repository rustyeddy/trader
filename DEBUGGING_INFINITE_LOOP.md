# Debugging the TestTrader Infinite Loop - Practical Guide

## Your Situation
TestTrader experiences an infinite loop when processing a year worth of real market candles on your machine.

## Solution Overview
Use synthetic candles to reproduce and debug the issue systematically.

## Part 1: Generate Synthetic Test Data (2 minutes)

### Option A: Using the CLI Tool (Recommended)

```bash
cd /workspaces/trader

# Generate 1 year of hourly EUR/USD data
go run ./cmd/gen-testdata/main.go \
  -instrument EURUSD \
  -year 2025 \
  -timeframe H1 \
  -output testdata \
  -v

# You should see:
# ✓ Successfully generated 12 months of EURUSD H1 data
#   Total: 426 KB
#   Output: testdata
```

Data is now in: `testdata/eurusd/candles/h1/2025/{01-12}/`

### Option B: Programmatically in a Test

```go
package trader

import "testing"

func TestGenerateOnce(t *testing.T) {
    t.Skip("Uncomment to generate data")
    
    cfg := DefaultSyntheticConfig("EURUSD")
    cfg.Timeframe = H1
    paths, err := cfg.GenerateSyntheticYearlyAndWrite(&Store{basedir: "testdata"}, 2025)
    if err != nil {
        t.Fatal(err)
    }
    t.Logf("Generated %d months of data", len(paths))
}
```

## Part 2: Replicate Your Issue with Synthetic Data (5 minutes)

### Create a Test File

```go
// file: my_testtrader_infinite_loop_debug_test.go
package trader

import (
    "context"
    "testing"
    "time"
)

// TestReplicateInfiniteLoop attempts to replicate the infinite loop
// using synthetic data instead of real market data.
func TestReplicateInfiniteLoop(t *testing.T) {
    // STEP 1: Generate synthetic data
    cfg := DefaultSyntheticConfig("EURUSD")
    cfg.Timeframe = H1
    
    candleSets, err := cfg.GenerateSyntheticYearlyCandles(2025)
    if err != nil {
        t.Fatalf("Failed to generate candles: %v", err)
    }
    
    // STEP 2: Set a reasonable timeout
    // If real data hangs with infinite loop, synthetic data should complete fast
    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()
    
    // STEP 3: Feed candles to your trader logic
    totalCandles := 0
    lastProgressTime := time.Now()
    
    for monthIdx, cs := range candleSets {
        iter := NewCandleSetIterator(cs, TimeRange{})
        
        for iter.Next() {
            select {
            case <-ctx.Done():
                t.Fatalf("TIMEOUT after processing %d candles (Month %d)", 
                    totalCandles, monthIdx+1)
            default:
            }
            
            totalCandles++
            c := iter.Candle()
            
            // HERE IS WHERE YOUR TRADER LOGIC GOES
            // Replace this with your actual strategy/trader logic:
            // trader.ProcessCandle(c)
            // or:
            // strategy.OnBar(c)
            // or:
            // engine.Update(c)
            
            // Progress logging
            if time.Since(lastProgressTime) > 5*time.Second {
                t.Logf("Progress: %d candles, Month %d", totalCandles, monthIdx+1)
                lastProgressTime = time.Now()
            }
        }
        
        iter.Close()
        t.Logf("✓ Completed Month %d: %d candles", monthIdx+1, len(cs.Candles))
    }
    
    t.Logf("✓✓ SUCCESS: Processed all %d candles without infinite loop!", totalCandles)
}
```

### Run the Test

```bash
# Run with timeout to detect a hang
go test -v -run TestReplicateInfiniteLoop -timeout 60s

# Expected output if successful:
# === RUN   TestReplicateInfiniteLoop
#     debug_test.go:45: Progress: 500 candles, Month 1
#     debug_test.go:45: Progress: 1000 candles, Month 2
#     ...
#     debug_test.go:62: ✓✓ SUCCESS: Processed all 6194 candles without infinite loop!
# --- PASS: TestReplicateInfiniteLoop (0.05s)

# Expected if infinite loop exists:
# === RUN   TestReplicateInfiniteLoop
#     debug_test.go:45: Progress: 500 candles, Month 1
#     [hangs...]
# [after 60 second timeout]
# --- FAIL: TestReplicateInfiniteLoop (60.01s)
```

## Part 3: Find Which Month Hangs (Binary Search)

If the test times out, use binary search to find the problem:

```go
func TestBinarySearchInfiniteLoop(t *testing.T) {
    cfg := DefaultSyntheticConfig("EURUSD")
    cfg.Timeframe = H1
    
    // Only process up to a certain month
    months := []int{1, 6, 3, 9, 12}  // Binary search order
    
    for _, month := range months {
        t.Run(fmt.Sprintf("Month%d", month), func(t *testing.T) {
            ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
            defer cancel()
            
            cs, err := cfg.GenerateSyntheticMonthlyCandles(2025, time.Month(month))
            if err != nil {
                t.Fatalf("Generate failed: %v", err)
            }
            
            iter := NewCandleSetIterator(cs, TimeRange{})
            
            candleCount := 0
            for iter.Next() {
                select {
                case <-ctx.Done():
                    t.Fatalf("TIMEOUT at month %d, candle %d", month, candleCount)
                default:
                }
                candleCount++
                c := iter.Candle()
                
                // Your trader logic here
            }
            
            t.Logf("✓ Month %d completed: %d candles", month, candleCount)
        })
    }
}
```

## Part 4: Find Which Specific Candle Hangs

```go
func TestFindSpecificProblematicCandle(t *testing.T) {
    // Once you know which month has the issue...
    cfg := DefaultSyntheticConfig("EURUSD")
    cfg.Timeframe = H1
    
    cs, _ := cfg.GenerateSyntheticMonthlyCandles(2025, time.January)  // Problem month
    
    iter := NewCandleSetIterator(cs, TimeRange{})
    
    problematicCandleIdx := 0
    startTime := time.Now()
    maxDuration := 5 * time.Second
    
    for iter.Next() {
        problematicCandleIdx++
        
        // Timeout check with specific candle
        if time.Since(startTime) > maxDuration {
            t.Logf("HUNG at candle %d", problematicCandleIdx)
            t.Logf("Candle details: %+v", iter.Candle())
            t.Fatalf("Infinite loop detected at candle index %d", problematicCandleIdx)
        }
        
        c := iter.CandleTime()
        
        t.Logf("[%d] Candle at %d: O=%.5f H=%.5f L=%.5f C=%.5f", 
            problematicCandleIdx,
            c.Timestamp,
            float64(c.Candle.Open)/1000000,
            float64(c.Candle.High)/1000000,
            float64(c.Candle.Low)/1000000,
            float64(c.Candle.Close)/1000000,
        )
        
        // Your trader logic here
    }
    
    t.Logf("✓ Processed all %d candles successfully", problematicCandleIdx)
}
```

## Part 5: Check Your Strategy/Trader Code

Once you know which candle causes the hang, examine your code:

### Common Infinite Loop Sources

```go
// ❌ PROBLEM 1: Infinite recursion
func (s *MyStrategy) OnBar(c Candle) {
    if someCondition {
        s.OnBar(c)  // INFINITE RECURSION!
    }
}

// ✓ SOLUTION: Remove recursion or add depth check

// ❌ PROBLEM 2: Event queue never empties
func ProcessEvents(eventQueue chan Event) {
    for {
        evt := <-eventQueue
        if someCondition {
            PublishEvent(evt)  // Re-queue the same event!
        }
    }
}

// ✓ SOLUTION: Prevent re-queueing the same event

// ❌ PROBLEM 3: Position management loop
func (a *Account) OpenPosition(req *Request) {
    for {
        if a.Positions[id] != nil {
            a.OpenPosition(sameRequest)  // INFINITE RECURSION!
        }
        break
    }
}

// ✓ SOLUTION: Simplify the logic, remove the loop

// ❌ PROBLEM 4: Event processing hangs on missing data
func (t *Trader) ProcessCandle(c Candle) {
    for t.PendingEvents > 0 {  // If events never decrement...
        t.processPendingEvent()
    }
}

// ✓ SOLUTION: Add event count limit or timeout
```

## Part 6: Verify the Fix

```go
func TestVerifyInfiniteLoopFixed(t *testing.T) {
    // After making your fix...
    
    cfg := DefaultSyntheticConfig("EURUSD")
    cfg.Timeframe = H1
    
    candleSets, _ := cfg.GenerateSyntheticYearlyCandles(2025)
    
    startTime := time.Now()
    totalCandles := 0
    
    for _, cs := range candleSets {
        iter := NewCandleSetIterator(cs, TimeRange{})
        
        for iter.Next() {
            totalCandles++
            c := iter.Candle()
            
            // Your FIXED trader logic here
        }
        
        iter.Close()
    }
    
    elapsed := time.Since(startTime)
    t.Logf("✓ Processed %d candles in %v", totalCandles, elapsed)
    
    // Should complete in reasonable time (< 1 second for synthetic)
    if elapsed > 10*time.Second {
        t.Errorf("Processing took too long: %v", elapsed)
    }
}
```

## Summary of Commands

```bash
# 1. Generate synthetic data
go run ./cmd/gen-testdata/main.go -instrument EURUSD -year 2025 -timeframe H1 -v

# 2. Run your test
go test -v -run TestReplicateInfiniteLoop -timeout 60s

# 3. If timeout occurs, binary search by month (see Part 3)
go test -v -run TestBinarySearchInfiniteLoop -timeout 60s

# 4. Find problematic candle (see Part 4)
go test -v -run TestFindSpecificProblematicCandle

# 5. Check trader code (Part 5) and fix

# 6. Verify fix works
go test -v -run TestVerifyInfiniteLoopFixed
```

## Key Insights

✅ **Synthetic data is instantly reproducible** - No waiting for download
✅ **Same seed = same behavior** - Helps with CI/CD
✅ **Can pinpoint exact problem** - Binary search finds the issue fast
✅ **Real data isn't needed** - The infinite loop exists in your logic, not the data

## What to Look For

When debugging, watch for:

1. **Recursion without base case**
   - `strategy.OnBar()` calls itself
   - Event processor re-queues same event

2. **Unbounded loops**
   - `while (true)` without break condition
   - `for { ... }` that can't exit

3. **Goroutine deadlocks**
   - Two goroutines waiting on each other
   - One goroutine stuck on channel read

4. **Position/Order state issues**
   - Creating infinite pending requests
   - Event processing that can't complete

## Next Steps

1. ✅ Generate synthetic data (2 min)
2. ✅ Create test and run it (5 min)
3. ✅ Binary search to find problem month (10 min)
4. ✅ Find specific problematic candle (5 min)
5. ✅ Review trader/strategy code at that point
6. ✅ Fix the infinite loop
7. ✅ Verify with full year test

---

**Time estimate: 30-60 minutes to find and fix the infinite loop**
