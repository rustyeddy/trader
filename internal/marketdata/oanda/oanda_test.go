package oanda

import (
	"testing"
	"time"

	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/num"
	"github.com/stretchr/testify/require"
)

// eurusdMeta is the Meta a valid EURUSD 2020-05 H1 partition resolves to.
func eurusdMeta() Meta {
	return Meta{
		Instrument: instrument.CurrencyPairID(num.MustParseCurrency("EUR"), num.MustParseCurrency("USD")),
		Interval:   RawH1,
		Year:       2020,
		Month:      time.May,
		Symbol:     "EURUSD",
	}
}

func at(y int, m time.Month, d, h int) time.Time { return time.Date(y, m, d, h, 0, 0, 0, time.UTC) }

// fixtureCase names an archive fixture and the partition it describes.
type fixtureCase struct {
	file     string
	symbol   string
	interval RawInterval
	year     int
	month    time.Month
}

var fixtures = []fixtureCase{
	{"EURUSD-2020-05-h1.csv", "EURUSD", RawH1, 2020, time.May},
	{"EURUSD-2020-05-d1.csv", "EURUSD", RawD1, 2020, time.May},
	{"USDJPY-2020-05-h1.csv", "USDJPY", RawH1, 2020, time.May},
}

func mustPath(t *testing.T, root string) string {
	t.Helper()
	p, err := PartitionKey{"EURUSD", RawH1, 2024, time.January}.Path(root)
	require.NoError(t, err)
	return p
}
