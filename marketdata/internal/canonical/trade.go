package canonical

import (
	"fmt"
	"strings"
	"time"

	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
)

// PriceBasisTrade is the price basis of a canonical-trade-v1 file: traded prices.
const PriceBasisTrade = "trade"

// VolumeShares is the volume unit of a canonical-trade-v1 file: whole shares.
const VolumeShares = "shares"

// Adjustment says how a source rewrote past prices for corporate actions.
type Adjustment string

// The adjustments (ADR-095).
const (
	// AdjustmentTotalReturn: adjusted for splits and dividends.
	AdjustmentTotalReturn Adjustment = "total-return"
	// AdjustmentSplit: adjusted for splits only.
	AdjustmentSplit Adjustment = "split"
	// AdjustmentUnadjusted: the prices as traded.
	AdjustmentUnadjusted Adjustment = "unadjusted"
	// AdjustmentUnknown: not established.
	AdjustmentUnknown Adjustment = "unknown"
)

// Evidence says how the adjustment of a source is known.
type Evidence string

// The kinds of evidence.
const (
	// EvidenceObserved: inferred by comparing data.
	EvidenceObserved Evidence = "observed"
	// EvidenceDocumented: stated by the source.
	EvidenceDocumented Evidence = "documented"
)

// TradeMeta is what a canonical-trade-v1 file records about its data beyond the
// build. Every field is required; the builder sets them and a reader never infers
// one. It is comparable, and all of it is part of Meta.SameBuild.
type TradeMeta struct {
	// PriceBasis is PriceBasisTrade.
	PriceBasis string
	// Currency is the quote currency of the prices, such as "USD".
	Currency string
	// VolumeUnit is VolumeShares.
	VolumeUnit string
	// Adjustment and AdjustmentEvidence say how past prices were rewritten and
	// how that is known.
	Adjustment         Adjustment
	AdjustmentEvidence Evidence
	// SnapshotFingerprint identifies the exact bytes of the one whole-history
	// input the series came from ("sha256:" and 64 lower-case hex digits). The
	// months of one series share it.
	SnapshotFingerprint string
	// SnapshotAsOf is the date those bytes were obtained, as YYYY-MM-DD.
	SnapshotAsOf string
	// Calendar names the date table the bar times come from, such as
	// CalendarUSEquityV2.
	Calendar string
}

// validFingerprint reports whether s is "sha256:" and 64 lower-case hex digits.
func validFingerprint(s string) bool {
	h, ok := strings.CutPrefix(s, "sha256:")
	return ok && len(h) == 64 && strings.Trim(h, "0123456789abcdef") == ""
}

// checkTradeMeta holds the file-level rules of canonical-trade-v1.
func (f File) checkTradeMeta() error {
	m := f.Meta.Trade
	bad := func(format string, args ...any) error {
		return fmt.Errorf("%w: %s: %s", ErrMalformedData, SchemaTrade, fmt.Sprintf(format, args...))
	}
	if f.Key.Interval != D1 {
		return bad("only d1 is supported, not %q", string(f.Key.Interval))
	}
	if f.Meta.AvailabilityPolicy != PolicyUSEquityCloseV1 {
		return bad("availability policy %q, want %s", f.Meta.AvailabilityPolicy, PolicyUSEquityCloseV1)
	}
	if m.Calendar != CalendarUSEquityV2 {
		return bad("calendar %q, want %s", m.Calendar, CalendarUSEquityV2)
	}
	if m.PriceBasis != PriceBasisTrade {
		return bad("price basis %q, want %s", m.PriceBasis, PriceBasisTrade)
	}
	if _, err := num.ParseCurrency(m.Currency); err != nil {
		return bad("currency %q: %v", m.Currency, err)
	}
	if m.VolumeUnit != VolumeShares {
		return bad("volume unit %q, want %s", m.VolumeUnit, VolumeShares)
	}
	switch m.Adjustment {
	case AdjustmentTotalReturn, AdjustmentSplit, AdjustmentUnadjusted, AdjustmentUnknown:
	default:
		return bad("adjustment %q", string(m.Adjustment))
	}
	switch m.AdjustmentEvidence {
	case EvidenceObserved, EvidenceDocumented:
	default:
		return bad("adjustment evidence %q", string(m.AdjustmentEvidence))
	}
	if !validFingerprint(m.SnapshotFingerprint) {
		return bad("snapshot fingerprint %q is not sha256:<64 hex digits>", m.SnapshotFingerprint)
	}
	if _, err := time.Parse(time.DateOnly, m.SnapshotAsOf); err != nil {
		return bad("snapshot as-of %q is not a YYYY-MM-DD date", m.SnapshotAsOf)
	}
	return nil
}

// checkTradeBar holds the rules of canonical-trade-v1 for one bar: the trade
// series alone, opening and available when its session does.
func (f File) checkTradeBar(b marketdata.Bar) error {
	at := b.Time.UTC().Format(time.RFC3339)
	if !b.Bid.IsZero() || !b.Ask.IsZero() || !b.Mid.IsZero() {
		return fmt.Errorf("%w: bar %s has a bid, ask or mid series, which %s does not store", ErrMalformedData, at, SchemaTrade)
	}
	s, err := USEquitySessionAt(b.Time)
	if err != nil {
		return fmt.Errorf("%w: bar %s: %w", ErrMalformedData, at, err)
	}
	if !b.AvailableAt.Equal(s.Close) {
		return fmt.Errorf("%w: bar %s is available at %s, but %s gives %s", ErrMalformedData, at,
			b.AvailableAt.UTC().Format(time.RFC3339), PolicyUSEquityCloseV1, s.Close.Format(time.RFC3339))
	}
	return nil
}
