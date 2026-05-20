// Package data defines the Provider interface implemented by every
// market-data source (Dukascopy, OANDA, future Polygon/IBKR, etc.).
//
// This package is deliberately free of trader-package types so it can be
// imported anywhere without circular dependency. Providers translate
// these generic params into their own internal types.
package data

import "time"

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
