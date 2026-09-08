package alpaca

import (
	"fmt"
	"math"
	"strconv"
	"time"

	sdkmarketdata "github.com/alpacahq/alpaca-trade-api-go/v3/marketdata"

	"github.com/rustyeddy/trader/num"
)

// This file isolates every SDK-bar-to-Record conversion, mirroring the
// role its previous, hand-written-JSON-decoding version played (issue
// #297): if the SDK's own Bar shape or this package's conversion
// policy ever needs correcting, this is the one file that needs to
// change — client.go's request construction, retry policy, and
// pagination delegation do not.
//
// # Why quantize a float64 into num.Price at all
//
// sdkmarketdata.Bar decodes Open/High/Low/Close directly into float64
// fields (github.com/alpacahq/alpaca-trade-api-go/v3/marketdata's own
// easyjson-generated decoder) — unlike this package's previous
// hand-written decoder, which decoded into json.Number specifically to
// preserve the API's original decimal text for num.ParsePrice. By the
// time this package receives a sdkmarketdata.Bar, that original text
// no longer exists: the SDK's own decoding has already performed one
// binary-float rounding pass that cannot be undone.
//
// quantizedPriceFromFloat is the deliberate response to that
// constraint, not a casual shortcut: it rounds the float64 to the cent
// tick size (ADR-047's Phase 1 equity default) before constructing
// num.Price via num.ParsePrice's own checked parsing — exactly the
// "round to the listing's tick size, then construct via the type's own
// constructor" reconstruction path ADR-045 explicitly permits for an
// analytical float64 becoming authoritative again. It is not a general
// license to skip quantization elsewhere; it exists here only because
// adopting the official SDK (issue #323) leaves no alternative.

// quantizedPriceFromFloat rounds v to two decimal places (the cent
// tick size, ADR-047's Phase 1 equity default) and constructs a
// num.Price from the resulting decimal text. It rejects a non-finite
// v (NaN/Inf) with ErrBadRequest — a value that should never occur in
// a real Alpaca bar, but one this package must not silently propagate
// into an accounting type if it ever does.
func quantizedPriceFromFloat(name string, v float64) (num.Price, error) {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return num.Price{}, fmt.Errorf("alpaca: %w: %s: non-finite price %v", ErrBadRequest, name, v)
	}
	price, err := num.ParsePrice(strconv.FormatFloat(v, 'f', 2, 64))
	if err != nil {
		return num.Price{}, fmt.Errorf("alpaca: %w: %s: %v", ErrBadRequest, name, err)
	}
	return price, nil
}

// recordsFromSDKBars converts bars (as returned by
// sdkmarketdata.Client.GetBars) into Records, re-anchoring each bar's
// Timestamp from its literal fetched instant to midnight UTC of its
// own trading date in America/New_York civil time — see the package
// doc comment's "Timestamp normalization" section for why — and
// quantizing each price field via quantizedPriceFromFloat.
func recordsFromSDKBars(bars []sdkmarketdata.Bar) ([]Record, error) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		return nil, fmt.Errorf("alpaca: %w: load America/New_York location: %v", ErrBadRequest, err)
	}

	out := make([]Record, 0, len(bars))
	for i, b := range bars {
		if b.Volume > math.MaxInt64 {
			return nil, fmt.Errorf("alpaca: %w: bar %d: volume %d overflows int64", ErrBadRequest, i, b.Volume)
		}

		tradingDate := b.Timestamp.In(loc)
		day := time.Date(tradingDate.Year(), tradingDate.Month(), tradingDate.Day(), 0, 0, 0, 0, time.UTC)

		rec := Record{Time: day, Volume: int64(b.Volume)}
		prices := []struct {
			dst  *num.Price
			v    float64
			name string
		}{
			{&rec.Open, b.Open, "o"},
			{&rec.High, b.High, "h"},
			{&rec.Low, b.Low, "l"},
			{&rec.Close, b.Close, "c"},
		}
		for _, p := range prices {
			v, err := quantizedPriceFromFloat(p.name, p.v)
			if err != nil {
				return nil, fmt.Errorf("alpaca: bar %d: %w", i, err)
			}
			*p.dst = v
		}
		out = append(out, rec)
	}
	return out, nil
}
