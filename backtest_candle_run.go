package trader

import (
	"context"
)

type CandleSource interface {
	Candles(ctx context.Context, req CandleRequest) (candleIterator, error)
}

type CandleRunRequest struct {
	DataRequest     CandleRequest
	StartingBalance Money
	AccountCCY      string
	Scale           Scale6
}

// func RunCandles(
// 	ctx context.Context,
// 	src CandleSource,
// 	req CandleRunRequest,
// 	strat Strategy,
// 	acct *Account,
// ) (*CandleEngine, error) {
// 	if acct == nil {
// 		return nil, fmt.Errorf("RunCandles: nil account")
// 	}

// 	it, err := src.Candles(ctx, req.DataRequest)
// 	if err != nil {
// 		return nil, err
// 	}

// 	engine := &CandleEngine{
// 		Instrument: req.DataRequest.Instrument,
// 		Timeframe:  req.DataRequest.Timeframe,
// 		Account:    acct,
// 	}
// 	if err := engine.RunContext(ctx, it, strat); err != nil {
// 		return nil, err
// 	}

// 	return engine, nil
// }
