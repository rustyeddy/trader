# quantitystudy

A design-investigation spike for
[#36 — Decide `Quantity` precision and asset constraints](https://github.com/rustyeddy/trader/issues/36),
which feeds [ADR-004](../../../docs/arch/adr-004-exact-numeric-representation.org)
under parent decision [#19](https://github.com/rustyeddy/trader/issues/19).

**This is not the future `Quantity` type.** Like its sibling
[`numericstudy`](../numericstudy/), it lives under `test/internal/` so the
import path keeps it out of production code. Delete or supersede it when #44
consolidates ADR-004.

It reuses `numericstudy` for exact decimal parsing and formatting, table
rendering, and the generated-documentation machinery, so both studies share one
implementation of the "no binary floating point" guarantee and one
implementation of the drift guard.

## Running it

```sh
go test ./test/internal/quantitystudy/ -v                              # everything
go test ./test/internal/quantitystudy/ -run TestGenerateReport -v      # the report
go test ./test/internal/quantitystudy/ -run TestGeneratedTables -update # refresh docs
```

Tables in this file and in ADR-004 sit between `quantitystudy:` markers and are
generated. `TestGeneratedTables` fails if any region drifts from what the code
produces. Do not hand-edit inside the markers.

## Recommendation: scale 1e8, with an explicitly bounded range

`Quantity` uses a fixed Trader-wide scale of **1e8** — the same scale as
`Price`. A decimal quantity `v` is stored as the `int64` `v * 100_000_000`.

- smallest representable quantity: **0.00000001** (one satoshi)
- maximum whole-unit quantity: **92,233,720,368** (~92.2 billion)
- quantities above that bound are **rejected as out of range**, never truncated
- extremely high-supply token positions above the bound are **outside the
  initial supported domain**

The recommendation rests on precision coverage plus an explicitly bounded
range, not on holding every value in the pressure matrix. See the frontier
below for why that distinction is forced rather than chosen.

### Quantity representation matrix

`PRECISION` marks a value needing more decimals than the scale holds;
`RANGE` marks one exceeding `int64` at that scale. The two are reported
separately because they lead to different conclusions — one narrows the asset
classes a scale can serve, the other bounds the supported domain.

<!-- BEGIN quantitystudy:quantity-matrix -->
| Class    | Symbol  |      Quantity | Dec | Integral |                 1e6 |               1e7 |                1e8 |                 1e9 |
|----------|---------|--------------:|----:|----------|--------------------:|------------------:|-------------------:|--------------------:|
| FX       | EUR/USD |             1 |   0 | no       |             1000000 |          10000000 |          100000000 |          1000000000 |
| FX       | EUR/USD |         10000 |   0 | no       |         10000000000 |      100000000000 |      1000000000000 |      10000000000000 |
| FX       | EUR/USD |      10000000 |   0 | no       |      10000000000000 |   100000000000000 |   1000000000000000 |   10000000000000000 |
| FX       | EUR/USD |    1000000000 |   0 | no       |    1000000000000000 | 10000000000000000 | 100000000000000000 | 1000000000000000000 |
| Equity   | SPY     |             1 |   0 | no       |             1000000 |          10000000 |          100000000 |          1000000000 |
| Equity   | SPY     |           0.5 |   1 | no       |              500000 |           5000000 |           50000000 |           500000000 |
| Equity   | SPY     |      0.000001 |   6 | no       |                   1 |                10 |                100 |                1000 |
| Futures  | ES      |             1 |   0 | yes      |             1000000 |          10000000 |          100000000 |          1000000000 |
| Futures  | ES      |             3 |   0 | yes      |             3000000 |          30000000 |          300000000 |          3000000000 |
| Futures  | ES      |         10000 |   0 | yes      |         10000000000 |      100000000000 |      1000000000000 |      10000000000000 |
| Crypto   | BTC     |             1 |   0 | no       |             1000000 |          10000000 |          100000000 |          1000000000 |
| Crypto   | BTC     |    0.00000001 |   8 | no       |           PRECISION |         PRECISION |                  1 |                  10 |
| Token    | SHIB    | 1000000000000 |   0 | no       | 1000000000000000000 |             RANGE |              RANGE |               RANGE |
| Boundary | ZERO    |             0 |   0 | no       |                   0 |                 0 |                  0 |                   0 |
<!-- END quantitystudy:quantity-matrix -->

### The precision/range frontier

No scaled `int64` holds both extremes. One satoshi needs 8 fraction digits; a
trillion whole units at 8 fraction digits needs 1e20, which is 67 bits against
`int64`'s 63.

<!-- BEGIN quantitystudy:frontier -->
| Scale | Smallest unit |   Max whole units | Holds 1 satoshi | Holds 1e12 units |
|-------|--------------:|------------------:|-----------------|------------------|
| 1e6   |      0.000001 | 9,223,372,036,854 | no              | yes              |
| 1e7   |     0.0000001 |   922,337,203,685 | no              | no               |
| 1e8   |    0.00000001 |    92,233,720,368 | yes             | no               |
| 1e9   |   0.000000001 |     9,223,372,036 | yes             | no               |
<!-- END quantitystudy:frontier -->

Only 1e6 holds a trillion units, and it cannot represent a satoshi. Only 1e8
and 1e9 represent a satoshi, and neither reaches a trillion units. Trading
range for precision is therefore not a free move — it swaps which asset class
is excluded.

Per [#36](https://github.com/rustyeddy/trader/issues/36), the trillion-unit row
is a **range-pressure case, not a requirement**. It is retained in the evidence
to mark the boundary rather than removed or quietly made to pass.
`TestOutOfDomainQuantityIsRejected` asserts it fails with a range error
specifically, so the boundary is visible in behaviour and not just in prose.

### Why 1e8 rather than 1e9

Both cover every intended asset class exactly. 1e9 spends a ninth decimal place
that nothing in the matrix needs, and pays a factor of ten in supported range
for it. 1e8 also matches the `Price` scale fixed by
[#33](https://github.com/rustyeddy/trader/issues/33), which keeps the two
scales in one mental model.

Explicitly **not** adopted, per #36: per-asset-class scales, or a widened
`Quantity`, solely for the pressure case. Either would add substantial
complexity to all quantity arithmetic, serialization, and APIs for a
requirement Trader does not currently have. If a concrete broker or instrument
later needs positions above the bound, the representation decision reopens with
real provider constraints — likely toward a widened backing representation or a
specialised asset-unit model, not silent scale variation inside `Quantity`.

## Representation scale is separate from instrument rules

The scale says what can be **stored**; `QuantityRules` says what may be
**traded**:

```go
type QuantityRules struct {
    Increment    int64 // smallest tradable step
    Minimum      int64 // smallest permitted order size
    Maximum      int64 // largest permitted order size
    IntegralOnly bool  // whole units only, e.g. futures contracts
}
```

<!-- BEGIN quantitystudy:instrument-rules -->
| Symbol  |  Increment |  Minimum |   Maximum | Integral only |
|---------|-----------:|---------:|----------:|---------------|
| EUR/USD |          1 |        1 | 100000000 | no            |
| SPY     |   0.000001 | 0.000001 |         — | no            |
| ES      |          1 |        1 |     10000 | yes           |
| BTC     | 0.00000001 |   0.0001 |         — | no            |
| SHIB    |          1 |     1000 |         — | no            |
<!-- END quantitystudy:instrument-rules -->

Holding quantity and increment in the same scale makes increment validation
exact integer modulo — `q % increment == 0`, no tolerance, no rounding.
`TestRulesAreIndependentOfScale` shows the same rule set behaving identically at
every scale that can represent it.

### Zero is representable; only orders reject it

Zero is a perfectly good `Quantity` — a flat position, a fully closed lot — so
the representation must hold it. `Validate` accepts it as instrument-conformant
and `ValidateOrder` rejects it, keeping the two questions apart.

### Direction is never encoded in the sign

Public order quantities are non-negative. A negative quantity is a programming
error, not a sell: side belongs to the order. `TestNonNegative` pins this,
including that `math.MinInt64` is rejected as a quantity rather than mishandled.

## Consequence for #38: scaled `Quantity` needs widened arithmetic

With `Price` at 1e8 and `Quantity` scaled too, `Price × Quantity` is
**double-scaled** — the same shape as `Price × Rate` in #33 — so the
intermediate overflows `int64` before the descaling divide.

<!-- BEGIN quantitystudy:notional -->
| Case            |           Price |      Quantity |      1e6 |      1e7 |      1e8 |      1e9 |
|-----------------|----------------:|--------------:|---------:|---------:|---------:|---------:|
| BRK.A block     |       750000.00 |         10000 | OVERFLOW | OVERFLOW | OVERFLOW | OVERFLOW |
| FX 10M notional |         1.08473 |      10000000 | OVERFLOW | OVERFLOW | OVERFLOW | OVERFLOW |
| FX 1B notional  |         1.08473 |    1000000000 | OVERFLOW | OVERFLOW | OVERFLOW | OVERFLOW |
| BTC 1k coins    | 150000.12345678 |          1000 | OVERFLOW | OVERFLOW | OVERFLOW | OVERFLOW |
| SPY fractional  |          700.12 |           0.5 |     263x |      26x |       2x | OVERFLOW |
| SHIB 1e12 units |      0.00000001 | 1000000000000 |       9x |      n/a |      n/a |      n/a |
<!-- END quantitystudy:notional -->

This confirms the caveat ADR-004 already recorded against #36 from the price
study. `TestWholeUnitQuantityIsSafer` isolates the cost precisely: the same
BRK.A-sized block is comfortable with a whole unscaled quantity and overflows
with a scaled one. Scaling `Quantity` is what pushes the product past `int64`,
so notional calculation must use the widened path that
[#38](https://github.com/rustyeddy/trader/issues/38) owns.

## How the code is organized

| File                | Contents                                                                    |
|---------------------|-----------------------------------------------------------------------------|
| `quantity.go`       | `QuantityRules` and validation, `SelectedScale`, supported-range helpers    |
| `assets.go`         | Quantity matrix, instrument rules, and notional cases — all as data         |
| `tables.go`         | `Frontier` analysis and the generated table fragments                       |
| `quantity_test.go`  | Checks 1–10 from #36                                                        |
| `golden_test.go`    | Verifies (and with `-update`, rewrites) the generated regions               |
| `nofloat_test.go`   | Fails if `float32`/`float64` or a float literal appears in the package      |
| `report_test.go`    | Prints the full report for reading                                          |
