package service

import (
	"context"
	"strings"

	"github.com/rustyeddy/trader"
)

// PriceInfo holds the current bid/ask snapshot for one instrument.
type PriceInfo struct {
	Instrument string  `json:"instrument"`
	Bid        float64 `json:"bid"`
	Ask        float64 `json:"ask"`
	Mid        float64 `json:"mid"`
	SpreadPips float64 `json:"spread_pips"`
}

// GetPricesRequest parameterises GetPrices.
// Instruments is a list of trader-format symbols (e.g. "EURUSD").
// An empty list defaults to all major pairs.
type GetPricesRequest struct {
	Instruments []string
}

// GetPrices fetches the current bid/ask for the requested instruments from
// OANDA and returns one PriceInfo per instrument.
func (a *Account) GetPrices(ctx context.Context, req GetPricesRequest) ([]PriceInfo, error) {
	names := req.Instruments
	if len(names) == 0 {
		names = trader.MajorInstruments()
	}

	oandaNames := make([]string, 0, len(names))
	instMap := make(map[string]*trader.Instrument, len(names))
	for _, name := range names {
		inst := trader.GetInstrument(name)
		if inst == nil {
			continue
		}
		oandaKey := inst.BaseCurrency + "_" + inst.QuoteCurrency
		oandaNames = append(oandaNames, oandaKey)
		instMap[oandaKey] = inst
	}

	raw, err := a.svc.OANDA.GetPricing(ctx, a.ID, oandaNames...)
	if err != nil {
		return nil, err
	}

	out := make([]PriceInfo, 0, len(raw))
	for _, p := range raw {
		spreadPips := 0.0
		if inst, ok := instMap[p.Instrument]; ok {
			pip := inst.PipSize()
			if pip > 0 {
				spreadPips = (p.Ask - p.Bid) / pip
			}
		}
		out = append(out, PriceInfo{
			Instrument: strings.ReplaceAll(p.Instrument, "_", ""),
			Bid:        p.Bid,
			Ask:        p.Ask,
			Mid:        p.Mid,
			SpreadPips: spreadPips,
		})
	}
	return out, nil
}

// GetPrices fetches current bid/ask snapshots. Prices are market data, not
// account-specific, so this read may default to the first account.
func (s *Service) GetPrices(ctx context.Context, req GetPricesRequest) ([]PriceInfo, error) {
	acc, err := s.FirstAccount(ctx)
	if err != nil {
		return nil, err
	}
	return acc.GetPrices(ctx, req)
}
