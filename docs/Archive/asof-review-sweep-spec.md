# DataManager Caching, Bounded Candle API, Review Refactor, and `--asof` Sweep

**Status:** IMPLEMENTED (2026-07-11) — archived for historical reference. All
four sub-projects below landed via PRs #152 (`review-refactor` DataManager
cache), #153 (`review-refactor` bounded accessor), #154
(`service-review-refactor`), and #155 (`asof-sweep`). See
[[file:../Review.org][Review.org]]'s "Historical (implemented via
docs/archive/asof-review-sweep-spec.md)" section for where each piece landed
in shipped code. This document is kept for the reasoning behind the build
order and the no-lookahead design constraint (§4.2), not as a live spec.
**Supersedes:** earlier single-item `--asof` spec — this replaces it with four
ordered sub-projects, discovered by reading the actual `rustyeddy/trader`
source (`datamanager/`, `service/review.go`, `review/`) rather than assuming.
**Repo state referenced:** `main` branch, as cloned 2026-07-09.

---

## 0. Why this is four tasks, not one

The original ask was "add an `--asof` flag to `trader review`." Reading the
real source surfaced two things that change the shape of the work:

1. **`DataManager` has no in-memory cache today.** `store.ReadCSV()`
   (`datamanager/store.go`) opens a file and parses it fresh on every call.
   `service/review.go`'s `ensureCachedOandaCandles` / `readCachedOandaCandleTimes`
   are review's own attempt to avoid re-fetching from OANDA, but every read
   still goes through a cold `dm.Candles()` → `store.ReadCSV()` path. This is
   why a live `trader review` run is already slow across ~16-17 instruments,
   and it would make a naive `--asof` range sweep unusably slow (roughly
   `days_in_range × today's_already-slow_single_run`, since adjacent sweep
   dates mostly re-read the same monthly CSVs from disk with zero reuse).

2. **`DataManager` has no exported bounded-slice accessor.** The only public
   candle-reading method is:

   ```go
   func (dm *DataManager) Candles(ctx context.Context, req CandleRequest) (market.CandleIterator, error)
   ```

   which chains one internal `candleSet` per calendar month into a streaming
   iterator. `candleSet` (`datamanager/candle_set.go`) is intentionally
   unexported — this was a deliberate design decision (not an oversight) to
   force all callers through `DataManager.Candles()` rather than letting
   packages reach into the storage layer directly, after `DataManager`'s
   responsibilities were being circumvented in earlier code. That constraint
   stays. What's missing is a *second* public method alongside the iterator
   — one that returns a plain, compacted `[]market.Candle` for callers (like
   `review/`) that need random access (`candles[len(candles)-1]`,
   `candles[len(candles)-n:]`) rather than sequential streaming.

So the real work is: add the cache (§1), add the bounded compacted-slice
method on top of it (§2), move `review`'s fetch logic onto that method,
deleting the redundant parallel caching/top-up logic it grew on its own
(§3), and only then add `--asof` (§4), which becomes a small, cheap addition
once §1–§3 are done rather than a feature that has to solve caching and
compaction itself.

**Suggested build order: 1 → 2 → 3 → 4, strictly sequential.** Each step is
a real prerequisite for the next, not just a nice-to-have ordering.

---

## 1. DataManager in-memory candle cache

**Goal:** make repeated reads of the same `(Instrument, Source, TF, Year,
Month)` a map lookup instead of a file open + `bufio.Scanner` parse.

**Where:** `datamanager/store.go`, around `store.ReadCSV(key Key) (*candleSet, error)`.

**Design sketch:**

```go
// in store (or a new cache.go in the datamanager package)
type store struct {
    // ... existing fields ...
    mu    sync.RWMutex
    cache map[Key]*candleSet
}

func (s *store) ReadCSV(key Key) (cs *candleSet, err error) {
    s.mu.RLock()
    if cached, ok := s.cache[key]; ok {
        s.mu.RUnlock()
        return cached, nil
    }
    s.mu.RUnlock()

    cs, err = s.readCSVUncached(key) // today's ReadCSV body, renamed
    if err != nil {
        return nil, err
    }

    s.mu.Lock()
    if s.cache == nil {
        s.cache = make(map[Key]*candleSet)
    }
    s.cache[key] = cs
    s.mu.Unlock()
    return cs, nil
}
```

**Key`(Instrument, Source, TF, Year, Month)`is already exactly the right
shape** — it's the same struct used today to address monthly CSV files
(`datamanager/store_key.go`), so no new key type is needed.

**Invariants to preserve / verify:**

- `candleSet` is returned by pointer today (`newMonthlyCandleSet` returns
  `*candleSet`) and is treated as effectively immutable once built from
  disk — confirm no caller mutates a `*candleSet` in place after reading it
  (`AddCandle`/`Merge` are used during *construction*, not after handoff).
  If any caller does mutate a returned `*candleSet`, caching it directly is
  unsafe and it needs a defensive copy on cache hit, or the mutating
  caller needs to stop mutating shared state.
