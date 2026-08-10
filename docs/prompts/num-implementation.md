You are working in the `rustyeddy/trader` repository.

Create and switch to a new branch named:

```text
num
```

Before writing code, read the following completely:

1. `docs/arch/adr-004-exact-numeric-representation.org`
2. GitHub issue `#22` — `M1-04 Implement exact foundational value types`

ADR-004 is the authoritative design contract. Do not reopen or reinterpret its settled decisions unless the document contains a genuine contradiction that prevents implementation. If you encounter such a contradiction, stop and describe it clearly before proceeding.

## Objective

Implement the exact foundational numeric types described by ADR-004 and issue #22 as a clean-room implementation.

Do not copy production numeric code wholesale from `trader-first-try`. The legacy repository may be consulted only for test scenarios, edge cases, golden vectors, or algorithmic lessons. It must not become a dependency.

## Package architecture

Use this package structure:

```text
num/
    doc.go

    price.go
    price_test.go

    quantity.go
    quantity_test.go

    rate.go
    rate_test.go

    currency.go
    currency_test.go

    money.go
    money_test.go

    encoding.go
    encoding_test.go

    internal/
        fixed/
            scale.go
            errors.go
            checked.go
            wide.go
            rounding.go
            decimal.go
            decimal_test.go
```

Minor file-name adjustments are acceptable if they improve cohesion, but preserve the package boundaries:

```text
github.com/rustyeddy/trader/num
github.com/rustyeddy/trader/num/internal/fixed
```

Do not create a `pkg/` directory.

## Architectural boundary

The public `num` package contains semantic Trader values:

```go
num.Price
num.Quantity
num.Rate
num.Currency
num.Money
```

The private package:

```text
num/internal/fixed
```

contains only exact scaled-integer representation mechanics, including:

* the common scale;
* checked `int64` arithmetic;
* signed widened multiplication and division;
* scale restoration;
* rounding;
* exact decimal parsing;
* canonical decimal formatting;
* representation-level errors.

Packages outside `num` must never import or access `num/internal/fixed`.

Accounts, brokers, strategies, indicators, risk, portfolio, persistence, and other Trader packages must use the semantic types exported by `num`. They must not know the raw scale, manipulate raw scaled integers, or call generic fixed-point primitives directly.

The architectural rule is:

> Code outside `num` expresses financial intent. Code inside `num/internal/fixed` implements exact representation mechanics.

Do not expose a generic public decimal or fixed-point type.

## Context-dependent arithmetic

Do not place every financial calculation in `num`.

Universally valid semantic operations belong in `num`, for example:

```go
Price.MulRate(Rate) (Price, error)
Money.MulRate(Rate) (Money, error)
Quantity.MulRate(Rate) (Quantity, error)

Money.Add(Money) (Money, error)
Money.Sub(Money) (Money, error)
```

Context-dependent calculations belong in their eventual domain packages.

For example:

```text
Price × Quantity -> Money
```

may require quotation currency, instrument metadata, contract multipliers, lot conventions, or broker rules. The widened arithmetic primitive belongs in `num/internal/fixed`, but do not invent a context-free public `num` operation when the domain meaning is incomplete.

## Implementation sequence

Implement the work incrementally in reviewable commits.

Recommended order:

1. Add the private fixed-point kernel.
2. Implement `Rate`.
3. Implement `Price`.
4. Implement `Quantity`.
5. Implement `Currency`.
6. Implement `Money`.
7. Implement text and JSON encoding.
8. Add architecture enforcement and comprehensive tests.

The branch may contain the complete implementation, but each commit should be coherent, focused, and leave the test suite passing.

## Fixed-point kernel requirements

Implement the behavior required by ADR-004, including:

* signed `int64` authoritative representation;
* common scale `1e8`;
* checked addition and subtraction;
* checked negation and absolute value;
* explicit rejection of invalid `math.MinInt64` operations;
* widened signed intermediate arithmetic for scaled multiplication and division;
* no `float64` overflow escape path;
* default rounding to nearest, ties to even;
* explicit alternative rounding policies where ADR-004 requires them;
* division-by-zero detection;
* overflow and underflow errors;
* exact decimal parsing without `float64`;
* full signed `int64` parsing support, including `math.MinInt64`;
* canonical decimal formatting without `float64`;
* no scientific notation;
* no thousands separators;
* no unnecessary trailing zeros;
* no trailing decimal point;
* normalization of negative zero to `0`;
* exact parse/format round trips.

Normal public operations must return errors. Panic-based `Must...` helpers, if any, are limited to programmer-controlled constants, fixtures, and tests.

Do not silently wrap, clamp, saturate, truncate, or round unless an operation explicitly selects a documented rounding policy.

## Semantic type requirements

Follow ADR-004 exactly.

### Price

* Backed by scaled signed `int64`.
* Scale `1e8`.
* Public valid values are normally non-negative.
* Tick size and display precision are not part of `Price`; they belong to instrument or listing rules.
* Zero is representable, with validity determined by context.

### Quantity

