package backtest

import (
	"context"

	"github.com/rustyeddy/trader/journal"
)

func RecordBacktest(ctx context.Context, btr BacktestRun) error {

	return nil
}

func GetBacktestRun(ctx context.Context, runID string) (btr BacktestRun, err error) {

	return
}

func ListTradesByRunID(ctx context.Context, runID string) (tr []journal.TradeRecord, err error) {

	return
}

func ListEquityByRunID(ctx context.Context, runID string) (eq []journal.EquitySnapshot, err error) {

	return
}

// ExportBacktestOrg loads everything and returns the Org block.
func ExportBacktestOrg(ctx context.Context, runID string) (ostr string, err error) {

	return
}
