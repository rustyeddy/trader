# Trader — independent repository architecture and code review

**Review date:** 2026-10-17  
**Repository:** https://github.com/rustyeddy/trader  
**Reviewed main commit:** [`cc14c6c153bb9c48cf98823fde9ef2c5747f05ec`](https://github.com/rustyeddy/trader/tree/cc14c6c153bb9c48cf98823fde9ef2c5747f05ec) (merged PR #530, 2026-10-16)

## Executive assessment

Trader has a serious and well-considered architecture. It avoids many
traps that commonly invalidate trading software: implicit
floating-point accounting, timestamp/look-ahead errors, direct
strategy-to-broker calls, implicit data repair, missing provenance,
and simulations that silently pretend uncertain fills are certain. The
foundational domain model is worth retaining.

**It is also more complex than its current operational maturity
warrants.** The combination of dozens of packages, very detailed ADRs,
numerous envelope and DTO layers, long policy comments, and
increasingly large orchestration files carries a real maintenance and
feature-development cost. The answer is *selective simplification*,
not a third rewrite or wholesale collapse of the existing boundaries.

The highest-priority work is to improve **fidelity and correctness at
the boundaries**—especially FX execution pricing and canonical-data
completeness—before extending research and strategy complexity.

## Review scope and limitations

This is a repository-wide structure/dependency/contract audit combined
with direct code inspection of representative and critical
implementations across `marketdata`, `internal/marketdata`,
`internal/backtest`, `internal/adapters/broker`, `internal/pipeline`,
`internal/risk`, `internal/service`, `cmd`, `sdk`, and MCP. It is
**not** a line-by-line review of every Go file. Tests were not
executed in this environment; the local Go toolchain is older than the
repository's `go 1.25.1` requirement and direct GitHub clone access is
unavailable here. The latest GitHub Actions workflow on the reviewed
commit **succeeded**, covering tidy, formatting, vet, lint, tests and
race tests. Thus findings below are static-code findings or explicitly
identified architectural risks, not assertions that CI is failing.

At the reviewed commit, the repository had approximately **1,141
tracked blobs**, including **933 Go files** (442 `*_test.go` files,
489 other handwritten Go files and two generated Protobuf files), **79
Go package directories**, **125 Markdown/Org documentation files**,
and roughly **74 individual ADR files**. These figures are inventory
counts, not complexity or quality scores.

## Findings, ordered by priority

### F1 — P1: Standard FX backtests omit the bid/ask execution spread

**Evidence:** [`marketdata/bar.go`](https://github.com/rustyeddy/trader/blob/cc14c6c153bb9c48cf98823fde9ef2c5747f05ec/marketdata/bar.go#L104-L132) defines FX canonical OHLC as *bid*-basis and stores average/maximum spread. [`cmd/internal/backtestcfg/environment.go`](https://github.com/rustyeddy/trader/blob/cc14c6c153bb9c48cf98823fde9ef2c5747f05ec/cmd/internal/backtestcfg/environment.go#L89-L112) returns `bar.Open` for either order side; the same file [constructs the simulator without slippage/commission models](https://github.com/rustyeddy/trader/blob/cc14c6c153bb9c48cf98823fde9ef2c5747f05ec/cmd/internal/backtestcfg/environment.go#L183-L190) and explicitly calls them `none` ([lines 232–251](https://github.com/rustyeddy/trader/blob/cc14c6c153bb9c48cf98823fde9ef2c5747f05ec/cmd/internal/backtestcfg/environment.go#L232-L251)).

**Impact:** A buy on bid-based data executes at the bid, not the ask;
its market spread cost is absent. Under these defaults, long FX round
trips and crossover strategies can look materially better than
executable reality. Equity-trade bars have a distinct basis, so the
bid-based FX issue should not be generalized blindly to every
instrument. The repository correctly labels the slippage and
commission models as `none`, but the resulting bid-fill assumption
deserves explicit validation or an operator warning.

**Fix:** Add an explicit `BidAskFillPriceSource` / execution-price
model with side-sensitive pricing; use bid for sells and ask or an
explicitly labelled approximate ask for buys. Because the canonical
record contains a *bar-average* spread rather than the actual open
spread, either retain open bid/ask in a future canonical schema or
state clearly that any `Open+AvgSpread` estimate is a proxy. Add
round-trip invariants and tests for both long and short FX trades;
record the selected fill model and spread approximation in the run
manifest.

### F2 — P1: `Manager.Bars` can return success despite missing bars *inside* a declared partition span

**Evidence:** [`internal/marketdata/query.go`](https://github.com/rustyeddy/trader/blob/cc14c6c153bb9c48cf98823fde9ef2c5747f05ec/internal/marketdata/query.go#L194-L257) collects `Manifest.Span` values, then checks the union of those spans with `coverageGap`, which [only compares time ranges](https://github.com/rustyeddy/trader/blob/cc14c6c153bb9c48cf98823fde9ef2c5747f05ec/internal/marketdata/query.go#L405-L445). [`marketdata/barset.go`](https://github.com/rustyeddy/trader/blob/cc14c6c153bb9c48cf98823fde9ef2c5747f05ec/marketdata/barset.go#L53-L82) validates bar ordering and timestamps, **not** continuity. The separate [`gapsForPartition` implementation](https://github.com/rustyeddy/trader/blob/cc14c6c153bb9c48cf98823fde9ef2c5747f05ec/internal/marketdata/coverage.go#L409-L460) knows how to check expected calendar boundaries, but the bar reader does not call it.

**Impact:** A partition can claim a complete time span even when a provider candle is missing inside that span. `Bars` can then return success and [`NewReplay`](https://github.com/rustyeddy/trader/blob/cc14c6c153bb9c48cf98823fde9ef2c5747f05ec/internal/backtest/replay.go#L106-L126) treats that as evidence of completeness. The normal CLI's data-preparation/check path does separately use coverage, so this is chiefly a contract gap for consumers that call `Bars` or `Replay` directly; it must not be stated as proof that the CLI routinely accepts such gaps.

**Fix:** Centralize one canonical, read-only continuity check that
walks expected bar boundaries against actual observations without
requiring access to the raw archive. Reuse it for reader validation
and maintenance coverage, or adjust the `Bars` contract to promise
only available ordered bars and force callers needing complete history
to request completeness explicitly. Regression fixture: a valid month
manifest with a single missing H1 candle, plus an empty-but-valid
BarSet.

### F3 — P2: A dataset rebuild can change `ConfigDigest` despite unchanged effective inputs

**Evidence:** [`marketdata.Manifest.BuiltAt`](https://github.com/rustyeddy/trader/blob/cc14c6c153bb9c48cf98823fde9ef2c5747f05ec/marketdata/manifest.go#L199-L207) is deliberately *excluded* from dataset `Revision` so an identical rebuild gets the same revision. However [`backtest.Manifest.configWire`](https://github.com/rustyeddy/trader/blob/cc14c6c153bb9c48cf98823fde9ef2c5747f05ec/internal/backtest/manifest.go#L350-L365) serializes **full** `[]marketdata.Manifest` values, including `BuiltAt`; [`ConfigDigest`](https://github.com/rustyeddy/trader/blob/cc14c6c153bb9c48cf98823fde9ef2c5747f05ec/internal/backtest/manifest.go#L422-L439) hashes that serialization.

**Impact:** Two logically identical datasets rebuilt at different
timestamps can produce different backtest configuration digests,
undermining the digest's documented role as an equivalence key. This
conclusion follows from the JSON shapes even if particular tests do
not currently rebuild an unchanged dataset.

**Fix:** For the **digest only**, project datasets into deterministic
IDs/revisions and other relevant invariant fields, explicitly omitting
`BuiltAt`. Retain `BuiltAt` in the human-readable provenance
report. Test two runs with identical input manifests except `BuiltAt`
and assert equal `ConfigDigest`.

### F4 — P2: CLI/MCP execution does not populate the run's Trader version

**Evidence:** [`internal/backtest/manifest.go`](https://github.com/rustyeddy/trader/blob/cc14c6c153bb9c48cf98823fde9ef2c5747f05ec/internal/backtest/manifest.go#L29-L50) and [`internal/service/backtest/request.go`](https://github.com/rustyeddy/trader/blob/cc14c6c153bb9c48cf98823fde9ef2c5747f05ec/internal/service/backtest/request.go#L19-L46) support `TraderVersion`, and [`internal/service/backtest/run.go`](https://github.com/rustyeddy/trader/blob/cc14c6c153bb9c48cf98823fde9ef2c5747f05ec/internal/service/backtest/run.go#L44-L64) forwards it. But the shared [`cmd/internal/backtestcfg/run.go`](https://github.com/rustyeddy/trader/blob/cc14c6c153bb9c48cf98823fde9ef2c5747f05ec/cmd/internal/backtestcfg/run.go#L199-L206) request literal omits this field. [`version.Current()`](https://github.com/rustyeddy/trader/blob/cc14c6c153bb9c48cf98823fde9ef2c5747f05ec/version/version.go#L76-L110) exists.

**Impact:** Normal CLI/MCP reports cannot identify the binary version
through their declared `trader_version` field, reducing
reproducibility.

**Fix:** Supply `TraderVersion` at the CLI/MCP composition boundary,
using `version.Current().Version` (and optionally `Revision` and dirty
flag as separate structured fields). Test the value in a saved report.

### F5 — P2: Concurrent submissions can admit risk against stale snapshots

**Evidence:** [`internal/service/execution/submit.go`](https://github.com/rustyeddy/trader/blob/cc14c6c153bb9c48cf98823fde9ef2c5747f05ec/internal/service/execution/submit.go#L101-L114) snapshots account state before calling the pipeline; [`internal/pipeline/pipeline.go`](https://github.com/rustyeddy/trader/blob/cc14c6c153bb9c48cf98823fde9ef2c5747f05ec/internal/pipeline/pipeline.go#L354-L384) evaluates risk on the supplied snapshot then submits in a separate step. The [`max-open-positions` rule](https://github.com/rustyeddy/trader/blob/cc14c6c153bb9c48cf98823fde9ef2c5747f05ec/internal/risk/maxopenpositions.go#L42-L77) bases its decision on the snapshot's open position count.

**Impact (architectural risk):** Two concurrent clients may see the
same pre-order state, each pass `max_open_positions=1`, and each
submit different orders. Simulated fill-time margin checking does not
inherently enforce every logical risk rule. This is not evidence of a
race in the single-threaded backtest scheduler; it becomes important
when multiple operator/automation clients share an account.

**Fix:** Give each account one serialized admission-and-dispatch owner
or use versioned/optimistic revalidation before broker
submission. Explicitly model pending accepted orders as reserved
exposure. Add a concurrent two-intent regression test, including the
async broker case. Avoid claiming cross-process atomicity until backed
by a real coordinator/reconciliation design.

### F6 — P2: `Account.Submit` idempotency ignores payload mismatches

**Evidence:** [`internal/adapters/broker/sim/account.go`](https://github.com/rustyeddy/trader/blob/cc14c6c153bb9c48cf98823fde9ef2c5747f05ec/internal/adapters/broker/sim/account.go#L481-L503) immediately returns a cloned existing order whenever `req.OrderID` already exists, without checking whether the new request's terms differ.

**Impact:** An accidental OrderID collision with different side,
quantity, or other semantics appears to succeed while the original
order remains authoritative. This may be rare with properly generated
IDs but is a preventable contract trap.

**Fix:** Make exactly repeated requests idempotent and reject
mismatched requests under the same ID with a typed conflict
error. Verify metadata and which fields are permitted to differ
according to the idempotency contract.

### F7 — P2 future-execution blocker: bracket entry and protective stop are not atomic or async-safe

**Evidence:**
[`internal/pipeline/pipeline.go`](https://github.com/rustyeddy/trader/blob/cc14c6c153bb9c48cf98823fde9ef2c5747f05ec/internal/pipeline/pipeline.go#L387-L495)
implements a bracket using a synchronous market-entry submission, then
a fresh snapshot, then a protective-stop submission. The comments
explicitly acknowledge that an async broker can accept but not yet
fill the entry, and that stop submission can fail; it refers to
tracked issue #366.

**Impact:** Fine as a deliberately bounded simulator/backtest feature;
**not a live-safe bracket guarantee**. The code does return a hard
error rather than falsely claiming success, so this is a known and
documented limitation, not a newly discovered silent bug.

**Fix:** Preserve the present explicit limitation. Before any future
async/live execution, implement broker-native bracket/OCO where
available, or a durable order/fill state machine with compensation,
monitoring, and restart recovery. Do not add more strategy features
that assume atomic protection until this is resolved.

### F8 — P3: Repeated full event-log scans are quadratic

**Evidence:** [`internal/backtest/scheduler.go`](https://github.com/rustyeddy/trader/blob/cc14c6c153bb9c48cf98823fde9ef2c5747f05ec/internal/backtest/scheduler.go#L1070-L1141) reopens `Events(ctx, "")` from the beginning after submissions and filters by `lastBrokerSeq`. The source explicitly documents worst-case `O(n²)` behavior.

**Impact:** Not a correctness bug for hundreds of intents, but
possible research scale degradation during sweeps or high-frequency
tests.

**Fix:** Add a resumable event cursor or an incremental broker-event
drain path. Benchmark before changing the design.

### F9 — P3: The canonical partition cache still reads and hashes every file on a hit

**Evidence:** [`internal/marketdata/query.go`](https://github.com/rustyeddy/trader/blob/cc14c6c153bb9c48cf98823fde9ef2c5747f05ec/internal/marketdata/query.go#L306-L320) calls `store.version` to validate cached data. [`internal/marketdata/store_csv.go`](https://github.com/rustyeddy/trader/blob/cc14c6c153bb9c48cf98823fde9ef2c5747f05ec/internal/marketdata/store_csv.go#L852-L875) calculates the version by opening and SHA-256 hashing the entire file each time.

**Impact:** Caching prevents parse/validation but not the full disk
read, limiting its value for repeated larger queries. This maximizes
detection of out-of-band edits, so it is a deliberate
safety/performance trade-off, not a bug.

**Fix:** Profile first; if needed, use immutable revision-aware
partition names, validated content digest on publish plus a stat fast
path, or a read snapshot lifecycle. Do not quietly sacrifice
coherence.

### F10 — P3: Complexity has accumulated in redundant layers and documentation

**Examples:** [`internal/service/backtest/response.go`](https://github.com/rustyeddy/trader/blob/cc14c6c153bb9c48cf98823fde9ef2c5747f05ec/internal/service/backtest/response.go#L9-L42) mirrors the fields of `internal/backtest.Result` and explains that this is to insulate service consumers, even though both types are private runtime contracts. [`internal/marketdata/manager.go`](https://github.com/rustyeddy/trader/blob/cc14c6c153bb9c48cf98823fde9ef2c5747f05ec/internal/marketdata/manager.go#L234-L249) calls out a provider interface that has remained unused by actual operations and exists only for an old test. [`internal/backtest/scheduler.go`](https://github.com/rustyeddy/trader/blob/cc14c6c153bb9c48cf98823fde9ef2c5747f05ec/internal/backtest/scheduler.go) and [`internal/adapters/broker/sim/account.go`](https://github.com/rustyeddy/trader/blob/cc14c6c153bb9c48cf98823fde9ef2c5747f05ec/internal/adapters/broker/sim/account.go) are large orchestration/transition files with long explanatory comments, issue history, and multiple related state changes.

**Impact:** The design is comprehensible when read patiently, but
harder to navigate and change in normal engineering cycles. ADR
history is being repeated in source comments at a volume that makes
the actual invariants harder to find.

**Fix:** Preserve the architecture but trim private DTO copies with no
demonstrated semantic difference; remove known unused interfaces;
partition large orchestration functions by operation or explicit
phase; write short, current contracts in source and link to ADRs for
their historical rationale. Keep public contracts and invariant tests
especially clear. Do not simplify by abandoning exact numbers,
causality, or safe defaults.

## What I would keep

1. **Exact monetary/price arithmetic:** `num` and its fixed-point
   backing are worth the overhead in financial software.
2. **Strong instrument/listing distinction:** essential when moving
   between OANDA FX, Stooq/Alpaca equities and multiple venues.
3. **Intent → planner → risk → broker:** an excellent separation and
   shared pathway; simplify plumbing but do not collapse the roles.
4. **Deterministic scheduler:** timestamp batches, next-bar-open
   intent handling, frozen account views, delayed history append and
   controlled clock directly protect against look-ahead bias.
5. **Provenance and raw/canonical data separation:** reproducible
   datasets and auditability matter more than clever abstractions.
6. **Private runtime / public SDK boundary:** scope control around
   external strategy compatibility is sensible.
7. **Default safety and strong CI:** paper default, MCP writes opt-in,
   no live execution claim, full race test suite.

## How I would simplify Trader (without rebuilding it)

**Reduce redundant private layers, not domain concepts.** The public
`sdk`, `num`, `instrument`, `marketdata`, `indicator`, `analysis` API
plus a private runtime is fundamentally a sound approach. Avoid adding
a new request/response wrapper just because an application layer
exists; use it when the use case truly needs a different contract or
lifecycle. Prefer a single owned source of truth for data gap
validation, executable-price modelling, risk admission and fill
bookkeeping.

**Make side effects obvious.** Read-only research is a valuable
promise. Keep `Plan` and `Coverage` free of writes. Make `Sync`,
`Build`, `Submit`, `Replace`, and journal writes overtly mutating
operations. Treat the account execution owner as a true concurrency
boundary before multiple clients submit at once.

**Create a shorter architectural map.** One living high-level
dependency diagram; one page per major subsystem listing contracts and
invariants; ADRs as historical decisions. Remove stale M2 references
from current package docs and eliminate comments that restate every PR
debate. Keep the ADR archive as the decision history, but make it easy
to find *what is true today*.

**Delay infrastructure until it has consumers.** The current
`RunStore` already saves a schema-versioned report by RunID with an
atomic rename:
[`internal/service/backtest/store.go`](https://github.com/rustyeddy/trader/blob/cc14c6c153bb9c48cf98823fde9ef2c5747f05ec/internal/service/backtest/store.go#L37-L123). Start
by fixing manifest identity and adding optional resolved configuration
/ exact strategy provenance, then implement `list` / `compare` over
simple files. Only introduce a searchable SQLite index when the volume
and query requirements warrant it.

## Suggested sequence

| Phase                                | Work                                                                                                                                | Desired proof                                                                                                |
|--------------------------------------|-------------------------------------------------------------------------------------------------------------------------------------|--------------------------------------------------------------------------------------------------------------|
| **1: Research truth**                | F1 FX fill basis/spread; F2 bar continuity; F3 digest excludes `BuiltAt`                                                            | Executable-price round trips, synthetic missing-candle fixture, repeated-rebuild digest equality             |
| **2: Basic operational correctness** | F4 version capture; F6 idempotency conflict; review cross-client risk serialization F5                                              | Saved manifest identifies binary; duplicate-request conflict tests; concurrent competing-order test          |
| **3: Architecture cleanup**          | Remove dead provider seam, trim private response wrappers, break scheduler/sim methods into focused phase helpers, prune stale docs | Fewer duplicate contracts without API break or changed golden outputs                                        |
| **4: Scale when measured**           | F8 incremental event reader; F9 partition version-cache optimization; richer run artifact navigation                                | Repeatable benchmarks and baseline-relative performance improvement                                          |
| **Before non-simulated execution**   | F7 async-safe protection, durable fill reconciliation/restart handling, cross-process risk ownership                                | Fault injection: delayed fills, process crash, stop rejection, network timeout, ambiguous submission outcome |

## Suggested first GitHub issues

- **BUG: FX backtest execution prices must respect basis and side; account for spread** — P1
- **BUG: Canonical `Bars` and replay completeness must detect missing expected intervals** — P1
- **BUG: Backtest config digest changes when only canonical dataset `BuiltAt` changes** — P2
- **BUG: Populate `TraderVersion` on all CLI/MCP backtest runs** — P2
- **HARDEN: Enforce conflicting-duplicate OrderID failure in sim broker** — P2
- **DESIGN: Serialize account-level risk admission across concurrent submissions** — P2 (before multi-client paper trading)
- **CLEANUP: Remove dead Manager provider test seam and audit duplicate private DTOs** — P3
- **PERF: Resume broker journal reads rather than rescanning event history** — P3, after benchmark

**Final position:** Trader's foundations are good; no rewrite is
justified. Make the research results trustworthy first, then trim
avoidable abstraction and duplicated narrative. Trader will feel
substantially simpler when it has fewer independent decisions and less
duplicated plumbing, not merely fewer files.
