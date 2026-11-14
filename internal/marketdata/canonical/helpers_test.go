package canonical

import (
	"testing"
	"time"

	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
)

func px(s string) num.Price { return num.MustParsePrice(s) }

func utc(y int, m time.Month, d, h int) time.Time { return time.Date(y, m, d, h, 0, 0, 0, time.UTC) }

// goodBar is a valid H1 bar opening at open.
func goodBar(t *testing.T, open time.Time) marketdata.Bar {
	t.Helper()
	avail, err := AvailableAt(H1, open)
	if err != nil {
		t.Fatal(err)
	}
	bid := marketdata.OHLC{Open: px("1.10000"), High: px("1.10100"), Low: px("1.09900"), Close: px("1.10050")}
	ask := marketdata.OHLC{Open: px("1.10010"), High: px("1.10111"), Low: px("1.09911"), Close: px("1.10061")}
	mid, err := marketdata.MidOf(bid, ask)
	if err != nil {
		t.Fatal(err)
	}
	return marketdata.Bar{Time: open, AvailableAt: avail, Bid: bid, Ask: ask, Mid: mid, Volume: 100}
}

func testMeta() Meta {
	return Meta{RawFingerprint: "sha256:abc", BuilderVersion: "builder-v1", ValidatorVersion: "validator-v1",
		AvailabilityPolicy: PolicyScheduledCloseV1, BuiltAt: utc(2026, 11, 14, 12)}
}

func jan24H1() Key {
	return Key{Source: "oanda", Symbol: "EURUSD", Interval: H1, Year: 2024, Month: time.January}
}

// goodFile is a valid January 2024 H1 file with three consecutive bars.
func goodFile(t *testing.T) File {
	t.Helper()
	f := File{Key: jan24H1(), Meta: testMeta()}
	for i := range 3 {
		f.Bars = append(f.Bars, goodBar(t, utc(2024, 1, 2, i)))
	}
	return f
}
