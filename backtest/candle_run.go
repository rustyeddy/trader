package backtest

import (
	"context"
	"fmt"

	"github.com/rustyeddy/trader/account"
	"github.com/rustyeddy/trader/data"
	"github.com/rustyeddy/trader/types"
)

type CandleSource interface {
	Candles(ctx context.Context, req data.CandleRequest) (data.CandleIterator, error)
}

type CandleRunRequest struct {
	DataRequest     data.CandleRequest
	StartingBalance types.Money
	AccountCCY      string
	Scale           types.Scale6
}

func RunCandles(
	ctx context.Context,
	src CandleSource,
	req CandleRunRequest,
	strat CandleStrategy,
	acct *account.Account,
) (*CandleEngine, error) {
	if acct == nil {
		return nil, fmt.Errorf("RunCandles: nil account")
	}

	it, err := src.Candles(ctx, req.DataRequest)
	if err != nil {
		return nil, err
	}

	engine := &CandleEngine{
		Instrument: req.DataRequest.Instrument,
		Timeframe:  req.DataRequest.Timeframe,
		Account:    acct,
	}
	if err := engine.Run(it, strat); err != nil {
		return nil, err
	}

	return engine, nil
}