* Backed by scaled signed `int64`.
* Scale `1e8`.
* Public values are non-negative.
* Direction is represented by order side, never quantity sign.
* Zero is representable, but order construction will eventually require quantity greater than zero.
* Increment, minimum, maximum, and integral-only rules belong outside the representation type.

### Rate

* Backed by scaled signed `int64`.
* Scale `1e8`.
* Signed.
* It is the foundational exact dimensionless representation.
* Domain-specific constraints should eventually use semantic wrappers rather than weakening the base representation.

### Currency

* Validate supported currency identifiers according to the repository’s existing conventions and ADR-004.
* Do not silently accept malformed or unspecified currencies.
* Keep the implementation suitable for currencies beyond FX-only assumptions.

### Money

Conceptually:

```go
type Money struct {
    amount   int64
    currency Currency
}
```

Requirements:

* Signed amount at scale `1e8`.
* Currency is mandatory.
* The Go zero value of `Money` is invalid or unspecified because it has no currency.
* Valid zero money must be constructed explicitly with a currency.
* Same-currency addition and subtraction are allowed with checked overflow.
* Cross-currency arithmetic must return an error.
* Currency conversion is not implicit.
* Any future conversion operation must require target currency, rate, and provenance.
* Do not expose a lower-level currency-free amount as the public domain value.

## Public boundaries

External callers should construct semantic values through exact decimal text or safe semantic constructors.

Raw scaled `int64` construction must be narrowly scoped and clearly identified as already scaled. It is intended only for trusted internal code, persistence reconstruction, representation tests, or similarly controlled use.

Do not expose raw scaled integers as the ordinary public API.

Do not provide implicit `float64` conversion.

Any unavoidable future float conversion belongs in provider adapters, must be explicitly named as rounded or lossy, and must validate NaN, infinity, range, rounding, and semantic invariants.

## Serialization

Implement ADR-004 serialization behavior:

* `Price`, `Quantity`, and `Rate` serialize publicly as canonical decimal strings.
* `Money` serializes as a structured value containing:

  * canonical decimal amount;
  * explicit currency.
* Text and JSON round trips must be exact.
* Public serialization must not expose raw scaled integers.
* Writers emit canonical forms.
* Readers may accept exact equivalent decimal forms when ADR-004 permits them.
* Do not use `float64` during encoding or decoding.

Persistence-specific database adapters may be deferred if issue #22 does not require a database package yet, but the public types must support exact reconstruction suitable for scaled `BIGINT` plus explicit currency storage.

## Determinism and float containment

Authoritative calculations must be deterministic and exact.

Binary floating point is prohibited from:

* `num`;
* domain values;
* brokerage;
* accounting;
* risk;
* orders;
* positions;
* portfolio;
* persistence;
* authoritative public APIs.

Do not add `float64` fields, parameters, return values, or internal arithmetic to the public `num` implementation.

Indicators may eventually use approved sealed floating-point implementations, but that is outside this package. Such code must accept and return semantic `num` values and quantize before an authoritative decision boundary.

Add an automated check if practical to prevent accidental floating-point use in `num` and `num/internal/fixed`.

## Testing requirements

Implement thorough tests with the code, not afterward.

Include:

* zero, positive, and negative cases where semantically valid;
* all eight decimal places;
* trailing-zero acceptance;
* canonical trailing-zero removal;
* malformed input;
* excess precision;
* out-of-range input;
* full `math.MinInt64` parsing for signed types;
* rejection of negative `Price` and `Quantity`;
* invalid or missing currency;
* same-currency arithmetic;
* cross-currency arithmetic rejection;
* checked overflow and underflow;
* negation and absolute-value failure at `math.MinInt64`;
* widened realistic `Price × Rate`;
* widened realistic `Quantity × Rate`;
* midpoint rounding for positive and negative values;
* nearest-even tie behavior;
* division by zero;
* exact text round trips;
* exact JSON round trips;
* negative-zero normalization;
* absence of silent wrapping;
* absence of `float64` in exact parsing, formatting, and arithmetic.

Use table-driven tests where appropriate.

Add fuzz tests or property tests for high-value invariants, especially:

```text
Parse(Format(x)) == x
```

and checked arithmetic boundary behavior.

Reuse useful scenarios from `trader-first-try` only after translating them to ADR-004 semantics. Do not preserve obsolete assumptions such as:

* `Price` backed by `int32`;
* scales of `1e5` or `1e6`;
* float-based formatting;
* panic-first external constructors;
* FX-only precision;
* currency-free `Money`.

## Quality gates

Before finishing:

1. Run the full repository tests.
2. Run `make check` if available.
3. Run formatting and static analysis used by the repository.
4. Confirm no package outside `num` imports `num/internal/fixed`.
5. Confirm the implementation does not expose raw scale details unnecessarily.
6. Confirm the exported API contains only semantically valid operations.
7. Update issue #22’s implementation notes or prepare a concise PR description mapping the implementation to its acceptance criteria.

Do not merge the branch.

At completion, report:

* commits created;
* files added or changed;
* exported API summary;
* tests added;
* commands run and results;
* any intentionally deferred portions of issue #22;
* any ADR ambiguity encountered.
