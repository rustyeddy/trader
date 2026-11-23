# Practical financial correctness baseline

Issue #532 is about independent financial reference values, not chasing
sub-cent artifacts that cannot affect ordinary trading decisions.

The initial goldens in `financial_golden_test.go` check:
- USD-quoted EURUSD, 10,000 units, and a $10,000 account.
- Long/short wins and losses, mark-to-market equity, close-and-realize,
  and final account balances.
- Adverse fill prices (spread/slippage represented through fills).
- A three-point equity path suitable for an independently computed
  peak-to-trough drawdown check.

**Hand-calculated reference:** for 10,000 units, a 0.00100 price move is
$10 of P&L; a 0.00300 adverse mark is -$30; $30/$10,000 = 0.30%.
The account keeps precise money internally; presentation to cents is
separate. No separate commission/financing debit is modeled by these
fixtures: do not interpret their absence as zero real-world costs.

This is intentionally a *first increment*, not closure of #532. A later
focused test can independently assert the reporting layer's max drawdown
and any modeled commissions once those are explicit in the engine. Keep
edge-case rounding analysis in existing numeric tests or a documented
limitation unless it causes a material order, risk, or accounting error.
