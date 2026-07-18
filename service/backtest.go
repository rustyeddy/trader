package service

import (
	"context"

	"github.com/rustyeddy/trader/backtest"
	backtestsvc "github.com/rustyeddy/trader/service/backtest"
)

// backtestSvc constructs a fresh backtestsvc.Service from the current field
// values on every call (not cached) so it always reflects whatever s.Log /
// s.Backtests hold at call time — including when s was built via a bare
// &Service{Log: l} struct literal (used throughout cmd/ and tests) rather
// than New(), which a cached/embedded copy would not pick up.
func (s *Service) backtestSvc() *backtestsvc.Service {
	return &backtestsvc.Service{Executor: s.Backtests, Log: s.Log}
}

// RunBacktest executes one compiled backtest definition end-to-end and
// returns the rendered summary. See service/backtest.Service.RunBacktest.
func (s *Service) RunBacktest(ctx context.Context, compiled backtest.CompiledBacktest) (backtest.BacktestReportSummary, error) {
	return s.backtestSvc().RunBacktest(ctx, compiled)
}

// RunBacktestConfigs loads and executes a slice of YAML config files. See
// service/backtest.Service.RunBacktestConfigs.
func (s *Service) RunBacktestConfigs(ctx context.Context, configPaths []string) ([]backtest.BacktestReportSummary, error) {
	return s.backtestSvc().RunBacktestConfigs(ctx, configPaths)
}

// RunBacktestPathSpecs resolves config path specs and executes them. See
// service/backtest.Service.RunBacktestPathSpecs.
func (s *Service) RunBacktestPathSpecs(ctx context.Context, pathSpecs []string) ([]backtest.BacktestReportSummary, error) {
	return s.backtestSvc().RunBacktestPathSpecs(ctx, pathSpecs)
}

// RunBacktestConfigsAndWriteReports executes the given configs and persists
// report artifacts. See service/backtest.Service.RunBacktestConfigsAndWriteReports.
func (s *Service) RunBacktestConfigsAndWriteReports(ctx context.Context, configPaths []string, outDir string) ([]backtest.BacktestReportSummary, error) {
	return s.backtestSvc().RunBacktestConfigsAndWriteReports(ctx, configPaths, outDir)
}

// RunBacktestPathSpecsAndWriteReports resolves config path specs, executes
// them, and persists report artifacts. See
// service/backtest.Service.RunBacktestPathSpecsAndWriteReports.
func (s *Service) RunBacktestPathSpecsAndWriteReports(ctx context.Context, pathSpecs []string, outDir string) ([]backtest.BacktestReportSummary, error) {
	return s.backtestSvc().RunBacktestPathSpecsAndWriteReports(ctx, pathSpecs, outDir)
}

// The functions below have no Service-scoped state (no Log/Executor
// dependency) and are re-exported directly as aliases so existing call
// sites (service.ListBacktestSummaries(...) etc.) keep compiling unchanged
// while the implementation lives in service/backtest.
var (
	ResolveBacktestConfigPaths = backtestsvc.ResolveBacktestConfigPaths
	WriteBacktestReports       = backtestsvc.WriteBacktestReports
	WriteBacktestSummaryJSON   = backtestsvc.WriteBacktestSummaryJSON
	WriteBacktestSummaryOrg    = backtestsvc.WriteBacktestSummaryOrg
	RebuildBacktestIndex       = backtestsvc.RebuildBacktestIndex
	ListBacktestSummaries      = backtestsvc.ListBacktestSummaries
	ReadBacktestSummaryFile    = backtestsvc.ReadBacktestSummaryFile
	ReadBacktestSummaryByName  = backtestsvc.ReadBacktestSummaryByName
	ListBacktestOrgReports     = backtestsvc.ListBacktestOrgReports
	ReadBacktestOrgReport      = backtestsvc.ReadBacktestOrgReport
)
