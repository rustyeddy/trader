package service

import (
	"context"
	"maps"
	"strings"

	"github.com/rustyeddy/trader/market"
)

// PipValuesRequest parameterises PipValues.
type PipValuesRequest struct {
	Units       int64              // 0 = default 100000 (standard lot)
	Instruments []string           // empty = all major pairs
	Rates       map[string]float64 // explicit rate overrides for USD-base pairs
}

// PipValueRow holds pip-value calculations for one instrument.
type PipValueRow struct {
	Instrument string  `json:"instrument"`
	Pips1      float64 `json:"pips_1"`
	Pips10     float64 `json:"pips_10"`
	Pips100    float64 `json:"pips_100"`
	Pips1000   float64 `json:"pips_1000"`
	RateUsed   float64 `json:"rate_used,omitempty"` // set for USD-base pairs
	RateLive   bool    `json:"rate_live,omitempty"` // true when rate came from OANDA
}

// PipValuesResult holds the full pip-value table.
type PipValuesResult struct {
	Units int64         `json:"units"`
	Rows  []PipValueRow `json:"rows"`
}

// pipValueDefaults are approximate mid rates for USD-base pairs when OANDA is unavailable.
var pipValueDefaults = map[string]float64{
	"USDJPY": 150.00,
	"USDCHF": 0.90,
	"USDCAD": 1.36,
}

var usdBasePairsOanda = []string{"USD_JPY", "USD_CHF", "USD_CAD"}

func (s *Service) PipValues(ctx context.Context, req PipValuesRequest) (*PipValuesResult, error) {
	units := req.Units
	if units <= 0 {
		units = 100_000
	}
	instruments := req.Instruments
	if len(instruments) == 0 {
		instruments = market.MajorInstruments()
	}

	rates := make(map[string]float64, len(pipValueDefaults))
	maps.Copy(rates, pipValueDefaults)
	live := false

	// Try to fetch live mid prices for USD-base pairs when OANDA is available.
	if s.OANDA != nil {
		if prices, err := s.OANDA.GetPricing(ctx, s.AccountID, usdBasePairsOanda...); err == nil {
			for _, p := range prices {
				traderName := strings.ReplaceAll(p.Instrument, "_", "")
				rates[traderName] = p.Mid
			}
			live = true
		}
	}

	// Explicit overrides always win over live.
	for k, v := range req.Rates {
		if v <= 0 {
			continue // non-positive rates produce meaningless pip values; skip
		}
		rates[market.NormalizeInstrument(k)] = v
		live = false
	}

	rows := make([]PipValueRow, 0, len(instruments))
	for _, name := range instruments {
		inst := market.GetInstrument(name)
		if inst == nil {
			continue
		}
		norm := market.NormalizeInstrument(name)
		rate := rates[norm]
		row := PipValueRow{
			Instrument: name,
			Pips1:      inst.PipValueUSD(rate, units, 1),
			Pips10:     inst.PipValueUSD(rate, units, 10),
			Pips100:    inst.PipValueUSD(rate, units, 100),
			Pips1000:   inst.PipValueUSD(rate, units, 1000),
		}
		if inst.QuoteCurrency != "USD" {
			row.RateUsed = rate
			row.RateLive = live
		}
		rows = append(rows, row)
	}

	return &PipValuesResult{Units: units, Rows: rows}, nil
}
