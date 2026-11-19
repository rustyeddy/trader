# Real-data replay: OANDA H1, two instruments, one account (#689)

Real canonical bars, read through the public market-data read path, replayed by the
existing engine. Nothing here is a new loader or a new interface.

```text
canonical-v2 files --marketdata/service.Load--> marketdata.Series (EURUSD, GBPUSD)
                   --trader.RunSeries / backtest.runSeriesFrom--> mergedBars -> frames -> EMAX
```

## What is proven, and where

| claim | test (root package, `historical_test.go`) |
|---|---|
| the public read path yields real EURUSD and GBPUSD H1 bars with source, interval, a loaded month, no unexplained gap, persisted `available_at` | `TestRealData_PublicReadPathYieldsCanonicalBarsWithTheirProvenance` |
| frames group by the **persisted** `AvailableAt`, one per instant, ascending, one bar per instrument in instrument order, every bar delivered once, none added, none still open | `TestRealData_FramesGroupByPersistedAvailabilityInInstrumentOrder` |
| a missing instrument is absent from its frames, not filled: 22 real sparse frames (EURUSD hole, 2007-01-08 23:00Z to 2007-01-09 20:00Z) | `TestRealData_AMissingInstrumentIsAbsentFromItsFramesNotFilled` |
| the strategy sees a bar only once it has closed; nothing is carried forward (delivered counts equal the series' counts) | `TestRealData_TheStrategySeesEachBarOnlyOnceItHasClosedAndNothingCarriedForward` |
| the daylight-saving change (2024-03-10) moves the reopening from 22:00Z to 21:00Z and frames follow the stored availability, not a recomputed one | `TestRealData_ReplayAcrossTheDaylightSavingChangeFollowsPersistedAvailability` |
| a real-data EMAX backtest is deterministic (repeat run, other series order) and pinned to the cent | `TestRealData_RunSeriesIsDeterministicWhateverTheSeriesOrder` |
| the ATR study runs over the same public series | `TestRealData_ATRStudyReadsTheSamePublicSeries` |

The mutation checks that these tests catch: reversing the instrument tie order, and
stamping a frame with a later instant than its bars' `AvailableAt`.

**ATR study status:** `runATRStudy` is fed the same loaded series in the test above, so
that integration is proven at the library level. It has no command line or service
entry point yet, so there is no archive smoke for it; that is a follow-up if wanted.

## Run

CI and local, no archive, credential or network (the fixtures are in `testdata/data`,
provenance in `testdata/data/PROVENANCE.md`):

```sh
go test . -run TestRealData -v
```

Optional smoke against the real archive (reads only; needs `/srv/data` or `--data-root`).
The command is the ordinary `trader backtest`, which goes through `service.RunBacktest`:

```sh
trader backtest --data-root /srv/data --instrument EURUSD,GBPUSD --interval h1 \
  --from 2024-03-01 --to 2024-04-01 2>/dev/null
```

Results (2026-11-18):

```text
fixture (no February, so no warm-up):  --data-root testdata/data
oanda EURUSD,GBPUSD h1 [2024-03-01, 2024-04-01): 1010 bars
  orders: submitted 636, filled 423, rejected 0, cancelled 211, pending 2
  trades: closed 211, wins 60, losses 150, break-even 1
  money: realized -$97.80, unrealized $9.49, balance $9902.20, equity $9911.69

archive (February 2024 loads and prefills the indicators, untraded):
  orders: submitted 636, filled 423, rejected 0, cancelled 211, pending 2
  trades: closed 211, wins 62, losses 148, break-even 1
  money: realized -$87.98, unrealized $9.50, balance $9912.02, equity $9921.52
```

The two differ only because the command prefills the indicators from the month before
`--from` when it is in the data root (`warmupBars`), and the fixture holds March alone. The
test run has no warm-up, so it matches the fixture line. The January 2007 window gives
1063 bars, 593 submitted orders, balance $10352.07 on the fixture.

Credential-dependent steps (downloading from OANDA) are not part of any of this.
