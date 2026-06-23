package trader

// Transition shim re-exporting the marketdata package (store + data layer,
// extracted from the root trader package) so existing callers — including
// service/cmd/api and data/dukascopy — compile unchanged during the migration.
// Remove entries as callers move to github.com/rustyeddy/trader/marketdata
// directly. See docs/pkg-migration.org.

import "github.com/rustyeddy/trader/marketdata"

// Store / data-manager types.
type (
	Store         = marketdata.Store
	DataManager   = marketdata.DataManager
	CandleRequest = marketdata.CandleRequest
	Key           = marketdata.Key
	DataKind      = marketdata.DataKind
	RawTick       = marketdata.RawTick

	// candle-set machinery (still used by root tests).
	candleSet         = marketdata.CandleSet
	candleSetIterator = marketdata.CandleSetIterator

	// data-statistics types.
	Analyzer = marketdata.Analyzer
	Stat     = marketdata.Stat

	// synthetic candle generation.
	SyntheticCandleConfig = marketdata.SyntheticCandleConfig

	// candle validation.
	CandleValidationRequest = marketdata.CandleValidationRequest
	CandleValidationReport  = marketdata.CandleValidationReport
	CandleValidationIssue   = marketdata.CandleValidationIssue
)

// DataKind values.
const (
	KindUnknown = marketdata.KindUnknown
	KindTick    = marketdata.KindTick
	KindCandle  = marketdata.KindCandle
)

// Store / data-manager constructors and globals.
var (
	NewDataManager            = marketdata.NewDataManager
	GetStore                  = marketdata.GetStore
	NewStoreAt                = marketdata.NewStoreAt
	SwapStore                 = marketdata.SwapStore
	SetDataDir                = marketdata.SetDataDir
	NewDownloader             = marketdata.NewDownloader
	RequiredTickHoursForMonth = marketdata.RequiredTickHoursForMonth
	newMonthlyCandleSet       = marketdata.NewMonthlyCandleSet

	GetDataManager       = marketdata.GetDataManager
	SlotMayHaveForexData = marketdata.SlotMayHaveForexData
	RawCandlePathAt      = marketdata.RawCandlePathAt
	BuildInventory       = marketdata.BuildInventory
	ValidateCandleData   = marketdata.ValidateCandleData

	// data-statistics analyzers.
	NewSwingAnalyzer   = marketdata.NewSwingAnalyzer
	NewSpreadAnalyzer  = marketdata.NewSpreadAnalyzer
	NewTrendAnalyzer   = marketdata.NewTrendAnalyzer
	NewSessionAnalyzer = marketdata.NewSessionAnalyzer
	RunAnalysis        = marketdata.RunAnalysis

	// synthetic candle generation helpers.
	DefaultSyntheticConfig        = marketdata.DefaultSyntheticConfig
	GenerateSyntheticYearTestData = marketdata.GenerateSyntheticYearTestData
)
