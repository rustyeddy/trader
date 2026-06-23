package indicator

// Migration shim: exposes the internal fixed-point rounding helper that the
// root trader package (ChandelierExit) still calls across the new package
// boundary. Remove once that caller no longer needs it (e.g. when the helper
// moves to market alongside the other rounding primitives). See
// docs/pkg-migration.org.

// RoundDivPositive performs integer division of num by den with round-half-up.
func RoundDivPositive(num, den int64) int64 { return roundDivPositive(num, den) }
