package market

// Data-source identifiers for the Source field of a candle set. They name the
// upstream origin of candle data and are shared by the candle layer (here) and
// the store/marketdata layer that consumes it.
const (
	SourceDukascopy = "dukascopy"
	SourceOanda     = "oanda"
	SourceCandles   = "candles"
)
