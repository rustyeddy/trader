package service

import (
	"context"
	"fmt"
	"time"

	"github.com/rustyeddy/trader/datamanager"
)

type ValidateCandleDataRequest struct {
	Instruments []string
	Source      string
	Timeframe   string
	From        time.Time
	To          time.Time
	IncludeRaw  bool
	RawDir      string
}

func (s *Service) ValidateCandleData(ctx context.Context, req ValidateCandleDataRequest) (*datamanager.CandleValidationReport, error) {
	if len(req.Instruments) == 0 {
		return nil, fmt.Errorf("missing instruments")
	}
	tf, err := parseTraderTimeframe(req.Timeframe)
	if err != nil {
		return nil, err
	}
	return datamanager.ValidateCandleData(ctx, datamanager.CandleValidationRequest{
		Instruments: req.Instruments,
		Source:      req.Source,
		Timeframe:   tf,
		Start:       req.From,
		End:         req.To,
		IncludeRaw:  req.IncludeRaw,
		RawDir:      req.RawDir,
	})
}