- The current month's data is a moving target — a cache entry for the
  in-progress month could go stale mid-session if new candles are
  downloaded into it. Decide: skip caching the current month (always read
  fresh), or add an explicit invalidation path that `ensureCachedOandaCandles`
  (or its replacement in §3) calls after a successful download. Skipping
  cache for the current month is the simpler, safer starting point.
- This is a process-lifetime, in-memory cache only — no persistence, no
  cross-process sharing, no TTL beyond "don't cache the current month."
  Keep it that simple; there's no evidence a more elaborate cache is
  needed yet.

**Acceptance check:** a test that reads the same `Key` twice, confirms the
second read doesn't touch the filesystem (e.g. via a `store` variant backed
by a counting/failing filesystem stub after the first read), and confirms
both reads return equal `candleSet` contents.

---

## 2. Bounded, compacted candle accessor on DataManager

**Goal:** a second public method alongside `Candles()` that returns a plain
`[]market.Candle`, already compacted to real trading data — not the
iterator, and not the raw calendar-indexed array.

**Critical constraint, confirmed against source:** `candleSet.Candles` is a
**dense, calendar-indexed array** — it has a slot for every timeframe step
in the month, including weekends and holidays when the market is closed,
specifically so index arithmetic (`idx := int(off / tf)`) is fast and
direct. Those closed-market slots are zero-value `market.Candle{}` and are
marked invalid in `candleSet.Valid` (a bitset). The existing iterator
(`candleSetIterator.Next()`) explicitly skips invalid slots:

```go
if market.BitIsSet(it.cs.Valid, it.idx) {
    return true
}
```

**A new accessor must reproduce this same filtering — compact to
valid-only candles — not expose the raw calendar-indexed array.** This
isn't a nice-to-have: `review/pair.go`'s `computeD1`/`computeH4` do a bare
`for _, c := range candles { adx.Update(c) }` with no validity check.
Feeding zero-value candles from closed-market slots into `ADX`, `EMA`, or
`ChoppinessIndex` would silently corrupt every indicator's warmup and
running state — wrong answers with no error. Any indicator consumer that
might use this method in the future has the same requirement: they should
never have to know or care that the underlying storage has calendar gaps.

**Design sketch:**

```go
// GetCandles returns the most recent count valid (market-session-only,
// gap-compacted) candles for instrument/tf at or before asof, sourced
// from the cached candleSet storage. Unlike Candles(), which returns a
// streaming iterator, GetCandles is for callers that need random access
// (last candle, last N candles) and are willing to materialize the full
// result eagerly.
func (dm *DataManager) GetCandles(ctx context.Context, req CandleRequest, asof time.Time, count int) ([]market.Candle, error) {
    // 1. Compute a Range wide enough to cover `count` valid candles ending
    //    at or before asof (reuse/relocate the windowing math currently
    //    duplicated in service/review.go's reviewWindow()).
    // 2. Call dm.Candles(ctx, req) to get the iterator (now cache-backed
    //    per §1, so repeated calls covering overlapping months are cheap).
    // 3. Drain the iterator (which already skips invalid slots) into a
    //    slice, filtering to candles with open time <= asof.
    // 4. Trim to the most recent `count` entries.
    // 5. Return the compacted, ordered slice.
}
```

Implementation-wise this can literally be "drain the existing iterator into
a slice" — the compaction work is already done by `candleSetIterator`; this
method's job is just to materialize it into the shape `review/` (and future
consumers) actually need, plus the `asof` cutoff and `count` trim that
`service/review.go` currently does by hand in `readCachedOandaCandleTimes`.

**Acceptance check:** a test with a `candleSet` containing a known weekend
gap, confirming `GetCandles` never returns a zero-value `market.Candle` and
never returns more than `count` entries.

---

## 3. Refactor `service/review.go` onto the new API

**Goal:** delete the redundant fetch/cache logic that grew inside
`review`'s service layer because `DataManager` didn't yet offer what it
needed, now that it does.

**What gets deleted or drastically shrunk** (`service/review.go`):

