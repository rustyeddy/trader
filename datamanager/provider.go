// Package data defines the Provider interface implemented by every
// market-data source (Dukascopy, OANDA, future Polygon/IBKR, etc.).
//
// This package is deliberately free of trader-package types so it can be
// imported anywhere without circular dependency. Providers translate
// these generic params into their own internal types.
package datamanager

import (
	"context"
	"time"

	"github.com/rustyeddy/trader/market"
)

// Provider knows how to fetch raw data from a specific market data source.
// Implementations live in source-specific subpackages and register
// themselves via init() with the package-level registry.
type Provider interface {
	Name() string

	// SourceURL returns the HTTP URL to download the raw data unit
	// identified by the params. For file-based providers like Dukascopy
	// this is a static file URL. For API-based providers like OANDA this
	// is a REST endpoint with query parameters baked in.
	SourceURL(p SourceParams) string
}

// SourceParams identifies a single data slice to download. Providers
// interpret the fields as appropriate (file-based providers use the time
// fields directly, API providers may use From/To range fields).
type SourceParams struct {
	Instrument string
	Time       time.Time // anchor time for the data slice
	Timeframe  string    // "tick", "M1", "H1", etc.
}

// CandleProvider is the acquisition boundary for sources that return
// finished candles directly (e.g. OANDA's REST API), as opposed to Provider,
// which downloads raw files for DataManager to aggregate locally (e.g.
// Dukascopy tick files). Unlike Provider, implementations typically need
// runtime credentials, so they are constructed explicitly by the caller
// (service layer) rather than self-registering in init().
type CandleProvider interface {
	Name() string

	// FetchCandleMonth returns one calendar month of candles for instrument
	// at the given timeframe, in provider-native units (bid OHLC). The
	// returned slice is dense: one slot per timeframe step in the month,
	// with zero-valued candles for slots the provider had no data for.
	FetchCandleMonth(ctx context.Context, instrument string, tf market.Timeframe, monthStart time.Time) (*CandleMonth, error)
}

// CandleMonth is the result of a CandleProvider month fetch.
type CandleMonth struct {
	// Candles is dense: one slot per timeframe step in the month.
	Candles []market.Candle

	// Raw optionally preserves the provider's native bid+ask OHLC rows
	// (before conversion to the canonical bid-only representation), for
	// callers that want to keep a raw-source archive. Nil if the provider
	// doesn't support raw preservation.
	Raw []RawCandleRow
}

// RawCandleRow is one provider-native bid+ask OHLC row, prior to conversion
// into the canonical candle representation.
type RawCandleRow struct {
	Time                               time.Time
	BidOpen, BidHigh, BidLow, BidClose float64
	AskOpen, AskHigh, AskLow, AskClose float64
	Volume                             int
	Complete                           bool
}

// ProviderStore is the narrow, read-mostly slice of store functionality
// exposed to Provider implementations (e.g. datamanager/dukascopy) that need
// to check for or save already-downloaded raw files. It deliberately omits
// canonical candle writes — those stay internal to DataManager's own build
// pipeline.
type ProviderStore interface {
	Exists(k Key) (bool, error)
	PathForAsset(k Key) (string, error)
	OpenTickIterator(key Key) (iterator[RawTick], error)
}

// ForProviders returns the narrow store accessor available to Provider
// implementations. Nothing outside datamanager (and its provider
// subpackages) may access the store any other way.
func ForProviders() ProviderStore {
	return getStore()
}
