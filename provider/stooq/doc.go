// Package stooq reads Stooq's daily US data in its native format: the member of a
// bulk archive (d_us_txt.zip) or the same bytes extracted from it. It parses and
// maps only, into the marketdata.SourcePartition the market-data service converts
// to canonical bars (ADR-095); it rejects nothing for market reasons, sets no
// opening or availability time and writes nothing.
//
// The format is a header line and one row per day:
//
//	<TICKER>,<PER>,<DATE>,<TIME>,<OPEN>,<HIGH>,<LOW>,<CLOSE>,<VOL>,<OPENINT>
//	SPY.US,D,20050225,000000,92.7949,93.8716,92.7097,93.6948,79289004,0
//
// Stooq's other format, the single-symbol "Date,Open,High,Low,Close,Volume"
// download, is not read. Files are read from the data root through the store.
// Nothing here uses the network.
package stooq
