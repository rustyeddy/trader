package market

import "time"

// Migration shims: these export internal helpers that the root trader package
// still references directly across the new package boundary. They exist only to
// keep the package split mechanical and additive (no edits to the moved files).
//
// As callers are migrated, prefer moving each helper to its proper home (e.g.
// bit helpers with the candle family, time-range/price-parse helpers with the
// store/marketdata package) rather than cementing these exports. Remove entries
// here once nothing in root depends on them. See docs/pkg-migration.org.

// TimeMillis is the exported alias for the internal millisecond duration type.
type TimeMillis = timemilli

// Fixed-point math helpers.
func MulDivCeil64(a, b, den int64) (int64, error)         { return mulDivCeil64(a, b, den) }
func MulDivFloor64(a, b, den int64) (int64, error)        { return mulDivFloor64(a, b, den) }
func MulChecked64(a, b int64) (int64, error)              { return mulChecked64(a, b) }
func AbsInt64Checked(v int64) (int64, error)              { return absInt64Checked(v) }
func RoundHalfAwayFromZero(num, den int64) (int64, error) { return roundHalfAwayFromZero(num, den) }
func SignedMulDivRound(a, b, den int64) (int64, error)    { return signedMulDivRound(a, b, den) }

// Bit-set helpers (used by candle spread masks).
func BitSet(bits []uint64, i int)        { bitSet(bits, i) }
func BitIsSet(bits []uint64, i int) bool { return bitIsSet(bits, i) }

// Time-range and price-parsing helpers.
func TimeRangeFromStrings(fromStr, toStr, tfstr string) (TimeRange, error) {
	return timeRangeFromStrings(fromStr, toStr, tfstr)
}
func NewTimeRange(start, end Timestamp, tf Timeframe) TimeRange { return newTimeRange(start, end, tf) }
func MonthRange(year, month int) TimeRange                      { return monthRange(year, month) }
func ParseRawPrice(s string) (Price, error)                     { return parseRawPrice(s) }

// Misc formatting/time helpers still used by root.
func FormatScaledPrice(price Price, scale int32) string { return formatScaledPrice(price, scale) }
func TimeMilliFromTime(t time.Time) TimeMillis          { return timeMilliFromTime(t) }
func IsFXMarketClosed(t time.Time) bool                 { return isFXMarketClosed(t) }
