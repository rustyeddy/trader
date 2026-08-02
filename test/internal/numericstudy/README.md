# numericstudy

A design-investigation spike for
[#33 — Validate `Price` scale across supported asset classes](https://github.com/rustyeddy/trader/issues/33),
which feeds [ADR-004](../../../docs/arch/adr-004-exact-numeric-representation.org)
under parent decision [#19](https://github.com/rustyeddy/trader/issues/19).

**This is not the future `Price` type.** It lives under `test/internal/` so the
import path itself keeps it out of production code — nothing outside `test/`
can reference it. Delete or supersede it when #44 consolidates ADR-004.

## Printing the report

Print the validation tables to your terminal:

```sh
go test ./test/internal/numericstudy/ -run TestGenerateReport -v
```

Or write them straight to a file, with no test framing around them:

```sh
NUMERICSTUDY_REPORT=report.org go test ./test/internal/numericstudy/ -run TestGenerateReport
```

The tables come out as org-mode and paste directly into the ADR. They are
generated from the same `Assets`, `Notionals`, and `Rates` data that the
assertions exercise, so the document cannot drift from the evidence.

Run everything, including the assertions behind the findings below:

```sh
go test ./test/internal/numericstudy/ -v
```

## Recommendation: scale 1e8

`Price` uses a fixed internal scale of **1e8** — a decimal value `v` is stored
as the `int64` `v * 100_000_000`.

Four candidates were evaluated: 1e5, 1e6, 1e8, 1e9.

**1e5 and 1e6 are disqualified on precision.** Neither represents satoshi
precision (8 decimals), and 1e5 cannot represent the 64ths that Treasury
futures expand to. Both 1e8 and 1e9 represent every intended asset class
exactly, so precision alone does not separate them.

**Storage range separates nothing either.** 1e8 tops out at ~92.2 billion,
roughly 122,000× the highest-priced listed equity; 1e9 tops out at ~9.2
billion, still far above anything we intend to trade.

**Intermediate arithmetic decides it.** Margin to the `int64` ceiling for
`Price × Quantity`, where only one operand is scaled:

| Case                    | Quantity      |   1e8 |  1e9 |
| ----------------------- | ------------- | ----: | ---: |
| BRK.A block             | 10,000        |  12×  |  1×  |
| FX 10M notional         | 10,000,000    | 8503× | 850× |
| FX 1B notional          | 1,000,000,000 |   85× |   9× |
| BTC 1k coins            | 1,000         |  615× |  61× |

1e9 leaves the largest realistic notional essentially *at* the ceiling — a
BRK.A-sized block consumes the entire `int64` range with 1× to spare. 1e8 buys
back an order of magnitude for one decimal place that no intended asset class
needs. 8 decimals is also the natural stopping point: it is exactly satoshi
precision, the widest decimal requirement Trader accepts.

## Findings for #38 (rounding, overflow, intermediate arithmetic)

Two findings are the study's most consequential output, and neither is fixable
by choosing a different scale.

### 1. Double-scaled products overflow at every usable scale

`Price × Rate` scales *both* operands, so the product carries the scale twice
before the descaling divide. At 1e8, realistic pairs — BRK.A × a financing
rate, BTC/USD × a JPY conversion rate — overflow `int64`. 1e9 is worse. 1e5
escapes only because it cannot represent the wider assets at all, which is a
symptom of being too narrow rather than evidence of safety.

This requires a widened (128-bit) intermediate. `TestPriceTimesRateWidened`
demonstrates one that works and agrees with the narrow path wherever the narrow
path is valid.

### 2. Naive accumulation overflows inside a single backtest

A plain `int64` sum of BRK.A-priced bars overflows after ~122,978 bars at 1e8 —
under three months of M1 data.

| Scale | Bars of 750000.00 before overflow |
| ----- | --------------------------------: |
| 1e8   |                           122,978 |
| 1e9   |                            12,297 |

Rolling sums and averages need a widened accumulator or a running-mean
formulation, not a raw `int64` total.

## Tick size stays separate from the scale

Instrument tick size is parsed into the same internal scale but remains an
instrument property, distinct from the representation. With both in one scale,
increment validation is exact integer modulo (`price % tick == 0`) with no
per-scale special cases — asserted in `TestPriceIsMultipleOfTick`.

Display precision is likewise independent: a value stored at 1e8 renders at
whatever precision the instrument declares (`FormatFixed`).

## Unsupported quotation formats

These are provider quotation *conventions*, not decimal numbers. The parser
rejects all of them; each must be normalized to plain decimal text in a
provider adapter before reaching `Price`.

| Format                  | Example      | Normalizes to | Reason                                        |
| ----------------------- | ------------ | ------------- | --------------------------------------------- |
| Treasury 32nds          | `110-16`     | 110.5         | `-` is a fraction separator, not a sign       |
| Treasury 32nds + halves | `110-16.5`   | 110.515625    | expands to 64ths; needs 6 decimals            |
| Grain eighths           | `575'6`      | 575.75        | apostrophe-delimited eighths of a cent        |
| Scientific notation     | `1.5e-8`     | 0.000000015   | exponent form rejected; expand before parsing |
| Grouped thousands       | `750,000.00` | 750000.00     | locale grouping is display, not exact input   |

All are representable once normalized, so **no intended instrument is excluded
by the 1e8 choice** — the constraint lands on the adapter layer, not on the
representation.

## How the code is organized

| File               | Contents                                                                                |
|--------------------|-----------------------------------------------------------------------------------------|
| `scale.go`         | `ParseDecimal`, `FormatDecimal`, `FormatFixed`, `Canonical`, range and overflow helpers |
| `assets.go`        | The asset matrix, notional and rate cases, unsupported-quotation list — all as data     |
| `scale_test.go`    | Round-trip, tick, boundary, and rejection tests                                         |
| `overflow_test.go` | Intermediate-arithmetic evidence, plus 128-bit mul/div study helpers                    |
| `report_test.go`   | Generates the org tables above                                                          |

Two deliberate choices in `scale.go`:

- **No `float64` anywhere.** Decimal text converts directly to and from scaled
  `int64`, digit by digit, with an overflow guard on each step.
- **Parsing rejects rather than rounds.** `ErrTooManyDecimals`, `ErrOverflow`,
  and `ErrSyntax` are distinct because losing precision and exceeding range
  lead to different conclusions. A study that silently rounded would hide the
  exact failures it exists to surface.
