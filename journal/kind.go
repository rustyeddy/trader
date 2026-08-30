package journal

import "fmt"

// Kind identifies which payload field of a Record/Entry is populated.
// Like broker.EventKind, this is Trader's own closed vocabulary, not
// broker- or strategy-reported state, so the zero value is reserved as
// invalid/unset.
type Kind uint8

const (
	// KindUnknown is Kind's zero value. It is never a valid Record.Kind.
	KindUnknown Kind = iota
	// KindRunStarted marks the first record of a run's journal.
	// Record.RunStarted is populated.
	KindRunStarted
	// KindIntent reports a strategy-emitted order.Intent.
	// Record.Intent is populated.
	KindIntent
	// KindProposal reports an execution.Planner-produced order.Proposal.
	// Record.Proposal is populated.
	KindProposal
	// KindDecision reports a risk.Engine decision, allowed or rejected.
	// Record.Decision is populated.
	KindDecision
	// KindRequest reports a risk-approved, broker-neutral order.Request.
	// Record.Request is populated.
	KindRequest
	// KindOrder reports an authoritative order.Order lifecycle change,
	// as observed from the broker's own event stream — never derived
	// from a pipeline.Result, so it is never journaled twice for the
	// same underlying broker acceptance. Record.Order is populated.
	KindOrder
	// KindFill reports one authoritative execution against an order, as
	// observed from the broker's own event stream. Record.Fill is
	// populated.
	KindFill
	// KindAccount reports an authoritative account.Snapshot change, as
	// observed from the broker's own event stream. Record.Account is
	// populated.
	KindAccount
	// KindStatus reports a broker session/account operational status
	// change, as observed from the broker's own event stream.
	// Record.Status is populated.
	KindStatus
	// KindTrade reports one derived order.Trade (backtest.DeriveTrades).
	// Record.Trade is populated.
	KindTrade
	// KindRunCompleted marks the last record of a run's journal,
	// written only once the run finished successfully.
	// Record.RunCompleted is populated.
	KindRunCompleted
)

// String returns a human-readable Kind name.
func (k Kind) String() string {
	switch k {
	case KindRunStarted:
		return "run-started"
	case KindIntent:
		return "intent"
	case KindProposal:
		return "proposal"
	case KindDecision:
		return "decision"
	case KindRequest:
		return "request"
	case KindOrder:
		return "order"
	case KindFill:
		return "fill"
	case KindAccount:
		return "account"
	case KindStatus:
		return "status"
	case KindTrade:
		return "trade"
	case KindRunCompleted:
		return "run-completed"
	default:
		return fmt.Sprintf("Kind(%d)", uint8(k))
	}
}

func (k Kind) valid() bool {
	switch k {
	case KindRunStarted, KindIntent, KindProposal, KindDecision, KindRequest,
		KindOrder, KindFill, KindAccount, KindStatus, KindTrade, KindRunCompleted:
		return true
	default:
		return false
	}
}