- `ensureCachedOandaCandles` — the manual "check `LastCompleteDate`, download
  the gap, write to store" dance. Whether this responsibility moves into
  `DataManager` itself (arguably where it belongs — "ensure data is
  available" is a `DataManager` job) or stays a thin wrapper in `service/`
  is a judgment call for implementation time; either way, the *duplication*
  of cache-adjacent bookkeeping should go.
- `readCachedOandaCandleTimes` — replaced by a call to `dm.GetCandles(ctx,
  req, asof, count)` from §2. The manual iterator draining and count-trim
  loop it currently does becomes `GetCandles`'s job, not review's.
- `fetchReviewCandleTimesFromOANDA` — the direct-OANDA fallback used when
  the cache "can't satisfy count." Whether this fallback still needs to
  exist as review-level logic, or whether `GetCandles`/`DataManager` should
  own the "top up from OANDA if the local store is short" responsibility
  end-to-end, is worth deciding during implementation — but it should not
  remain a second, independent implementation of "get me candles, fetching
  if necessary."
- `reviewWindow` — the granularity-to-duration windowing math. If §2's
  `GetCandles` takes over window-sizing internally (recommended — it's
  generic "how much calendar time do I need for N candles at this
  granularity" logic, not review-specific), this can move into
  `datamanager` and be deleted from `service/review.go`.

**What stays unchanged:** `review.ReviewPair` and everything in `pair.go`/
`classify.go`/`review.go` (the `review/` package) — these are pure
functions taking candle slices and must remain so. This refactor only
touches how `service/review.go` *obtains* those slices, not what it does
with them.

**Acceptance check:** existing `service/review_test.go` and
`api/rest/review_test.go` continue passing unmodified (behavior, not
implementation, should be preserved) — plus a new test confirming a second
`ReviewWatchlist` call for the same instruments in the same process doesn't
re-read CSV files that the first call already loaded (validates §1's cache
is actually being exercised end-to-end, not just unit-tested in isolation).

---

## 4. `trader review --asof` sweep

Now that §1–§3 exist, this is close to what the original design assumed —
but explicitly restated here since scope and semantics matter.

### 4.1 Explicit scope boundary

**This replays the classification layer only** — `Bucket`/`Bias`/`Notes` as
they would have appeared at a past date. It does **not** simulate trade
entries, stops, or P&L. Actually backtesting the *trading outcomes* of
historical Tradeable signals would require a `Strategy` mirroring
`Classify()`'s logic running through the existing bar-by-bar `Backtest`
engine (`backtest.go`, `backtest_run.go` et al.) — a materially larger,
separate effort, not assumed here.

### 4.2 No-lookahead correctness — highest risk item

Does `--asof 2026-06-15` mean "as if it were mid-day June 15, seeing only
candles complete before that wall-clock moment," or "using the closed June
15 D1 candle as the most recent bar"? These are different, and a D1 candle
whose date is `<= asof` in stored historical data was **not necessarily
closed at that historical moment** — at 9am NY on June 15, that candle was
still in progress. If `GetCandles` (§2) naively includes "candle whose open
time is `<= asof`" rather than "candle that was actually closed as of that
wall-clock moment," every historical scan gets a one-bar lookahead bias on
every timeframe, worst on W1/D1 where one bar is a lot of price action.

Recommend making this explicit in the request type rather than an implicit
convention:

```go
type ReviewRangeRequest struct {
    Instruments []string
    From, To    time.Time
    Interval    time.Duration // e.g. 24h for daily replay

    // ClosedBarsOnly, always true for the initial implementation: each
    // sweep step only ever sees candles fully closed before the step's
    // timestamp. This is the only mode that avoids lookahead bias.
    // (Decided 2026-07-09: starting closed-bars-only; no opt-out planned
    // for v1.)
}
```

Write a test that deliberately checks this: pick a known date, assert
`GetCandles(..., asof, ...)` never returns a candle whose period extends
past `asof`.

### 4.3 Open sub-questions

1. **Output shape.** A single-date `--asof` call fits the existing
   tabwriter table. A *range* sweep needs a `Date` dimension — `--output
   csv` (recommended, most useful for the future grading/scoring TODO) or
   an org table with a `Date` column.
2. **Per-instrument date handling.** Treat each instrument independently —
   skip and log rather than fail the whole batch, since a pair added to the
   watchlist more recently than `From`, or with a data gap, shouldn't block
   the rest of the sweep.
3. **Warmup-insufficient dates.** Skip with a logged gap rather than fail
   fast, consistent with (2).

### 4.4 Suggested implementation steps

1. Add `ReviewRangeRequest` per §4.2.
2. Add a sweep function looping `From..To` by `Interval`, calling
   `dm.GetCandles(ctx, req, stepTime, count)` per instrument/timeframe at
   each step (cache-backed per §1, so adjacent steps sharing a month are
   cheap), then calling the existing, unmodified `review.ReviewPair`.
3. Wire `--asof` (single date) and `--from/--to/--interval` (range) CLI
   flags, output format per §4.3.1.
4. Spot-check output against known chart history for 2-3 pairs (e.g.
   GBPAUD, AUDUSD) — manual correctness check before trusting the sweep
   for later threshold-tuning work.

### 4.5 Explicitly out of scope

- Trade simulation / P&L (§4.1)
- The 0–100 grading/composite score (separate TODO item — depends on this
  item's output but is a separate build)
- Config-driven thresholds (separate TODO item — independent, can land
  before or after this)
