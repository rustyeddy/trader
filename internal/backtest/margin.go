package backtest

import (
	"github.com/rustyeddy/trader/internal/account"
	"github.com/rustyeddy/trader/internal/account/margin"
	runtimeorder "github.com/rustyeddy/trader/internal/order"
	"github.com/rustyeddy/trader/internal/risk"
	"github.com/rustyeddy/trader/num"
)

// InitialMarginRatioParameter is the ComponentInfo parameter key under
// which a composition root records the initial-margin ratio in both
// the account_initial_margin risk rule's and the margin model's
// descriptors (ADR-066), and from which a report reads it back.
const InitialMarginRatioParameter = "initial_margin_ratio"

// MarginRejections counts a run's initial-margin refusals (ADR-066),
// split by where they happened.
type MarginRejections struct {
	// Admission counts risk decisions rejected by the
	// account_initial_margin rule.
	Admission int
	// Fill counts orders the broker refused at fill time for
	// insufficient margin: market orders rejected, and resting orders
	// canceled with that reason.
	Fill int
}

// Total is Admission + Fill.
func (m MarginRejections) Total() int { return m.Admission + m.Fill }

// isMarginAdmissionRejection reports whether d rejected a proposal
// under the account_initial_margin rule.
func isMarginAdmissionRejection(d risk.Decision) bool {
	if d.Allowed {
		return false
	}
	for _, v := range d.Violations {
		if v.Rule == risk.AccountInitialMarginRuleName {
			return true
		}
	}
	return false
}

// isMarginFillRejection reports whether o is a broker refusal for
// insufficient margin: a rejected order, or a broker-initiated cancel,
// carrying ReasonInsufficientMargin.
func isMarginFillRejection(o runtimeorder.Order) bool {
	switch o.Status {
	case runtimeorder.StatusRejected:
		return o.Rejection != nil && o.Rejection.Reason == runtimeorder.ReasonInsufficientMargin
	case runtimeorder.StatusCanceled:
		return o.CancelReason != nil && o.CancelReason.Reason == runtimeorder.ReasonInsufficientMargin
	default:
		return false
	}
}

// grossNotional returns s's gross position notional (ADR-066), each
// open position valued at its snapshot mark, or nil when any open
// position has no mark or the value can't be computed.
func grossNotional(s account.Snapshot) *num.Money {
	gross, err := num.ParseMoney("0", s.Currency())
	if err != nil {
		return nil
	}
	for _, p := range s.Positions() {
		if p.Quantity.IsZero() {
			continue
		}
		m, ok := s.Mark(account.KeyOf(p.Listing))
		if !ok {
			return nil
		}
		n, err := margin.Notional(margin.Valued{Listing: p.Listing, Quantity: p.Quantity, Price: m.Price}, s.Currency())
		if err != nil {
			return nil
		}
		if gross, err = gross.Add(n); err != nil {
			return nil
		}
	}
	return &gross
}
