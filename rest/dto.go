package rest

import (
	"time"

	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/service"
)

// The JSON the API speaks. Times are RFC 3339 UTC with a Z; prices are exact decimal
// strings with five decimals (never floating point); a price basis the bar does not
// have is left out.

type ohlcJSON struct {
	Open  string `json:"open"`
	High  string `json:"high"`
	Low   string `json:"low"`
	Close string `json:"close"`
}

type barJSON struct {
	// Time is when the bar opens. AvailableAt is the earliest instant a consumer may
	// use the completed bar: never act on a bar before it.
	Time        time.Time `json:"time"`
	AvailableAt time.Time `json:"available_at"`
	Volume      int64     `json:"volume"`
	Bid         *ohlcJSON `json:"bid,omitempty"`
	Ask         *ohlcJSON `json:"ask,omitempty"`
	Mid         *ohlcJSON `json:"mid,omitempty"`
	Trade       *ohlcJSON `json:"trade,omitempty"`
}

type monthJSON struct {
	Year   int    `json:"year"`
	Month  int    `json:"month"`
	State  string `json:"state"`
	Good   int    `json:"good"`
	Bad    int    `json:"bad"`
	Reason string `json:"reason,omitempty"`
}

type gapJSON struct {
	// Kind is "closure" (a year-end holiday), "known_hole" (bars the provider does not
	// have, on a reviewed list) or "unexplained". Weekends are not reported.
	Kind string `json:"kind"`
	// Start is when the first missing bar would open; End is exclusive.
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
	Bars  int       `json:"bars"`
}

type summaryJSON struct {
	Months           int `json:"months"`
	Loaded           int `json:"loaded"`
	Bad              int `json:"bad"`
	Missing          int `json:"missing"`
	Stale            int `json:"stale"`
	GapsUnexplained  int `json:"gaps_unexplained"`
	GapsKnownHole    int `json:"gaps_known_hole"`
	GapsKnownClosure int `json:"gaps_known_closure"`
}

type coverageJSON struct {
	// Complete is false when a month is bad, missing or stale or bars are missing
	// that nothing explains, over the range from this page on.
	Complete bool        `json:"complete"`
	Summary  summaryJSON `json:"summary"`
	// Months are the months overlapping this page that are not fully loaded, and Gaps
	// are the runs of missing bars that start in this page: the rows that are missing.
	Months []monthJSON `json:"months"`
	Gaps   []gapJSON   `json:"gaps"`
}

type barsJSON struct {
	Source     string     `json:"source"`
	Instrument string     `json:"instrument"`
	Interval   string     `json:"interval"`
	From       time.Time  `json:"from"`
	To         time.Time  `json:"to"`
	AsOf       *time.Time `json:"as_of,omitempty"`
	Bars       []barJSON  `json:"bars"`
	// NextCursor is present when more bars follow: pass it back as cursor, with the
	// same other parameters, for the next page.
	NextCursor string       `json:"next_cursor,omitempty"`
	Coverage   coverageJSON `json:"coverage"`
	Data       *dataJSON    `json:"data,omitempty"`
}

// dataJSON describes a trade-price series. It is absent for quote data.
type dataJSON struct {
	// PriceBasis is "trade": the bars have no bid, ask or mid.
	PriceBasis         string `json:"price_basis"`
	Currency           string `json:"currency"`
	VolumeUnit         string `json:"volume_unit"`
	Adjustment         string `json:"adjustment"`
	AdjustmentEvidence string `json:"adjustment_evidence"`
	AvailabilityPolicy string `json:"availability_policy"`
	Calendar           string `json:"calendar"`
	// SnapshotFingerprint and SnapshotAsOf identify the one input of the series:
	// pages with different fingerprints must not be joined.
	SnapshotFingerprint string `json:"snapshot_fingerprint"`
	SnapshotAsOf        string `json:"snapshot_as_of"`
}

func dataOf(d *service.DataInfo) *dataJSON {
	if d == nil {
		return nil
	}
	return &dataJSON{PriceBasis: d.PriceBasis, Currency: d.Currency, VolumeUnit: d.VolumeUnit, Adjustment: d.Adjustment,
		AdjustmentEvidence: d.AdjustmentEvidence, AvailabilityPolicy: d.AvailabilityPolicy, Calendar: d.Calendar,
		SnapshotFingerprint: d.SnapshotFingerprint, SnapshotAsOf: d.SnapshotAsOf}
}

type errorJSON struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type errorBody struct {
	Error errorJSON `json:"error"`
	// Coverage describes what was and was not found, for an incomplete or an empty
	// range.
	Coverage *coverageJSON `json:"coverage,omitempty"`
}

// price is what num.Price offers the encoding: exact fixed-decimal text.
type price interface {
	FormatFixed(decimals int) (string, error)
	String() string
}

func priceText(p price) string {
	if s, err := p.FormatFixed(priceDecimals); err == nil {
		return s
	}
	return p.String() // a price that does not fit five decimals keeps its own exact text
}

func ohlcOf(o marketdata.OHLC) *ohlcJSON {
	if o.IsZero() {
		return nil
	}
	return &ohlcJSON{Open: priceText(o.Open), High: priceText(o.High), Low: priceText(o.Low), Close: priceText(o.Close)}
}

// priceDecimals is the fixed decimals of every price the archive stores.
const priceDecimals = 5

func barOf(b marketdata.Bar) barJSON {
	return barJSON{Time: b.Time.UTC(), AvailableAt: b.AvailableAt.UTC(), Volume: b.Volume,
		Bid: ohlcOf(b.Bid), Ask: ohlcOf(b.Ask), Mid: ohlcOf(b.Mid), Trade: ohlcOf(b.Trade)}
}

func coverageOf(c service.BarsCoverage) coverageJSON {
	out := coverageJSON{Complete: c.Complete, Months: []monthJSON{}, Gaps: []gapJSON{},
		Summary: summaryJSON{Months: c.Summary.Months, Loaded: c.Summary.Loaded, Bad: c.Summary.Bad, Missing: c.Summary.Missing,
			Stale: c.Summary.Stale, GapsUnexplained: c.Summary.GapsUnexplained, GapsKnownHole: c.Summary.GapsKnownHole,
			GapsKnownClosure: c.Summary.GapsKnownClosure}}
	for _, m := range c.Months {
		out.Months = append(out.Months, monthJSON{Year: m.Year, Month: int(m.Month), State: string(m.State), Good: m.Good, Bad: m.Bad, Reason: m.Reason})
	}
	for _, g := range c.Gaps {
		out.Gaps = append(out.Gaps, gapJSON{Kind: string(g.Kind), Start: g.Start.UTC(), End: g.End.UTC(), Bars: g.Bars})
	}
	return out
}
