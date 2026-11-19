# Real OANDA H1 fixtures

These four files are **byte-for-byte copies of canonical-v2 files from the real archive**
(`/srv/data/oanda/canonical/<instrument>/<YYYY>/<MM>/`, built 2026-11-18 by
`trader data build` from the raw OANDA candles of `/srv/data/oanda/raw`). No bar was
edited, trimmed or manufactured; each file's `# meta` header still names the raw
partition it was built from (`raw_fingerprint`), the builder, validator and the
`scheduled-close-v1` availability policy.

| file | why this month | sha256 |
|---|---|---|
| `oanda/canonical/EURUSD/2007/01/EURUSD-2007-01-h1.csv` | OANDA has no EURUSD candle from 2007-01-08 23:00Z to 2007-01-09 20:00Z (a reviewed known provider hole, 22 bars) while GBPUSD has every one of them: 22 real sparse frames | `ddf7f99331745bf148d42c3dbc35ef9fb02632e120a8a39128c009ec8778f9d4` |
| `oanda/canonical/GBPUSD/2007/01/GBPUSD-2007-01-h1.csv` | same window | `77e94b8c961b329684d9c57f79f9885c8ae179033d9b3c1e45303804c594e935` |
| `oanda/canonical/EURUSD/2024/03/EURUSD-2024-03-h1.csv` | contains the US daylight-saving change (Sunday 2024-03-10): the weekly reopening moves from 22:00Z to 21:00Z | `5b785157a8dc5057bfef4336bd3cc5902288070fdec8f2af8a4667e67f94b162` |
| `oanda/canonical/GBPUSD/2024/03/GBPUSD-2024-03-h1.csv` | same month | `324a485ba1817333f4d11d21a74c162b94cccc87b9c30f4ea86cd41df4fa526c` |

Source `oanda`, interval `h1`, prices bid/ask/mid (the engine trades the mid), availability
persisted in each row (`available_at` = the close of the hour). To refresh, copy the same
files again from the archive and update the hashes and the pinned results in
`historical_test.go` (`TestRealData_*`).

The tests copy this tree into a temporary store root and read it through
`marketdata/service.Load`; nothing here is modified or read by path.
