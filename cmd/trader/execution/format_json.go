package execution

import (
	"encoding/json"
	"io"

	svcexecution "github.com/rustyeddy/trader/service/execution"
)

// jsonFormatter renders a stable, structured JSON document per
// response, converting domain values into small, unexported view
// types defined in this file — the same convention cmd/trader/data
// and cmd/trader/broker's own jsonFormatter establish, duplicated here
// rather than shared across command-family packages (issue #201).
type jsonFormatter struct{}

func (jsonFormatter) encode(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

type violationView struct {
	Rule     string `json:"rule"`
	Message  string `json:"message"`
	Measured string `json:"measured"`
	Limit    string `json:"limit"`
}

type warningView struct {
	Rule    string `json:"rule"`
	Message string `json:"message"`
}

type decisionView struct {
	Allowed    bool            `json:"allowed"`
	Violations []violationView `json:"violations"`
	Warnings   []warningView   `json:"warnings"`
}

func toDecisionView(d svcexecution.SubmitResponse) decisionView {
	violations := make([]violationView, 0, len(d.Decision.Violations))
	for _, v := range d.Decision.Violations {
		violations = append(violations, violationView{Rule: v.Rule, Message: v.Message, Measured: v.Measured, Limit: v.Limit})
	}
	warnings := make([]warningView, 0, len(d.Decision.Warnings))
	for _, w := range d.Decision.Warnings {
		warnings = append(warnings, warningView{Rule: w.Rule, Message: w.Message})
	}
	return decisionView{Allowed: d.Decision.Allowed, Violations: violations, Warnings: warnings}
}

type proposalView struct {
	Instrument string `json:"instrument"`
	Side       string `json:"side"`
	Quantity   string `json:"quantity"`
}

func toProposalView(resp svcexecution.SubmitResponse) proposalView {
	p := resp.Proposal
	return proposalView{Instrument: p.Listing.Symbol(), Side: p.Side.String(), Quantity: p.Quantity.String()}
}

type requestView struct {
	OrderID     string `json:"order_id,omitempty"`
	Type        string `json:"type,omitempty"`
	TimeInForce string `json:"time_in_force,omitempty"`
}

func toRequestView(resp svcexecution.SubmitResponse) requestView {
	if !resp.Decision.Allowed {
		return requestView{}
	}
	r := resp.Request
	return requestView{OrderID: r.OrderID.String(), Type: r.Type.String(), TimeInForce: r.TimeInForce.String()}
}

type evaluateView struct {
	Proposal proposalView `json:"proposal"`
	Decision decisionView `json:"decision"`
	Request  requestView  `json:"request"`
}

func (j jsonFormatter) FormatEvaluate(w io.Writer, resp svcexecution.SubmitResponse) error {
	return j.encode(w, evaluateView{
		Proposal: toProposalView(resp),
		Decision: toDecisionView(resp),
		Request:  toRequestView(resp),
	})
}

type orderView struct {
	BrokerOrderID  string `json:"broker_order_id,omitempty"`
	Status         string `json:"status,omitempty"`
	FilledQuantity string `json:"filled_quantity,omitempty"`
}

func toOrderView(resp svcexecution.SubmitResponse) orderView {
	if !resp.Decision.Allowed {
		return orderView{}
	}
	o := resp.Order
	return orderView{BrokerOrderID: o.BrokerOrderID, Status: o.Status.String(), FilledQuantity: o.FilledQuantity.String()}
}

type submitView struct {
	Proposal proposalView `json:"proposal"`
	Decision decisionView `json:"decision"`
	Request  requestView  `json:"request"`
	Order    orderView    `json:"order"`
}

func (j jsonFormatter) FormatSubmit(w io.Writer, resp svcexecution.SubmitResponse) error {
	return j.encode(w, submitView{
		Proposal: toProposalView(resp),
		Decision: toDecisionView(resp),
		Request:  toRequestView(resp),
		Order:    toOrderView(resp),
	})
}
