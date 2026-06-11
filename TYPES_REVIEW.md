# Review of types_*.go Files

**Date:** 2026-06-11  
**Scope:** All non-test files matching `types_*.go` in `/workspaces/trader`

## Executive Summary

The `types_*.go` layer is well-structured and handles domain types, fixed-point arithmetic, and time/candle processing coherently. However, several issues introduce maintenance risk and API confusion:

- **1 HIGH severity**: shadowing bug in `Aggregate()` method
- **4 MEDIUM severity**: duplicated bit helpers, double map lookups, misleading String methods, and misnamed types
- **3 LOW severity**: dead/test-only utilities, legacy wrappers, and unused type constants

Most issues are low-risk to fix and would improve clarity and reduce drift over time.

---

## HIGH SEVERITY

### 1. Shadow Variable Bug in `Aggregate()` Method

**File:** [types_candle.go](types_candle.go#L720-L730)  
**Symbols:** local closures `isValid`, `setValid`  
**Category:** Correctness

**Issue:**  
Local function closures in the `Aggregate()` method shadow package-level functions with identical names (defined [types_candle.go](types_candle.go#L258-L264)). The local versions redefine the same bit-twiddling logic without bounds optimization:

```go
// Package-level helpers (L258-264)
func setValid(valid []uint64, idx int) { ... }
func isValid(valid []uint64, idx int) bool { ... }

// Local closures in Aggregate() (L720-726)
isValid := func(i int) bool { ... }  // shadows package function
setValid := func(i int) { ... }      // shadows package function
```

**Why it matters:**  
Silent shadowing can lead to maintenance errors. If the package-level functions are updated, callers don't realize they're shadowed within this method, leading to subtle logic drift.

**Recommended Fix:**  
Rename the closures to avoid shadowing (e.g., `isValidLocal`, `setValidLocal`) or consolidate to a single set of helpers.

---

## MEDIUM SEVERITY

### 2. Fragmented Bit Manipulation Functions (3 implementations)

**Files:** [types_utils.go](types_utils.go#L51-L58), [types_candle.go](types_candle.go#L258-L264), [types_candle.go](types_candle.go#L720-L730)  
**Symbols:** `bitIsSet`/`bitSet`, `setValid`/`isValid`, local closures in `Aggregate()`  
**Category:** Redundancy

**Issue:**  
Same bitset logic exists in three places with overlapping semantics:
- `bitIsSet`/`bitSet` as standalone helpers in `types_utils.go` (used in tests and throughout codebase)
- `setValid`/`isValid` as standalone functions in `types_candle.go`
- Local closures `isValid`/`setValid` inside `Aggregate()` method

**Why it matters:**  
Multiple entry points for identical operations create maintenance risk. If a bug is found in bit logic, you must fix it in 3 places. Naming inconsistency makes it unclear which to use.

**Recommended Fix:**  
Consolidate into a single canonical implementation. Best approach: keep only one set of package-level helpers and replace method-level implementations with calls to them.

---

### 3. Double Map Lookup in `GetInstrument()`

**File:** [types_instruments.go](types_instruments.go#L188-L200)  
**Symbol:** `GetInstrument()`  
**Category:** Correctness/Performance

**Issue:**  
The function performs two lookups in the `Instruments` map—first directly, then again after symbol normalization:

```go
func GetInstrument(symbol string) *Instrument {
    if inst, ok := Instruments[symbol]; ok {           // First lookup
        return inst
    } else {
        if symbol, ok = symmap[symbol]; ok {            // Normalize via symmap
            if inst, ok = Instruments[symbol]; ok {     // Second lookup (redundant)
                return inst
            }
        }
    }
    return nil
}
```

If the first branch succeeds, the else clause never executes, so the second lookup is unnecessary.

**Why it matters:**  
Unnecessary map lookups reduce performance in hot code. Also, the `else` structure is misleading to readers.

**Recommended Fix:**  
Refactor to a single lookup path: attempt normalization first, then perform one `Instruments` lookup.

---

### 4. Unused Type: `symbol` (lowercase) and Orphaned Constants

**File:** [types_instruments.go](types_instruments.go#L9-L22)  
**Symbol:** `symbol` type + constants (EUR_USD, GBP_USD, etc.)  
**Category:** Redundancy

**Issue:**  
The type `symbol` is defined but only used to type constants on lines 12–22. These constants are never referenced elsewhere in the codebase; callers use string literals or the `symmap` for normalization instead.

**Why it matters:**  
Adds conceptual duplication without providing value. Tests and production code both use strings or the `symmap`, not these typed constants.

**Recommended Fix:**  
Remove the type definition and constants. If symbol lookup is needed, rely on `symmap` or inline string literals.

---

### 5. Misleading String Methods on Money and Price Types

**File:** [types_money.go](types_money.go#L39), [types_money.go](types_money.go#L82)  
**Symbols:** `Money.String()`, `Price.String()`  
**Category:** API Design

**Issue:**  
Both methods print raw scaled integers as floating-point format instead of scaled human values:

```go
func (m Money) String() string {
    return fmt.Sprintf("%f", float64(m))  // Prints raw internal units, not scaled value
}

func (p Price) String() string {
    return fmt.Sprintf("%f", float64(p))  // Prints raw internal units, not scaled value
}
```

Expected usage: callers see `Money.String()` or `Price.String()` and assume human-readable output, but get raw internal units instead.

**Why it matters:**  
These methods look like display formatters but represent internal accounting units. Leaking raw values into logs, CSV, or UI produces confusing or wrong output. Readers (developers and consumers) misinterpret values.

**Recommended Fix:**  
Either:
1. Make `String()` return properly scaled human values with fixed precision, OR
2. Rename methods to `RawString()` and add explicit `String()` that uses `Float64()` with appropriate precision

---

### 6. Misnamed Type: `Pips` Implies Whole Pips But Stores Tenths

**File:** [types_units.go](types_units.go#L50-L65)  
**Symbol:** `Pips` type, `pipScale` constant  
**Category:** Naming

**Issue:**  
The type is named `Pips` but the internal implementation stores tenths of a pip (scale 10):

```go
type Pips int32

const pipScale = 10  // tenths of a pip

// Comment says: "1 == .1 pip and 20 == 2 pips"
func PipsFromFloat(v float64) Pips {
    return Pips(math.Round(v * pipScale))
}
```

Callers see `Pips` and assume 1 = 1 pip, not 0.1 pip.

**Why it matters:**  
Easy to misuse in strategy/risk code where pip calculations are critical. A trader using `Pips` type without reading docs might place wrong-sized positions.

**Recommended Fix:**  
Rename to `DeciPips` or `TenthsOfPips`, OR keep `Pips` but make constructor names explicit (e.g., `PipsFromFloatTenths`, `WholePipsFromFloat`) and add doc comments.

---

## MEDIUM-LOW SEVERITY

### 7. Non-Idiomatic String Method Signature on `candleTime`

**File:** [types_candle.go](types_candle.go#L60-L62)  
**Symbol:** `String` function (not method)  
**Category:** Naming

**Issue:**  
Defined as a function `func String(c candleTime) string` instead of a receiver method `func (c candleTime) String() string`. Does not satisfy the `fmt.Stringer` interface, so it won't be called automatically by `fmt.Printf("%v")`.

**Recommended Fix:**  
Convert to receiver method:
```go
func (c candleTime) String() string {
    return c.Candle.String()
}
```

---

## LOW SEVERITY

### 8. Dead or Test-Only Utility Functions in Production Types File

**File:** [types_utils.go](types_utils.go#L28), [types_utils.go](types_utils.go#L63), [types_utils.go](types_utils.go#L94)  
**Symbols:** `parseEST()`, `secondsToTFString()`, `tfStringToSeconds()`  
**Category:** Surface Area

**Issue:**  
Current usage is test-only:
- `parseEST` used only in `types_utils_test.go`
- `secondsToTFString` used only in tests
- `tfStringToSeconds` used only in tests

**Why it matters:**  
Increases the surface area of core types without runtime value in production code.

**Recommended Fix:**  
Move these to test helper files or mark clearly as test-support functions. Consider moving to a test utility module or prefixing with `test_` if they remain in production types.

---

### 9. Legacy Naming Noise: Multiple Market-Close Wrappers

**File:** [types_time.go](types_time.go#L251-L260)  
**Symbols:** `isFXMarketClosed()`, `IsForexMarketClosed()`, `isForexMarketClosed()`  
**Category:** API Clutter

**Issue:**  
Three near-identical names for one behavior:
- Private `isFXMarketClosed()` delegates to `isForexMarketClosed()`
- Public `IsForexMarketClosed()` delegates to `isForexMarketClosed()`
- Canonical implementation `isForexMarketClosed()`

Comment notes this is "retained for backward compatibility."

**Why it matters:**  
Adds discoverability friction and conceptual clutter without functional benefit.

**Recommended Fix:**  
Keep one canonical exported API (`IsForexMarketClosed`) and one private implementation (`isForexMarketClosed`). Remove or deprecate the `isFXMarketClosed` wrapper with a comment + removal plan.

---

### 10. Orphaned Version Suffix: `candleSetIteratorV1`

**File:** [types_candle.go](types_candle.go#L794-L795)  
**Symbol:** `candleSetIteratorV1` type  
**Category:** Naming

**Issue:**  
Type name has "V1" suffix, suggesting versioning or multiple iterator strategies, but no V2, V3, etc. exist. Likely leftover from refactoring.

**Recommended Fix:**  
Rename to `candleSetIterator` (remove the "V1" suffix).

---

## POSITIVE PATTERNS TO PRESERVE

### 1. Parameter Extraction Layer (`types_param.go`)
The `GetIntParam`, `GetFloat64Param`, `GetBoolParam`, `GetStringParam` functions use a consistent `(value, found, error)` return pattern. Excellent design for flexible config parsing.

### 2. Well-Structured Fixed-Point Arithmetic (`types_math.go`)
Uses 128-bit intermediate precision for safe overflow detection (`mulDiv64`, `mulDivCeil64`, `mulDivFloor64`). Defensive programming that prevents silent underflow/overflow in trading calculations.

### 3. Comprehensive Time Handling (`types_time.go`)
Includes timezone-aware forex market-close logic and clear `TimeRange` abstractions. Well-tested and practical.

### 4. Thread-Safe Lot Management (`types_lot.go`)
Uses `sync.RWMutex` correctly for concurrent access patterns. Safe concurrency model.

### 5. Consistent Domain Type Wrappers
The scaled-int types (`Money`, `Price`, `Rate`, `Units`) follow a coherent pattern with `FromFloat()` and `Float64()` conversions at boundaries. Clean boundary-layer design.

---

## SUMMARY TABLE

| Severity | File | Symbol | Category | Issue |
|----------|------|--------|----------|-------|
| **HIGH** | types_candle.go | `isValid`, `setValid` (closures) | Correctness | Shadow bug in `Aggregate()` |
| MEDIUM | types_utils.go, types_candle.go | `bitIsSet`/`setValid`/`SetValid` | Redundancy | Fragmented bit logic (3 implementations) |
| MEDIUM | types_instruments.go | `GetInstrument()` | Correctness | Double map lookup |
| MEDIUM | types_instruments.go | `symbol` type + constants | Redundancy | Unused type for constants |
| MEDIUM | types_money.go | `Money.String()`, `Price.String()` | API Design | Misleading String methods |
| MEDIUM | types_units.go | `Pips` | Naming | Type implies whole pips but stores tenths |
| MED-LOW | types_candle.go | `String()` function | Naming | Non-idiomatic method signature |
| LOW | types_utils.go | `parseEST`, `secondsToTFString`, `tfStringToSeconds` | Surface Area | Dead/test-only utilities |
| LOW | types_time.go | `isFXMarketClosed`, `IsForexMarketClosed` | API Clutter | Legacy wrapper noise |
| LOW | types_candle.go | `candleSetIteratorV1` | Naming | Orphaned version suffix |

---

## RECOMMENDED CLEANUP ORDER

### Phase 1 (Low-Risk, High-Value)
1. Remove `symbol` type and unused constants from `types_instruments.go`
2. De-noise legacy market-close wrappers in `types_time.go`
3. Rename `candleSetIteratorV1` to `candleSetIterator`
4. Convert `String(candleTime)` function to receiver method
5. Move test-only utilities to test files or deprecate

### Phase 2 (Medium-Risk, Medium-Value)
6. Consolidate bit helpers (choose canonical location, remove duplicates)
7. Simplify `GetInstrument()` lookup path
8. Rename `Pips` type to `DeciPips` with explicit documentation

### Phase 3 (Higher-Risk, Lower-Value)
9. Fix `String()` methods on `Money` and `Price` (requires careful audit of current callers)
10. Fix shadow bug in `Aggregate()` (verify test coverage first)

---

## Notes for Implementation

- All changes should include regression tests to catch subtle logic drift
- The bit-helper consolidation requires the most careful review to ensure no behavior changes
- The `String()` method changes on `Money`/`Price` should be socialized with all call sites first
- Refer to the "Positive Patterns to Preserve" section when making changes—the core design is sound
