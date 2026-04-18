package trader

import (
	"context"
)

func RecordBacktest(ctx context.Context, btr BacktestRun) error {

	return nil
}

func GetBacktestRun(ctx context.Context, runID string) (btr BacktestRun, err error) {

	return
}

func ListTradesByRunID(ctx context.Context, runID string) (tr []TradeRecord, err error) {

	return
}

func ListEquityByRunID(ctx context.Context, runID string) (eq []EquitySnapshot, err error) {

	return
}

// ExportBacktestOrg loads everything and returns the Org block.
func ExportBacktestOrg(ctx context.Context, runID string) (ostr string, err error) {

	return
}
