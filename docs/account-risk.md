# Account Risk & Capital Management

This document explains how Trader sizes positions and limits risk at the
**account** level. It covers the vocabulary professional desks use, the
constraints each market type imposes, and — for every layer — what Trader
implements today, what milestone **Account Margin Admission** (umbrella #409)
adds, and what remains future work.

It is a primer and roadmap, not a design record. Binding decisions live in the
ADRs (`docs/arch/adr-decisions.org`); where this document and an ADR disagree,
the ADR wins. In particular:

- **ADR-004** — prices, quantities, money, and margin are exact `num` types,
  never `float64`.
- **ADR-006** — sizing (`risk.Sizer`) and admission (`risk.Engine`) are
  separate stages.
- **ADR-029** — risk decisions are strict approve/reject. A rule never
  resizes or otherwise mutates a proposal.
- **Architecture: live guards vs. trade risk** — halts, kill switches,
  flattening, and connection/staleness interlocks are operational guards owned
  by the future `live` package, not risk rules.

Status markers used below:

- **[today]** — implemented in this repository now.
- **[#NNN]** — planned in the Account Margin Admission milestone.
- **[future]** — recognized need, not yet scheduled.

---

## 1. Diagnosis: why a backtest bought 4.6× equity

The prompt for this document was an equity backtest in which $10,000 of
equity opened 100 SPY shares at about $463.08 — $46,308 notional, roughly
**4.6× equity** — on what was meant to be an unlevered account.

Fixed-fractional sizing ("risk 1% per trade") computes size from the adverse
distance to the stop:

```
risk budget = equity × risk_fraction
quantity    = risk budget / adverse_distance      (rounded down, ADR-030)
```

That quantity is *unbounded*: as the stop tightens, size grows without limit.

| Stock price | Stop distance | Risk (1% of $10k) | Units | Notional | × equity |
|-------------|---------------|-------------------|-------|----------|----------|
| $100        | $5.00         | $100              | 20    | $2,000   | 0.2×     |
| $100        | $0.50         | $100              | 200   | $20,000  | **2.0×** |
| $463        | ~$1.00        | $100              | 100   | $46,308  | **4.6×** |

The risk-per-trade math was correct: each trade risked 1% to its stop. What
was missing is any check that the account could **finance** the position. In
this codebase specifically:

1. `risk.NewFixedFractionSizer` answers "how much am I willing to lose?" only.
   It never consults buying power.
2. The backtest composition root (`cmd/trader/backtest/service.go`) builds
   `risk.NewEngine()` with **no rules**, so even the per-position
   `MaxPositionLeverageRule` never ran.
3. The simulated broker (`internal/adapters/broker/sim`) has no margin policy:
   `BuyingPower` and `MarginAvailable` mirror cash, `MarginUsed` is always
   zero, and cash may go negative. This is a documented M3 placeholder, not a
   leverage model.

The backtest scheduler is single-threaded and deterministic, so concurrent
over-subscription was *not* a cause.

**The fix is structural:** risk-per-trade says how much you *want*; a separate
account-level admission check decides whether the account *allows* it. In
Trader that check rejects an over-limit order outright rather than shrinking it
— see §3 for why, and #409 for the milestone.

---

## 2. Vocabulary

| Term                              | Meaning                                                                                                                                            | In Trader                                                                                                                                                   |
|-----------------------------------|----------------------------------------------------------------------------------------------------------------------------------------------------|-------------------------------------------------------------------------------------------------------------------------------------------------------------|
| **Equity / NAV**                  | Cash + unrealized P&L of open positions. The base for all percentages.                                                                             | `account.Snapshot.Equity()`                                                                                                                                 |
| **Balance**                       | Cash only. Don't size off this when positions are open.                                                                                            | `Snapshot.CashBalances()`                                                                                                                                   |
| **Notional (exposure)**           | Units × price × multiplier, in account currency.                                                                                                   | per-position rules value the proposal at `Input.ReferencePrice`; the account-wide aggregate values other open positions at their current marks [#411, #412] |
| **Gross exposure**                | Σ \|notional\| across positions. Longs and shorts both add.                                                                                        | [#411]                                                                                                                                                      |
| **Net exposure**                  | Σ signed notional. Longs minus shorts.                                                                                                             | [future] (`internal/portfolio` groups positions per instrument across accounts, without valuing them)                                                       |
| **Leverage**                      | Gross exposure ÷ equity.                                                                                                                           | per-position today; account-wide [#413]                                                                                                                     |
| **Buying power**                  | Funds available to open new positions — a money amount, not a notional; how much notional it supports depends on the multiplier and margin policy. | `Snapshot.BuyingPower()` — placeholder in sim until [#412]                                                                                                  |
| **Initial margin**                | Collateral required to *open* exposure.                                                                                                            | `initial_margin_ratio` [#414]                                                                                                                               |
| **Maintenance margin**            | Collateral required to *keep* exposure open; breach triggers margin call / liquidation.                                                            | [future] — v1 non-goal                                                                                                                                      |
| **Margin utilization**            | Margin used ÷ equity.                                                                                                                              | `Snapshot.MarginUsed()` [#412]                                                                                                                              |
| **Margin closeout**               | Broker force-closes positions below a margin threshold.                                                                                            | [future]                                                                                                                                                    |
| **Trade risk (R)**                | Loss if the stop is hit: units × \|entry − stop\| (+ costs).                                                                                       | `PerTradeLossRule` [today]                                                                                                                                  |
| **Portfolio heat**                | Σ R across all open trades.                                                                                                                        | [future]                                                                                                                                                    |
| **High-water mark (HWM)**         | Highest equity reached; drawdown is measured from it.                                                                                              | backtest metrics [today]; as a gate [future]                                                                                                                |
| **Drawdown**                      | (HWM − equity) ÷ HWM.                                                                                                                              | backtest metrics [today]                                                                                                                                    |
| **Circuit breaker / kill switch** | Halts new entries (or flattens) on a loss threshold or operator action.                                                                            | future `live` guard, not a risk rule                                                                                                                        |
| **Correlation bucket**            | Positions that behave as one bet (e.g. all USD-short pairs).                                                                                       | [future]                                                                                                                                                    |
| **Gap risk**                      | Loss beyond the stop when price jumps past it.                                                                                                     | modeled by sim gap fills (ADR-026); not in sizing                                                                                                           |
| **Pre-trade risk check**          | Gate every proposal passes before submission.                                                                                                      | `risk.Engine` — approve/reject only                                                                                                                         |

The two numbers most often confused are **heat** (risk to stops) and
**exposure** (notional). A robust system limits both. Heat without an exposure
cap is exactly the 4.6× failure; exposure without heat lets wide-stop trades
quietly risk too much.

---

## 3. The layered model, mapped to Trader's pipeline

Trader's order path (ADR-005, ADR-006, ADR-031, ADR-032) is:

```
strategy intent
  │
  ▼
risk.Sizer            ── chooses quantity (Layer 1, and optionally Layer 2 capping)
  │
  ▼
execution.Planner     ── intent → concrete order.Proposal
  │
  ▼
risk.Engine           ── every configured risk.Rule; approve or reject (Layers 2–4)
  │
  ▼
broker.Account        ── sim (backtest) / alpaca (paper) / oanda (future)
  │
  ▼
journal               ── intents, proposals, risk decisions, orders, fills

  ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─
live session guards   ── halt / pause / kill switch / flatten (Layers 5–6, future)
```

**Reject, don't resize.** A risk rule never changes a proposal's quantity
(ADR-029). If an order would breach a limit, it is rejected and the journal
records which rule and why (`risk.Violation{Rule, Message, Measured, Limit}`).
Quantity is decided only in the `Sizer`, which is an explicit, configured,
manifest-recorded choice. A *capping* sizer — e.g. `min(risk-budget size,
buying-power size)` that records which constraint bound — is legitimate sizing
rather than silent resizing; whether to add one is an open question in #410 /
#417.

Every position/exposure-limit rule — the rules ADR-034 covers, and the
planned account initial-margin rule, which follows the same principle — must
always admit a de-risking proposal, so an account that is already over a limit
is never trapped. This is not a requirement on every conceivable rule: a
future stateful or operational rule may legitimately reject a de-risking
proposal.

### Layer 1 — Sizing intent  (`risk.Sizer`)

- **[today]** `NewFixedFractionSizer`: equity × risk fraction ÷ stop distance,
  rounded down to the listing's quantity increment (ADR-030).
- **[today]** `NewFullNotionalSizer` (ADR-061): invests
  `min(Equity, BuyingPower)` at the reference price.
- **[future]** Minimum stop distance (e.g. ≥ 0.5 × ATR) — stops tighter than
  noise are the direct route to oversized positions.
- **[future]** Gap-adjusted risk: `max(stop distance, gapFactor × ATR)` for
  gap-prone instruments. A stop is an order, not a guarantee.
- **[future]** Costs and expected slippage in per-unit risk.
- For FX, per-unit risk must be in account currency (pip value). Today both
  sizers and the rules that compute monetary values (`PerTradeLossRule`,
  `MaxInstrumentExposureRule`, `MaxPositionLeverageRule`) reject a listing
  whose settlement currency differs from the account currency rather than
  converting. The count/quantity rules (`MaxPositionQuantityRule`,
  `MaxOpenPositionsRule`) don't inspect currency and can admit such a
  proposal.

### Layer 2 — Account financing  (initial margin)

The missing layer behind the 4.6× run.

```
prospective gross notional × initial_margin_ratio ≤ equity
```

v1 applies the single account-level ratio to each position's notional and sums
the results. The aggregate is defined per position (Σ notionalᵢ × rateᵢ) so a
later per-listing or per-instrument override — an OANDA `marginRate`, or a
futures dollars-per-contract margin — changes only that per-position input,
not the aggregate calculation or the meaning of the account setting (#411).

- **[#410]** ADR-066 settles terminology, valuation basis, fill-time
  semantics, and non-goals.
- **[#411]** One exact calculation of gross notional and required margin,
  shared by risk and the simulator.
- **[#412]** Sim snapshots expose per-position marks and margin-aware
  `BuyingPower` / `MarginUsed` / `MarginAvailable`.
- **[#413]** `account_initial_margin` risk rule, evaluated on the *resulting*
  position (a 100-long → 100-short reversal is 100 short, not 200).
- **[#414]** `backtest.initial_margin_ratio` (default 1.0), installed for
  built-in and external strategies.
- **[#415]** The simulator re-checks at the actual fill price, so a gap or
  slippage cannot create an over-limit position.

Note: `initial_margin_ratio: 1.0` means "unlevered", **not** "cash account" —
it still permits shorting up to equity unless #410 adds an explicit shorting
restriction.

### Layer 3 — Position limits  (`risk.Rule`, [today])

- `MaxPositionQuantityRule` — cap on resulting units in one instrument.
- `MaxInstrumentExposureRule` — cap on resulting notional in one instrument.
- `MaxPositionLeverageRule` — one position's notional ÷ leverage ≤ equity.
  Per-position only; it does not bound the account (ADR-034).
- Quantity rounding is always *down* to the listing increment; a sizer result
  that rounds to zero produces no order rather than rounding up.
- **[future]** Absolute fat-finger `maxOrderNotional`; % of average daily
  volume for illiquid names.

These rules exist but are not yet configurable from the backtest config; #414
introduces config-driven engine composition.

### Layer 4 — Portfolio limits

- **[today]** `MaxOpenPositionsRule`.
- **[#413]** Account-wide gross leverage via initial margin.
- **[future]** Portfolio heat (Σ R).
- **[future]** Correlation buckets; for FX, decompose each pair into
  per-currency exposure (long EURUSD = +EUR, −USD) and cap net exposure per
  currency.

### Layer 5 — Account state gates

Two different kinds of thing, owned by different packages:

- **Stateful risk rules** [future] — daily/weekly loss limits, a drawdown
  ladder that tightens sizing (e.g. halve size below 5% drawdown). The
  architecture's traceability table assigns loss limits to "stateful risk
  supervision". Today's rules are stateless functions of one snapshot, so
  these need a design (and an ADR) for where the state lives and how it is
  recorded for determinism.
- **Live guards** [future, `live` package, M6/M8] — halt, pause, kill switch,
  flatten. Design rules worth keeping:
  - Halts don't auto-rearm; re-enabling is an explicit, journaled operator
    action.
  - Measure intraday loss on equity (mark-to-market), not balance.
  - Define the session boundary (e.g. 17:00 America/New_York for FX) once —
    Trader's calendars already do (ADR-012, ADR-049).

### Layer 6 — Post-trade monitoring  [future]

Maintenance margin, distance to broker closeout, and self-reduction before the
broker liquidates. Explicit v1 non-goals of #409; a later issue can add
`maintenance_margin_ratio` and a liquidation policy.

---

## 4. Account models by market type

Margin rules are properties of the venue and instrument, not of Trader.
v1 uses a single account-level ratio; per-instrument rates are a planned
extension (#410, #411).

### Cash equities (no margin)

- Buying power = settled cash − cash committed to pending orders.
- Leverage ≤ 1.0 and **no shorting**. Trader's ratio 1.0 covers the first but
  not the second (see Layer 2).
- **Settlement:** US equities settle T+1. Rebuying with unsettled proceeds can
  cause good-faith violations in a real cash account. Trader's simulator does
  not model settlement [future]; frequent-trading equity backtests should note
  the simplification.

### Margin equities (Reg T)

- Initial margin 50% (`initial_margin_ratio: 0.5`); maintenance ≥ 25% per
  FINRA, often 30%+ at the broker, higher for volatile names.
- Pattern-day-trader rules have imposed a $25k minimum for frequent intraday
  round trips; FINRA has been revising this — **verify the current rule before
  modeling it**.
- Trader status: Alpaca paper broker adapter (ADR-051) and Stooq/Alpaca
  historical data (ADR-047–053). Broker-side Reg-T replication is a non-goal.

### Forex (OANDA)

- Margin rate is per instrument; in the US, NFA limits are 50:1 on majors (2%)
  and 20:1 on others (5%). Read the instrument's actual `marginRate` rather
  than hardcoding.
- OANDA closes out when margin closeout percent reaches 100% (equity at half
  of margin used). Modeling this is Layer 6 work [future].
- Continuous session with **weekend gaps** — Friday close to Sunday open is
  the main gap risk.
- Financing (swap/rollover) accrues daily; `account.Snapshot.Financing()`
  exists but the simulator does not accrue it yet [future].
- Trader status: OANDA historical data today; broker adapter is M6.

### Futures

- Margin is fixed dollars per contract, not a ratio. Integer contract rounding
  dominates at small account sizes — often "the account is too small for this
  contract".
- Daily mark-to-market settles P&L to cash; rolls and expirations must be
  handled.
- Trader status: `instrument.KindFuture` / `KindContinuousSeries` exist
  (ADR-003); no futures data, broker, or margin model yet.

### Crypto

- **Spot** behaves like cash equities, 24/7, no settlement delay.
- **Perpetuals/margin** have per-position maintenance margin, a liquidation
  price, and periodic funding — a separate account model.
- Trader status: not supported.

---

## 5. Parameters

Illustrative defaults are conservative swing-trading starting points, not
recommendations. Percentages are of equity unless noted.

### Implemented as risk rules  [today]

Available as constructors in `internal/risk`; not yet exposed in backtest
config (config-driven composition arrives with #414).

| Rule                        | Parameter                  | Notes                                                         |
|-----------------------------|----------------------------|---------------------------------------------------------------|
| `PerTradeLossRule`          | risk fraction (`num.Rate`) | Independently re-derives planned loss; never trusts the sizer |
| `MaxPositionQuantityRule`   | max units (`num.Quantity`) | Per instrument                                                |
| `MaxInstrumentExposureRule` | max notional (`num.Money`) | Per instrument, valued at `ReferencePrice`                    |
| `MaxPositionLeverageRule`   | max leverage (`num.Rate`)  | Per position only                                             |
| `MaxOpenPositionsRule`      | max count                  | Blocks only count increases                                   |

### Planned  [#409 milestone]

| Parameter                       | Default     | Notes                                   |
|---------------------------------|-------------|-----------------------------------------|
| `backtest.initial_margin_ratio` | 1.0         | Unlevered; 0.5 = 2×, 0.25 = 4× gross    |
| full-notional sizer headroom    | TBD in #417 | Avoid fill-time rejection on small gaps |

### Future candidates

| Parameter                                  | Illustrative    | Kind                       |
|--------------------------------------------|-----------------|----------------------------|
| `minStopATR`                               | 0.5             | sizer                      |
| `gapFactorATR`                             | 1.0             | sizer                      |
| `maxOrderNotional`                         | absolute $      | rule                       |
| `maxPortfolioHeat`                         | 4%              | rule                       |
| `maxBucketHeat`                            | 1.5%            | rule                       |
| `maxCurrencyNetExposure`                   | 2× equity       | rule (FX)                  |
| `maintenance_margin_ratio`                 | venue-specific  | sim / live                 |
| `dailyLossLimitPct` / `weeklyLossLimitPct` | 2% / 4%         | stateful risk              |
| drawdown ladder (caution / halt)           | 5% → ×0.5 / 10% | stateful risk / live guard |
| `maxOrdersPerMinute`                       | small integer   | live guard                 |
| `maxStalePriceAge`                         | e.g. 30s        | live guard                 |

Keep the tuned set small. Most limits should have defaults that almost never
change; the ones actually tuned tend to be the risk fraction, portfolio heat,
and drawdown thresholds.

Fee, spread, and slippage realism already exists as configurable simulator
models (ADR-028).

---

## 6. Further considerations

- **Stops aren't a maximum loss.** Gap risk makes heat an under-estimate. A
  stress figure ("loss if every position gaps 2 ATR against") is a useful
  future report metric.
- **Correlation spikes in stress.** Conservative fixed buckets beat estimated
  correlation matrices.
- **Pending orders consume buying power.** In the simulator v1 relies on the
  fill-time check (#415). A live session needs reservation on approval and
  release on cancel/reject, or must defer to the broker's margin engine —
  deferred to live-trading work (#410 records this).
- **Stale data.** Sizing off a stale price is a fat-finger in disguise — a
  live guard.
- **Account currency.** Every P&L, risk, and margin number must be in account
  currency. Today the sizers and money-valuing rules reject cross-currency
  input rather than converting (count/quantity rules don't check currency); `num.Money.Convert` exists for portfolio aggregation (ADR-019).
- **Reconciliation (live).** Broker state is authoritative; compare and halt
  on mismatch (architecture invariant 3, M6).
- **Audit trail.** Every rejection is journaled as a risk decision with its
  violations. Reports gain margin rejection counts and peak gross leverage in
  #416.

---

## 7. Where it lives in Trader

The central rule: **backtest and live run the same risk code.** Both go
through `pipeline.Pipeline` → `risk.Sizer` → `execution.Planner` →
`risk.Engine` → `broker.Account` (ADR-031/032). Only the composition root and
the broker adapter differ. If backtest admission differs from live admission,
the backtest measures a strategy you won't run.

| Concern                          | Location                                                                 |
|----------------------------------|--------------------------------------------------------------------------|
| Sizing                           | `internal/risk/sizer.go` (`Sizer`, fixed-fraction, full-notional)        |
| Admission rules                  | `internal/risk/*.go` (`Rule`, `Engine`, `Decision`, `Violation`)         |
| Resulting-position math          | `internal/risk/exposure.go` (`resultingPosition`)                        |
| Shared margin calculation        | [#411] small package importable by both `risk` and `sim`                 |
| Account state                    | `internal/account` (`Snapshot`: equity, buying power, margin, positions) |
| Cross-account view               | `internal/portfolio`                                                     |
| Simulated margin and fill checks | `internal/adapters/broker/sim` [#412, #415]                              |
| Backtest composition and config  | `cmd/trader/backtest` [#414]                                             |
| Journal of decisions             | `internal/journal` (`KindDecision`, …)                                   |
| Reports                          | `internal/report` [#416]                                                 |
| Live guards                      | future `live` package (M6/M8)                                            |

Design constraints for any new account-risk code:

- Exact numeric types throughout (ADR-004); `float64` only for analytical
  inputs such as ATR, converted at the boundary (ADR-045).
- Rules are pure functions of `risk.Input` (proposal, account snapshot,
  reference price, adverse distance). A rule that needs more information
  gets it through `Input` or the snapshot, not by reaching into a broker.
- Rules value the entire resulting position at one consistent price basis —
  never a blend of `ReferencePrice` and a position's historical `AvgPrice`
  (#183).
- Anything that changes simulated results is part of the manifest and
  `ConfigDigest` so runs stay reproducible (ADR-028, ADR-041).
- Backtests are single-threaded and deterministic. Concurrency and
  reservation concerns belong to live orchestration, which serializes
  approval per account.

---

## 8. Tests that would have caught the bug

| #   | Invariant                                                                                                                                                                                                                                                                                                      | Status                                                  |
|-----|----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|---------------------------------------------------------|
| 1   | At ratio 1.0, every fill that increases gross exposure leaves gross notional (valued at the fill price, other positions at their admission-time marks) × ratio ≤ the equity used to admit it. Later over-limit readings caused only by post-entry mark drift are not violations (no maintenance margin in v1). | [#414]                                                  |
| 2   | Σ R ≤ maxPortfolioHeat × equity at every bar                                                                                                                                                                                                                                                                   | [future]                                                |
| 3   | Multiple simultaneous signals never produce combined notional > buying power                                                                                                                                                                                                                                   | [#413] multi-instrument test; live concurrency [future] |
| 4   | A tight-stop signal on a $10k account at ratio 1.0 is **rejected** with rule `account_initial_margin` and its measured/limit values journaled                                                                                                                                                                  | [#413, #414]                                            |
| 5   | FX positions risk the configured amount in account currency                                                                                                                                                                                                                                                    | [future]; today cross-currency input is rejected        |
| 6   | Drawdown ladder applies its multiplier, then halts; recovery doesn't rearm without explicit action                                                                                                                                                                                                             | [future]                                                |
| 7   | Backtest closeout matches OANDA's closeout rule                                                                                                                                                                                                                                                                | [future]                                                |

---

## 9. Sequencing

Milestone **Account Margin Admission** (#409):

1. #410 — ADR-066: initial-margin admission design.
2. #411 — shared gross-notional / margin calculation.
3. #412 — sim per-position marks and margin-aware buying power.
4. #413 — account-level initial-margin risk rule.
5. #414 — `initial_margin_ratio` config and risk-engine composition.
6. #415 — sim fill-time enforcement (parallel with 4–5 after 3).
7. #416 — report and journal observability.
8. #417 — equity baselines and sizing guidance.

After that, in rough priority order:

- Config-driven composition of the other existing rules.
- Minimum stop distance / gap-adjusted sizing.
- Portfolio heat and correlation/currency buckets.
- Stateful loss limits and drawdown ladder (needs an ADR).
- Maintenance margin, closeout simulation, and financing accrual.
- Live guards and pending-order reservation with the `live` package.
- Futures and crypto account models when those venues arrive.
