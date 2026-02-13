# Risk Policy

This package enforces the "Learning Account Trading Risk Policy" before orders are placed and during backtests.

Call `riskpolicy.Evaluate()` with:
- `Policy` (limits)
- `TradeIntent` (entry/stop/tp/units)
- `AccountSnapshot` (equity/margin/open trades)
- `PnLSnapshot` (day/week realized)
- `quoteToAccountRate` (often 1.0 for USD account trading EUR/USD)

If Decision.Allowed is false, do not place the order. Record the violations in the journal/DB.
