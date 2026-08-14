package oanda

import (
	"testing"
	"time"

	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// eurusdMeta is the Meta a valid EURUSD 2020-05 H1 partition resolves to.
func eurusdMeta() Meta {
	return Meta{
		Instrument: instrument.CurrencyPairID(num.MustParseCurrency("EUR"), num.MustParseCurrency("USD")),
		Interval:   marketdata.H1,
		Year:       2020,
		Month:      time.May,
		Symbol:     "EURUSD",
	}
}

func TestCrossCheckSchemaAccepts(t *testing.T) {
	meta := eurusdMeta()
	comment := "# schema=raw-v1 source=oanda instrument=EURUSD tf=h1 year=2020 month=05"
	require.NoError(t, crossCheckSchema(comment, "p.csv", meta, "h1"))
}

// A daily partition's path token (d1) and comment tf (d) differ but resolve to
// the same interval; the cross-check must accept that.
func TestCrossCheckSchemaAcceptsDailyTokenVariance(t *testing.T) {
	meta := eurusdMeta()
	meta.Interval = marketdata.D1
	comment := "# schema=raw-v1 source=oanda instrument=EURUSD tf=d year=2020 month=05"
	require.NoError(t, crossCheckSchema(comment, "p.csv", meta, "d1"))
}

// A "# ..." comment that is not a schema line is ignored, not rejected.
func TestCrossCheckSchemaIgnoresNonSchemaComment(t *testing.T) {
	require.NoError(t, crossCheckSchema("# just a note", "p.csv", eurusdMeta(), "h1"))
}

func TestCrossCheckSchemaRejects(t *testing.T) {
	cases := map[string]string{
		"bad schema version":  "# schema=raw-v2 source=oanda instrument=EURUSD tf=h1 year=2020 month=05",
		"bad source":          "# schema=raw-v1 source=dukascopy instrument=EURUSD tf=h1 year=2020 month=05",
		"instrument mismatch": "# schema=raw-v1 source=oanda instrument=GBPUSD tf=h1 year=2020 month=05",
		"tf mismatch":         "# schema=raw-v1 source=oanda instrument=EURUSD tf=h4 year=2020 month=05",
		"unsupported tf":      "# schema=raw-v1 source=oanda instrument=EURUSD tf=w1 year=2020 month=05",
		"year mismatch":       "# schema=raw-v1 source=oanda instrument=EURUSD tf=h1 year=2019 month=05",
		"month mismatch":      "# schema=raw-v1 source=oanda instrument=EURUSD tf=h1 year=2020 month=06",
	}
	for kind, comment := range cases {
		err := crossCheckSchema(comment, "p.csv", eurusdMeta(), "h1")
		assert.ErrorIs(t, err, ErrMalformedData, kind)
	}
}
