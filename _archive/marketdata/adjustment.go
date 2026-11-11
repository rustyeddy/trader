package marketdata

import "fmt"

// AdjustmentPolicy records what corporate-action adjustment convention
// a dataset's OHLC prices already reflect (issue #298, EQ-05). It
// exists because equities, unlike FX, are subject to stock splits and
// dividends: two datasets for the same instrument/interval/span that
// differ only in adjustment convention are materially different data,
// not interchangeable revisions of the same thing, and this field is
// how that difference is made explicit, reproducible metadata rather
// than an implied provider default (issue #298's own acceptance
// criterion).
//
// AdjustmentPolicy is recorded, not selected: Trader does not compute
// or offer query-time adjustment (the architecture document's fuller
// AdjustmentMode sketch — Raw/SplitAdjusted/TotalReturnAdjusted/
// BackAdjusted, selected per query), which ADR-047 explicitly defers
// until a concrete multi-mode consumer exists. A canonical dataset
// carries exactly the one convention its source provider delivered.
type AdjustmentPolicy uint8

const (
	// AdjustmentUnknown is AdjustmentPolicy's zero value. Reserving
	// zero for an unset policy means an uninitialized Manifest can
	// never be silently read as describing adjusted (or unadjusted)
	// data.
	AdjustmentUnknown AdjustmentPolicy = iota
	// AdjustmentNotApplicable means the instrument class has no
	// corporate-action adjustment concept at all — FX pairs are never
	// split or paid a dividend. M2's canonical FX bars (oanda) use
	// this value.
	AdjustmentNotApplicable
	// AdjustmentUnadjusted means prices are exactly as originally
	// printed on their trade date, with no retroactive adjustment for
	// any later split or dividend.
	AdjustmentUnadjusted
	// AdjustmentSplitAdjusted means historical prices have been
	// retroactively adjusted for stock splits (so a share count/price
	// series stays continuous across a split date) but not for
	// dividends. Alpaca's historical bars use this convention (its
	// requests ask for split adjustment only). Stooq's history is
	// split-adjusted too, but also dividend-adjusted, so it records
	// AdjustmentTotalReturn instead (ADR-073). The split evidence is
	// AAPL's real 2014 (7-for-1) and 2020 (4-for-1) splits, both of
	// which show continuous pricing with no discontinuity on the split
	// date (issue #298).
	AdjustmentSplitAdjusted
	// AdjustmentTotalReturn means historical prices are adjusted for
	// both splits and dividends (a total-return series): every split and
	// every ex-dividend date rewrites all older prices. Stooq's daily
	// equity history uses this convention (ADR-073, issue #464), which
	// is why its price levels are lower than a split-adjusted-only
	// source's by roughly the dividends paid since, and why the two must
	// not be spliced into one series.
	AdjustmentTotalReturn
)

// String returns a human-readable AdjustmentPolicy name.
func (a AdjustmentPolicy) String() string {
	switch a {
	case AdjustmentUnknown:
		return "unknown"
	case AdjustmentNotApplicable:
		return "not_applicable"
	case AdjustmentUnadjusted:
		return "unadjusted"
	case AdjustmentSplitAdjusted:
		return "split_adjusted"
	case AdjustmentTotalReturn:
		return "total_return"
	default:
		return fmt.Sprintf("AdjustmentPolicy(%d)", uint8(a))
	}
}

// valid reports whether a names a defined, usable policy (not the zero
// value).
func (a AdjustmentPolicy) valid() bool {
	switch a {
	case AdjustmentNotApplicable, AdjustmentUnadjusted, AdjustmentSplitAdjusted, AdjustmentTotalReturn:
		return true
	default:
		return false
	}
}
