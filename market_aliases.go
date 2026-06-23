package trader

// This file re-exports the primitives that were extracted into the market
// package, so existing callers in the root trader package (and downstream
// packages) keep compiling unchanged during the package migration.
//
// It is a temporary transition shim. As callers are migrated to reference
// github.com/rustyeddy/trader/market directly, the corresponding entries here
// should be deleted, and the whole file removed once the last caller is gone.
// See docs/pkg-migration.org.

import "github.com/rustyeddy/trader/market"

// Fixed-point and market domain types.
type (
	Price      = market.Price
	PriceSum   = market.PriceSum
	Money      = market.Money
	Rate       = market.Rate
	Units      = market.Units
	Pips       = market.Pips
	Scale6     = market.Scale6
	Scale7     = market.Scale7
	Side       = market.Side
	Tick       = market.Tick
	BA         = market.BA
	Timestamp  = market.Timestamp
	TimeRange  = market.TimeRange
	Timeframe  = market.Timeframe
	AssetClass = market.AssetClass
	Instrument = market.Instrument

	Candle         = market.Candle
	CandleTime     = market.CandleTime
	CandleIterator = market.CandleIterator
)

// Data-source identifiers.
const (
	SourceDukascopy = market.SourceDukascopy
	SourceOanda     = market.SourceOanda
	SourceCandles   = market.SourceCandles
)

// Scales and other numeric constants.
const (
	PriceScale = market.PriceScale
	MoneyScale = market.MoneyScale
	RateScale  = market.RateScale
	UnitsScale = market.UnitsScale
)

// Side values.
const (
	Long  = market.Long
	Short = market.Short
)

// Timeframe values.
const (
	TF0 = market.TF0
	M1  = market.M1
	H1  = market.H1
	H4  = market.H4
	D1  = market.D1
)

// Time duration constants.
const (
	SecondInMS  = market.SecondInMS
	MinuteInMS  = market.MinuteInMS
	HourInMS    = market.HourInMS
	MinuteInSec = market.MinuteInSec
	HourInSec   = market.HourInSec
	Ticks       = market.Ticks
)

// Asset class values.
const (
	AssetForex   = market.AssetForex
	AssetEquity  = market.AssetEquity
	AssetFutures = market.AssetFutures
)

// Package-level vars.
var ErrTickNotFound = market.ErrTickNotFound

// Internal helpers still referenced by root files across the package boundary.
// These mirror the additive exports in market/migration_exports.go and keep the
// lowercase call sites in root unchanged during the migration.
type (
	timemilli  = market.TimeMillis
	candleTime = market.CandleTime
)

var (
	mulDivCeil64          = market.MulDivCeil64
	mulDivFloor64         = market.MulDivFloor64
	mulChecked64          = market.MulChecked64
	absInt64Checked       = market.AbsInt64Checked
	roundHalfAwayFromZero = market.RoundHalfAwayFromZero
	signedMulDivRound     = market.SignedMulDivRound
	bitSet                = market.BitSet
	bitIsSet              = market.BitIsSet
	isForexMarketClosed   = market.IsForexMarketClosed
	timeRangeFromStrings  = market.TimeRangeFromStrings
	newTimeRange          = market.NewTimeRange
	monthRange            = market.MonthRange
	parseRawPrice         = market.ParseRawPrice
	timeMilliFromTime     = market.TimeMilliFromTime
	isFXMarketClosed      = market.IsFXMarketClosed
)

// Constructors and helpers.
var (
	PriceFromFloat        = market.PriceFromFloat
	MoneyFromFloat        = market.MoneyFromFloat
	RateFromFloat         = market.RateFromFloat
	UnitsFromFloat        = market.UnitsFromFloat
	PipsFromFloat         = market.PipsFromFloat
	AvgSpreadPips         = market.AvgSpreadPips
	FromTime              = market.FromTime
	FromString            = market.FromString
	ParseDateTimestamp    = market.ParseDateTimestamp
	ParseTimeRange        = market.ParseTimeRange
	ParseTimeframe        = market.ParseTimeframe
	IsForexMarketClosed   = market.IsForexMarketClosed
	NewULID               = market.NewULID
	ShortDisplayID        = market.ShortDisplayID
	NormalizeInstrument   = market.NormalizeInstrument
	LookupInstrument      = market.LookupInstrument
	GetInstrument         = market.GetInstrument
	MajorInstruments      = market.MajorInstruments
	ApproximateUSDPerUnit = market.ApproximateUSDPerUnit
)
