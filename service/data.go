package service

import (
	"context"

	"github.com/rustyeddy/trader/datamanager"
	datasvc "github.com/rustyeddy/trader/service/data"
)

// dataSvc constructs a fresh datasvc.Service from the current field values
// on every call — see backtestSvc's doc comment in service/backtest.go for
// why this isn't cached/embedded.
func (s *Service) dataSvc() *datasvc.Service {
	return &datasvc.Service{OANDA: s.OANDA}
}

// DownloadOandaCandles fetches OANDA candles and writes them to the local
// store. See service/data.Service.DownloadOandaCandles.
func (s *Service) DownloadOandaCandles(ctx context.Context, req DownloadOandaCandlesRequest) (*DownloadOandaCandlesResult, error) {
	return s.dataSvc().DownloadOandaCandles(ctx, req)
}

// DeriveCanonicalFromRaw derives canonical candles from a raw month CSV. See
// service/data.Service.DeriveCanonicalFromRaw.
func (s *Service) DeriveCanonicalFromRaw(ctx context.Context, rawPath string, key datamanager.Key) (*DeriveResult, error) {
	return s.dataSvc().DeriveCanonicalFromRaw(ctx, rawPath, key)
}

// UpdateOandaCandles runs a catch-up download for one or more
// instrument/timeframe pairs. See service/data.Service.UpdateOandaCandles.
func (s *Service) UpdateOandaCandles(ctx context.Context, req UpdateOandaCandlesRequest) (*UpdateOandaCandlesResult, error) {
	return s.dataSvc().UpdateOandaCandles(ctx, req)
}

// DataStats computes candle statistics over a date range. See
// service/data.Service.DataStats.
func (s *Service) DataStats(ctx context.Context, req DataStatsRequest) (*DataStatsResult, error) {
	return s.dataSvc().DataStats(ctx, req)
}

// ValidateCandleData validates stored candle months. See
// service/data.Service.ValidateCandleData.
func (s *Service) ValidateCandleData(ctx context.Context, req ValidateCandleDataRequest) (*datamanager.CandleValidationReport, error) {
	return s.dataSvc().ValidateCandleData(ctx, req)
}

// CandlesCSV reads local candles and returns them as canonical candle CSV.
// See service/data.Service.CandlesCSV.
func (s *Service) CandlesCSV(ctx context.Context, req CandlesCSVRequest) (*CandlesCSVResult, error) {
	return s.dataSvc().CandlesCSV(ctx, req)
}

// Types re-exported as aliases so existing call sites
// (service.DownloadOandaCandlesRequest{...} etc.) keep compiling unchanged
// while the implementation lives in service/data.
type (
	DownloadOandaCandlesRequest = datasvc.DownloadOandaCandlesRequest
	DownloadOandaCandlesResult  = datasvc.DownloadOandaCandlesResult
	DeriveResult                = datasvc.DeriveResult
	UpdateOandaCandlesRequest   = datasvc.UpdateOandaCandlesRequest
	UpdateOandaCandlesResult    = datasvc.UpdateOandaCandlesResult
	UpdateItemResult            = datasvc.UpdateItemResult
	DataStatsRequest            = datasvc.DataStatsRequest
	DataStatsResult             = datasvc.DataStatsResult
	AnalyzerResult              = datasvc.AnalyzerResult
	StatRow                     = datasvc.StatRow
	ValidateCandleDataRequest   = datasvc.ValidateCandleDataRequest
	CandlesCSVRequest           = datasvc.CandlesCSVRequest
	CandlesCSVResult            = datasvc.CandlesCSVResult
	CandleCSVMetadata           = datasvc.CandleCSVMetadata
)

// Free functions with no Service-scoped state, re-exported directly.
var (
	ParseTraderTimeframe = datasvc.ParseTraderTimeframe
	ToOandaGranularity   = datasvc.ToOandaGranularity
	WriteCandlesCSV      = datasvc.WriteCandlesCSV
)

// parseTraderTimeframe/toOandaGranularity: unexported aliases so the
// remaining core-service files (review.go until it moves to service/review,
// portfolio_config.go) that call the old lowercase names keep compiling
// unchanged.
var (
	parseTraderTimeframe = datasvc.ParseTraderTimeframe
	toOandaGranularity   = datasvc.ToOandaGranularity
)
