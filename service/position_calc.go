package service

import (
	"context"
	"fmt"
	"math"

	trader "github.com/rustyeddy/trader"
)

// PositionCalcRequest parameterises PositionCalc.
// Supply Units or Notional to get a single row; omit both for a micro/mini/standard table.
// Price 0 fetches the live mid from OANDA (requires OANDA to be configured).
type PositionCalcRequest struct {
	Instrument string
	Price      float64 // 0 = fetch from OANDA
	Units      int64   // specific size; 0 = table or use Notional
	Notional   float64 // target USD notional; only used when Units == 0
	Pips       float64 // optional: include pip P&L in each row
}

// PositionRow is one row in a position sizing table.
type PositionRow struct {
	Label    string  `json:"label"`
	Units    int64   `json:"units"`
	Lots     float64 `json:"lots"`
	Notional float64 `json:"notional_usd"`
	Margin   float64 `json:"margin_usd"`
	PipsPL   float64 `json:"pips_pl_usd,omitempty"`
}

// PositionCalcResult is the full position calculator output.
type PositionCalcResult struct {
	Instrument string        `json:"instrument"`
	Price      float64       `json:"price"`
	MarginPct  float64       `json:"margin_pct"`
	Pips       float64       `json:"pips,omitempty"`
	Rows       []PositionRow `json:"rows"`
}

func (s *Service) PositionCalc(ctx context.Context, req PositionCalcRequest) (*PositionCalcResult, error) {
	inst := trader.NormalizeInstrument(req.Instrument)
	instMeta := trader.GetInstrument(inst)
	if instMeta == nil {
		return nil, fmt.Errorf("unknown instrument: %s", req.Instrument)
	}

	if math.IsNaN(req.Price) {
		return nil, fmt.Errorf("price must be a valid number")
	}
	if req.Price < 0 {
		return nil, fmt.Errorf("price must be >= 0 (use 0 to fetch from OANDA)")
	}
	if req.Units < 0 {
		return nil, fmt.Errorf("units must be >= 0")
	}
	if req.Notional < 0 {
		return nil, fmt.Errorf("notional must be >= 0")
	}
	if req.Pips < 0 {
		return nil, fmt.Errorf("pips must be >= 0")
	}
	if req.Units > 0 && req.Notional > 0 {
		return nil, fmt.Errorf("specify units or notional, not both")
	}

	price := req.Price
	if price == 0 {
		if s.OANDA == nil {
			return nil, fmt.Errorf("price required when OANDA is not configured")
		}
		oandaName := instMeta.BaseCurrency + "_" + instMeta.QuoteCurrency
		prices, err := s.OANDA.GetPricing(ctx, s.AccountID, oandaName)
		if err != nil {
			return nil, fmt.Errorf("fetch price: %w", err)
		}
		if len(prices) == 0 || prices[0].Mid == 0 {
			return nil, fmt.Errorf("OANDA returned zero price for %s", inst)
		}
		price = prices[0].Mid
	}

	type lotDef struct {
		label string
		units int64
	}
	var lots []lotDef
	switch {
	case req.Units > 0:
		lots = []lotDef{{"custom", req.Units}}
	case req.Notional > 0:
		lots = []lotDef{{"custom", posUnitsForNotional(instMeta, price, req.Notional)}}
	default:
		lots = []lotDef{
			{"micro (0.01)", 1_000},
			{"mini (0.1)", 10_000},
			{"standard (1.0)", 100_000},
		}
	}

	rows := make([]PositionRow, 0, len(lots))
	for _, l := range lots {
		n := posNotionalUSD(instMeta, price, l.units)
		m := n * instMeta.MarginRate.Float64()
		row := PositionRow{
			Label:    l.label,
			Units:    l.units,
			Lots:     float64(l.units) / 100_000,
			Notional: n,
			Margin:   m,
		}
		if req.Pips > 0 {
			row.PipsPL = instMeta.PipValueUSD(price, l.units, req.Pips)
		}
		rows = append(rows, row)
	}

	return &PositionCalcResult{
		Instrument: inst,
		Price:      price,
		MarginPct:  instMeta.MarginRate.Float64() * 100,
		Pips:       req.Pips,
		Rows:       rows,
	}, nil
}

func posNotionalUSD(inst *trader.Instrument, midPrice float64, units int64) float64 {
	if inst.BaseCurrency == "USD" {
		return float64(units)
	}
	return float64(units) * midPrice
}

func posUnitsForNotional(inst *trader.Instrument, midPrice, targetUSD float64) int64 {
	if inst.BaseCurrency == "USD" {
		return int64(math.Round(targetUSD))
	}
	if midPrice <= 0 {
		return 0
	}
	return int64(math.Round(targetUSD / midPrice))
}
