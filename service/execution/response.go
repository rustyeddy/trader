package execution

import (
	"github.com/rustyeddy/trader/order"
	"github.com/rustyeddy/trader/risk"
)

// SubmitResponse is the structured result of the Submit use case,
// mirroring the meaningful portions of pipeline.Result. Proposal is
// populated once planning succeeds, even if risk later rejects it or
// broker submission fails. Decision is populated once risk evaluation
// completes, even on rejection. Order is populated only after a
// successful broker submission. These are Trader domain values, not a
// CLI- or JSON-specific DTO shape — see the package doc comment.
type SubmitResponse struct {
	Proposal order.Proposal
	Decision risk.Decision
	Order    order.Order
}
