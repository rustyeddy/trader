# Market-data REST API

A read-only HTTP API over Trader's canonical historical bars (#680), for programs
such as `investor` that need the data without reading Trader's files, importing its
packages or talking to a provider. It is a transport only: the same
`service.ReadBars` the rest of Trader uses reads the canonical files, and nothing is
fetched from a provider when you call it. Getting new history into Trader stays a
separate, explicit workflow (`trader data download`, then `build`).

```sh
trader serve                                  # listens on 127.0.0.1:8080
trader serve --listen 127.0.0.1:9000 --data-root /srv/data
```

## Request

`GET /v1/marketdata/bars`

| Parameter | Required | Meaning |
|---|---|---|
| `instrument` | yes | `EURUSD`; case-insensitive |
| `interval` | yes | `h1` or `d1` |
| `from` | yes | start, **inclusive**, by the bar's *opening* time. RFC 3339 (any offset is converted to UTC) or a `YYYY-MM-DD` UTC date |
| `to` | yes | end, **exclusive**, by opening time; same formats |
| `source` | no | where the bars come from: `oanda` (default) or `stooq`, which has only `SPY` and `QQQ`, `d1` (see [Trade-price data](#trade-price-data)) |
| `limit` | no | page size, 1 to 10000, default 1000 |
| `cursor` | no | `next_cursor` of the previous page; keep every other parameter (except `limit`) the same |
| `as_of` | no | RFC 3339; keep only the bars that were available at that instant (see Look-ahead) |
| `partial` | no | `true` (default) serves what exists and describes what is missing; `false` is strict |

An unknown or repeated parameter is a `400`, so a typo is never silently ignored.

## Response

```json
{
  "source": "oanda",
  "instrument": "EURUSD",
  "interval": "h1",
  "from": "2024-05-20T00:00:00Z",
  "to": "2024-05-21T00:00:00Z",
  "bars": [
    {
      "time": "2024-05-20T00:00:00Z",
      "available_at": "2024-05-20T01:00:00Z",
      "volume": 1724,
      "bid":  {"open": "1.08719", "high": "1.08765", "low": "1.08683", "close": "1.08744"},
      "ask":  {"open": "1.08734", "high": "1.08780", "low": "1.08698", "close": "1.08760"},
      "mid":  {"open": "1.087265", "high": "1.087725", "low": "1.086905", "close": "1.08752"}
    }
  ],
  "next_cursor": "eyJ2IjoxLC...",
  "coverage": {
    "complete": true,
    "summary": {"months": 1, "loaded": 1, "bad": 0, "missing": 0, "stale": 0,
                "gaps_unexplained": 0, "gaps_known_hole": 0, "gaps_known_closure": 0},
    "months": [],
    "gaps": []
  }
}
```

- **Times** are RFC 3339 UTC with a `Z`. **Prices** are exact decimal strings (never
  floating point); a mid between two ticks can have a sixth decimal. A price basis a bar
  does not have (`bid`, `ask`, `mid`, `trade`) is left out.
- **`time` is when the bar opens; `available_at` is the earliest instant you may use the
  completed bar.** A forming candle is never served as a bar.
- Bars are ordered by `time` ascending, with no duplicates. `next_cursor` is absent on
  the last page. Treat the cursor as an opaque string.
- Nothing provider-specific appears: no credentials, no provider records, no provider
  identifiers beyond the plain source name.

## Trade-price data

`source=stooq` serves SPY and QQQ daily **trade** bars (ADR-095; [how to build them](stooq-daily.md)).
Each bar has `trade` and no `bid`, `ask` or `mid`. `time` is 09:30 New York and
`available_at` the scheduled close, 16:00 or 13:00 on an early close. `volume` counts
shares. The response also carries:

```json
"data": {
  "price_basis": "trade", "currency": "USD", "volume_unit": "shares",
  "adjustment": "total-return", "adjustment_evidence": "observed",
  "availability_policy": "us-equity-close-v1", "calendar": "us-equity-calendar-v2",
  "snapshot_fingerprint": "sha256:54b6...ee6b", "snapshot_as_of": "2026-09-10"
}
```

The prices are dividend-adjusted: they are not what traded on the day and are rewritten
by every later dividend. A series is one snapshot. Check that every page has the same
`snapshot_fingerprint`; a rebuild between two requests changes it. Weekends and listed
exchange closures are not gaps; any other missing session is an `unexplained` gap.
`data` is absent for `oanda`.

## Pages never repeat or skip

Follow `next_cursor` until it is absent. The same request always returns the same
bars, and the pages together are exactly the whole range. A cursor only works with the
query it came from (`instrument`, `interval`, `from`, `to`, `as_of`, `partial`;
`limit` may change); anything else is a `400`.

## Coverage: missing data is never hidden

`partial=true` is the default, so a request is answered with the data that exists
**and says what does not**. `coverage` tells you:

- `complete`: `true` when no month is bad, missing or stale and no bars are missing that
  nothing explains. It describes the range **from this page's start to `to`**: the first
  page describes the whole range, a later page only what remains.
- `summary`: counts of months by state and of gaps by kind.
- `months`: the months overlapping this page that are not fully loaded, each with its
  `state`: `bad` (the month has bad records; its good bars are served), `missing` (no
  canonical file), or `stale` (a file from another build; it is never served).
- `gaps`: the runs of missing bars that **start in this page**, each with `kind`,
  `start`, `end` (exclusive) and `bars`. Kinds: `closure` (a year-end holiday),
  `known_hole` (bars the provider does not have, on a reviewed list) and `unexplained`.
  Weekends are not reported. Closures and known holes are explained and still
  `complete`; an `unexplained` gap, or a bad, missing or stale month, makes it
  `complete: false`. Every gap appears on exactly one page.

With `partial=false` an incomplete range is refused (`422 incomplete`, below) instead of
served, with the same `coverage` in the error.

## Look-ahead

`from` and `to` select by **opening** time, so the last bar of a range can open before
`to` and only become available after it. To replay history without look-ahead, pass
`as_of`: only bars with `available_at <= as_of` are returned.

## Errors

Every error is `{"error": {"code": "...", "message": "..."}}`, and `no_data` and
`incomplete` also carry `coverage`.

| Status | `code` | When |
|---|---|---|
| 400 | `invalid_parameter` | a missing, unknown, repeated or malformed parameter |
| 400 | `invalid_request` | an unsupported interval, an invalid instrument name, an empty or reversed range, a position outside the range |
| 400 | `invalid_cursor` | a cursor this API did not issue, or one that belongs to a different query |
| 401 | `unauthorized` | a token is required and missing or wrong |
| 404 | `no_data` / `not_found` | no canonical file exists for any month of the range; or no such endpoint |
| 405 | `method_not_allowed` | anything but `GET` |
| 422 | `incomplete` | `partial=false` and the range is not complete |
| 422 | `mixed_series` | months of the range come from different snapshots or descriptions; rebuild the series |
| 499 | `cancelled` | the request was cancelled |
| 500 | `internal` | an unexpected failure (its detail is logged, never returned) |
| 504 | `timeout` | the request took longer than `--request-timeout` |

## Deployment and authentication

The server is local-only by default: it listens on `127.0.0.1:8080`. Listening on any
other address is refused unless `TRADER_API_TOKEN` is set in the environment; then every
request, including a 404, must carry `Authorization: Bearer <token>` (compared in
constant time; the token is never logged or returned). There is no TLS: put the server
behind a TLS-terminating proxy before it leaves the machine. It is read-only and holds no
provider credentials. Set the data root with `--data-root`, `TRADER_DATA_ROOT` or
`marketdata.root` in `etc/trader.yml`. `Ctrl-C` stops it after the requests in flight.

## Examples

```sh
# one page
curl -s 'http://127.0.0.1:8080/v1/marketdata/bars?instrument=EURUSD&interval=h1&from=2024-05-20&to=2024-05-21&limit=24'

# a replay as of a moment, strict
curl -s 'http://127.0.0.1:8080/v1/marketdata/bars?instrument=EURUSD&interval=d1&from=2024-01-01&to=2024-07-01&as_of=2024-03-15T21:00:00Z&partial=false'

# with a token
curl -s -H "Authorization: Bearer $TRADER_API_TOKEN" 'http://api.example:8080/v1/marketdata/bars?...'
```

An integration, for `investor` or any Go program (it decodes the documented schema,
follows the cursor and refuses to treat an incomplete range as complete):

```go
type Bar struct {
	Time        time.Time `json:"time"`
	AvailableAt time.Time `json:"available_at"`
	Volume      int64     `json:"volume"`
	Mid         *struct{ Open, High, Low, Close string } `json:"mid"`
}

type Page struct {
	Bars       []Bar  `json:"bars"`
	NextCursor string `json:"next_cursor"`
	Coverage   struct {
		Complete bool `json:"complete"`
		Gaps     []struct {
			Kind       string    `json:"kind"`
			Start, End time.Time
			Bars       int `json:"bars"`
		} `json:"gaps"`
	} `json:"coverage"`
}

func fetchAll(base, instrument, interval, from, to string) ([]Bar, error) {
	var all []Bar
	q := url.Values{"instrument": {instrument}, "interval": {interval}, "from": {from}, "to": {to}}
	for first := true; ; first = false {
		resp, err := http.Get(base + "/v1/marketdata/bars?" + q.Encode())
		if err != nil {
			return nil, err
		}
		var page Page
		err = json.NewDecoder(resp.Body).Decode(&page)
		_ = resp.Body.Close()
		if err != nil || resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("bars: status %d: %v", resp.StatusCode, err)
		}
		if first && !page.Coverage.Complete {
			// The first page describes the whole range: stop, or carry on knowing it.
			log.Printf("range %s..%s is not complete", from, to)
		}
		all = append(all, page.Bars...)
		if page.NextCursor == "" {
			return all, nil
		}
		q.Set("cursor", page.NextCursor)
	}
}
```
