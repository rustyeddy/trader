package alpaca

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/rustyeddy/trader/num"
)

// This file isolates every JSON-shape-specific type and parsing
// function for Alpaca's historical-bars response, deliberately, per
// doc.go's own "unverified API shape" note: if the real API's wire
// format differs from what is assumed here, this is the one file that
// needs correcting — client.go's request construction, retry policy,
// and pagination loop do not need to change.
//
// # Assumed response shape
//
//	{
//	  "bars": [
//	    {"t": "2024-01-02T05:00:00Z", "o": 472.16, "h": 473.67,
//	     "l": 470.49, "c": 472.65, "v": 123456789, "n": 4567, "vw": 472.1}
//	  ],
//	  "symbol": "SPY",
//	  "next_page_token": "abc123"
//	}
//
// "next_page_token" is assumed to be either absent, null, or an empty
// string when there is no further page.

// barsResponse is the assumed JSON shape of Alpaca's
// /v2/stocks/{symbol}/bars response.
type barsResponse struct {
	Bars          []wireBar `json:"bars"`
	Symbol        string    `json:"symbol"`
	NextPageToken string    `json:"next_page_token"`
}

// wireBar is one assumed bar entry. Prices are decoded as
// json.Number, not float64: Alpaca's real API almost certainly encodes
// them as JSON numeric literals, and decoding straight into a Go
// float64 before constructing a num.Price would round the value once
// in an unspecified way before num.ParsePrice's own parsing could ever
// see it. json.Number preserves the literal's original decimal text
// (json.Decoder never re-renders a numeric token through float64
// arithmetic when the destination type is json.Number), so
// num.ParsePrice parses the exact text the API sent — the same
// exactness discipline oanda.Record's own string-based price decoding
// already follows for a wire format that happens to use JSON strings
// instead of numbers for the same purpose. This is not a case ADR-045
// governs (that ADR's boundary is for an internal, already-exact
// num.Price converting *out* to float64 for analytical use); this is
// external, arbitrary-precision wire data converting *in*, which
// num.ParsePrice's own ordinary construction path already handles
// correctly as long as it never passes through float64 first.
type wireBar struct {
	Time   string      `json:"t"`
	Open   json.Number `json:"o"`
	High   json.Number `json:"h"`
	Low    json.Number `json:"l"`
	Close  json.Number `json:"c"`
	Volume int64       `json:"v"`
}

// records converts the decoded response into Records, re-anchoring
// each bar's Time from its literal fetched instant to midnight UTC of
// its own trading date in America/New_York civil time — see the
// package doc comment's "Timestamp normalization" section for why.
func (r barsResponse) records() ([]Record, error) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		return nil, fmt.Errorf("alpaca: %w: load America/New_York location: %v", ErrBadRequest, err)
	}

	out := make([]Record, 0, len(r.Bars))
	for i, b := range r.Bars {
		t, err := time.Parse(time.RFC3339Nano, b.Time)
		if err != nil {
			return nil, fmt.Errorf("alpaca: %w: bar %d: time: %v", ErrBadRequest, i, err)
		}
		tradingDate := t.In(loc)
		day := time.Date(tradingDate.Year(), tradingDate.Month(), tradingDate.Day(), 0, 0, 0, 0, time.UTC)

		rec := Record{Time: day, Volume: b.Volume}
		prices := []struct {
			dst  *num.Price
			s    string
			name string
		}{
			{&rec.Open, b.Open.String(), "o"},
			{&rec.High, b.High.String(), "h"},
			{&rec.Low, b.Low.String(), "l"},
			{&rec.Close, b.Close.String(), "c"},
		}
		for _, p := range prices {
			v, err := num.ParsePrice(p.s)
			if err != nil {
				return nil, fmt.Errorf("alpaca: %w: bar %d: %s: %v", ErrBadRequest, i, p.name, err)
			}
			*p.dst = v
		}
		out = append(out, rec)
	}
	return out, nil
}
