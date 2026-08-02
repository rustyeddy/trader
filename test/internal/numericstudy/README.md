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

Run everything, including the assertions behind the findings below:

```sh
go test ./test/internal/numericstudy/ -v
```

## The tables are generated, not hand-written

Every table in this README and in ADR-004 sits between `numericstudy:` markers
and is generated from `Assets`, `Notionals`, `Rates`, and `Candidates`.
`TestGeneratedTables` fails if any region drifts from what the code produces,
so **the documents cannot silently fall out of date**.

Change the data, then regenerate both documents in place:

```sh
go test ./test/internal/numericstudy/ -run TestGeneratedTables -update
```

Do not hand-edit inside the markers — the next run will overwrite it, and CI
fails in the meantime. Two companion tests keep the mechanism honest:
`TestEveryFragmentIsUsed` catches a generated table that no document embeds,
and `TestFragmentsRenderInBothFormats` checks the org and markdown renderers
agree on structure.

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
`Price × Quantity`, using **whole, unscaled** quantities so only one operand
carries a scale and no descaling divide is needed. This is the favorable case —
see the caveat below the table:

<!-- BEGIN numericstudy:notional-8v9 -->
| Case            |          Quantity |        1e8 |      1e9 |
|-----------------|------------------:|-----------:|---------:|
| BRK.A block     |            10,000 |        12x |       1x |
| FX 10M notional |        10,000,000 |     8,502x |     850x |
| FX 1B notional  |     1,000,000,000 |        85x |       8x |
| BTC 1k coins    |             1,000 |       614x |      61x |
| SHIB 1e12 units | 1,000,000,000,000 | 9,223,372x | 922,337x |
<!-- END numericstudy:notional-8v9 -->

1e9 leaves the largest realistic notional essentially *at* the ceiling — a
BRK.A-sized block consumes the entire `int64` range with 1× to spare. 1e8 buys
back an order of magnitude for one decimal place that no intended asset class
needs. 8 decimals is also the natural stopping point: it is exactly satoshi
precision, the widest decimal requirement Trader accepts.

**Caveat, pending [#36](https://github.com/rustyeddy/trader/issues/36).** These
margins assume whole unscaled quantities. If `Quantity` gets its own scale,
`Price × Quantity` becomes double-scaled exactly like `Price × Rate`, the
margins above no longer apply, and production multiplication needs a widened
intermediate regardless of the price scale. That would not change the 1e8
recommendation — 1e8 still beats 1e9 by 10× — but it would move `Price ×
Quantity` from "safe in `int64`" to "must use the widened path."

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

<!-- BEGIN numericstudy:rolling-sum -->
| Scale | Bars of 750000.00 before overflow |
|-------|----------------------------------:|
| 1e5   |                       122,978,293 |
| 1e6   |                        12,297,829 |
| 1e8   |                           122,978 |
| 1e9   |                            12,297 |
<!-- END numericstudy:rolling-sum -->

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

<!-- BEGIN numericstudy:unsupported -->
| Format                              | Example      | Normalizes to | Reason                                                    |
|-------------------------------------|--------------|--------------:|-----------------------------------------------------------|
| Treasury 32nds (dash)               | `110-16`     |         110.5 | not decimal text; '-' is a fraction separator, not a sign |
| Treasury 32nds plus halves/quarters | `110-16.5`   |    110.515625 | fraction of a 32nd; expands to 64ths, needing 6 decimals  |
| Grain fractions in eighths          | `575'6`      |        575.75 | apostrophe-delimited eighths of a cent                    |
| Scientific notation                 | `1.5e-7`     |    0.00000015 | exponent form is rejected; expand before parsing          |
| Grouped thousands                   | `750,000.00` |     750000.00 | locale grouping is a display concern, not an exact input  |
<!-- END numericstudy:unsupported -->

All are representable once normalized, so **no intended instrument is excluded
by the 1e8 choice** — the constraint lands on the adapter layer, not on the
representation.

### Normalization is necessary but not sufficient

Converting a quotation to plain decimal text does not guarantee the result
fits the precision policy. A value finer than the 1e8 quantum is rejected even
though it is syntactically valid — `1.5e-8` expands to `0.000000015`, which
needs 9 decimals and **cannot** be represented at 1e8.

<!-- BEGIN numericstudy:subquantum -->
| Value         | Digits | Note                                         |
|---------------|-------:|----------------------------------------------|
| `0.000000015` |      9 | 1.5e-8 expanded; half of the 1e8 quantum     |
| `0.000000001` |      9 | one 1e9 unit; below the 1e8 quantum entirely |
| `1.000000005` |      9 | ordinary price carrying one digit too many   |
<!-- END numericstudy:subquantum -->

`TestSubQuantumValuesRejected` pins this: each is rejected at 1e8 with
`ErrTooManyDecimals` and accepted at 1e9, confirming the failure is the
precision policy rather than a parser defect. Whether adapters reject such
values or round them explicitly is [#38](https://github.com/rustyeddy/trader/issues/38)
and [#39](https://github.com/rustyeddy/trader/issues/39)'s call, not this
study's.

### Known parser gap: `math.MinInt64`

`FormatDecimal` renders `math.MinInt64`, but `ParseDecimal` cannot construct
it. Parsing builds the magnitude first and negates last, and the magnitude
`9223372036854775808` exceeds `MaxInt64`, so the guard fires one unit before
the true negative bound. The gap is exactly one representable value wide, and
`TestParseCannotConstructMinInt64` documents it. The final negative-range
policy belongs to [#39](https://github.com/rustyeddy/trader/issues/39): either
the parser accepts the full `int64` range, or the domain range is deliberately
symmetric.

## How the code is organized

| File               | Contents                                                                                |
|--------------------|-----------------------------------------------------------------------------------------|
| `scale.go`         | `ParseDecimal`, `FormatDecimal`, `FormatFixed`, `Canonical`, range and overflow helpers |
| `assets.go`        | The asset matrix, notional, rate, and sub-quantum cases — all as data                   |
| `tables.go`        | Renders each table fragment in org or markdown from that data                           |
| `scale_test.go`    | Round-trip, tick, boundary, and rejection tests                                         |
| `overflow_test.go` | Intermediate-arithmetic evidence, plus 128-bit mul/div study helpers                    |
| `golden_test.go`   | Verifies (and with `-update`, rewrites) the generated regions in this file and the ADR  |
| `nofloat_test.go`  | Fails if `float32`/`float64` or a float literal appears anywhere in the package         |
| `report_test.go`   | Prints the full report for reading                                                      |

Two deliberate choices in `scale.go`:

- **No binary floating point anywhere, including the reporting.** Parsing,
  formatting, round trips, tick checks, and every overflow and margin
  calculation are integer arithmetic end to end: decimal text converts directly
  to and from scaled `int64`, digit by digit, with an overflow guard on each
  step. A study arguing against binary floating point should not lean on it to
  reach its conclusions, so `TestNoFloatingPoint` fails if `float32` or
  `float64` appears anywhere in the package.
- **Parsing rejects rather than rounds.** `ErrTooManyDecimals`, `ErrOverflow`,
  and `ErrSyntax` are distinct because losing precision and exceeding range
  lead to different conclusions. A study that silently rounded would hide the
  exact failures it exists to surface.
