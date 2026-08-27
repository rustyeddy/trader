package execution

import (
	"github.com/rustyeddy/trader/order"
	"github.com/rustyeddy/trader/risk"
)

// SubmitResponse is the structured result of the Evaluate/Submit use
// cases, mirroring the meaningful portions of pipeline.Result.
// Proposal is populated once planning succeeds, even if risk later
// rejects it or broker submission fails. Decision is populated once
// risk evaluation completes, even on rejection. Request is populated
// only once risk approves — the approved, broker-neutral order.Request
// Evaluate/Submit both build; Evaluate never goes further than this.
// Order is populated only after a successful broker submission
// (Submit only — Evaluate never mutates the broker, so its own
// SubmitResponse.Order always stays zero). These are Trader domain
// values, not a CLI- or JSON-specific DTO shape — see the package doc
// comment.
type SubmitResponse struct {
	Proposal order.Proposal
	Decision risk.Decision
	Request  order.Request
	Order    order.Order
}
