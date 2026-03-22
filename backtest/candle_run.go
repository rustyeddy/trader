package backtest

import (
	"context"

	"github.com/rustyeddy/trader/data"
	"github.com/rustyeddy/trader/types"
)

type CandleRunRequest struct {
	DataRequest     data.CandleRequest
	StartingBalance types.Money
	AccountCCY      string
	Scale           types.Scale6
}

func RunCandles(
	ctx context.Context,
	dm *data.DataManager,
	req CandleRunRequest,
	strat CandleStrategy,
) (*CandleEngine, error) {
	it, err := dm.Candles(ctx, req.DataRequest)
	if err != nil {
		return nil, err
	}

	engine := NewCandleEngine(
		req.DataRequest.Instrument,
		req.DataRequest.Timeframe,
		req.Scale,
		req.StartingBalance,
		req.AccountCCY,
	)

	if err := engine.Run(it, strat); err != nil {
		return nil, err
	}

	return engine, nil
}
