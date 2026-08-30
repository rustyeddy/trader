package jsonl

import (
	"encoding/json"
	"time"

	"github.com/rustyeddy/trader/account"
	"github.com/rustyeddy/trader/broker"
	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/journal"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
	"github.com/rustyeddy/trader/risk"
)

// This file converts journal.Record/Entry values to and from a stable
// JSON wire shape, following the same "explicit tagged wire struct,
// never raw reflection over the domain type" discipline
// backtest/manifest.go already established. This matters for more
// than style: instrument.Listing and account.Snapshot both have
// entirely unexported fields (ADR-007/ADR-016), so a bare
// json.Marshal of any domain type reaching one — order.Proposal,
// order.Request, order.Order, order.Fill, order.Trade, and
// account.Snapshot itself — would silently encode as "{}", discarding
// exactly the audit-relevant identity. Every wire struct below reads
// through the domain type's own accessors/exported fields instead.

type specWire struct {
	TickSize           num.Price    `json:"tick_size"`
	QuantityIncrement  num.Quantity `json:"quantity_increment"`
	Multiplier         num.Rate     `json:"multiplier"`
	SettlementCurrency num.Currency `json:"settlement_currency"`
}

func toSpecWire(s instrument.Spec) specWire {
	return specWire{TickSize: s.TickSize(), QuantityIncrement: s.QuantityIncrement(), Multiplier: s.Multiplier(), SettlementCurrency: s.SettlementCurrency()}
}

type listingWire struct {
	InstrumentID string   `json:"instrument_id"`
	Provider     string   `json:"provider"`
	Venue        string   `json:"venue,omitempty"`
	Symbol       string   `json:"symbol"`
	Spec         specWire `json:"spec"`
	Tradable     bool     `json:"tradable"`
}

func toListingWire(l instrument.Listing) listingWire {
	return listingWire{
		InstrumentID: l.InstrumentID().String(),
		Provider:     l.Provider(),
		Venue:        l.Venue(),
		Symbol:       l.Symbol(),
		Spec:         toSpecWire(l.Spec()),
		Tradable:     l.Tradable(),
	}
}

type intentWire struct {
	IntentID   id.IntentID   `json:"intent_id"`
	Kind       string        `json:"kind"`
	Instrument instrument.ID `json:"instrument"`
	Side       string        `json:"side,omitempty"`
	Quantity   *num.Quantity `json:"quantity,omitempty"`
	StopPrice  *num.Price    `json:"stop_price,omitempty"`
	Metadata   id.Metadata   `json:"metadata"`
}

func toIntentWire(in order.Intent) intentWire {
	return intentWire{
		IntentID:   in.IntentID,
		Kind:       in.Kind.String(),
		Instrument: in.Instrument,
		Side:       sideString(in.Side),
		Quantity:   in.Quantity,
		StopPrice:  in.StopPrice,
		Metadata:   in.Metadata,
	}
}

type proposalWire struct {
	Listing     listingWire  `json:"listing"`
	AccountID   id.AccountID `json:"account_id"`
	Side        string       `json:"side"`
	Type        string       `json:"type"`
	TimeInForce string       `json:"time_in_force"`
	Quantity    num.Quantity `json:"quantity"`
	LimitPrice  *num.Price   `json:"limit_price,omitempty"`
	StopPrice   *num.Price   `json:"stop_price,omitempty"`
	ReduceOnly  bool         `json:"reduce_only,omitempty"`
	Metadata    id.Metadata  `json:"metadata"`
}

func toProposalWire(p order.Proposal) proposalWire {
	return proposalWire{
		Listing:     toListingWire(p.Listing),
		AccountID:   p.AccountID,
		Side:        p.Side.String(),
		Type:        p.Type.String(),
		TimeInForce: p.TimeInForce.String(),
		Quantity:    p.Quantity,
		LimitPrice:  p.LimitPrice,
		StopPrice:   p.StopPrice,
		ReduceOnly:  p.ReduceOnly,
		Metadata:    p.Metadata,
	}
}

type violationWire struct {
	Rule     string `json:"rule"`
	Message  string `json:"message"`
	Measured string `json:"measured,omitempty"`
	Limit    string `json:"limit,omitempty"`
}

type warningWire struct {
	Rule    string `json:"rule"`
	Message string `json:"message"`
}

type ruleResultWire struct {
	Rule       string          `json:"rule"`
	Violations []violationWire `json:"violations,omitempty"`
	Warnings   []warningWire   `json:"warnings,omitempty"`
}

type decisionWire struct {
	Allowed     bool             `json:"allowed"`
	Violations  []violationWire  `json:"violations,omitempty"`
	Warnings    []warningWire    `json:"warnings,omitempty"`
	RuleResults []ruleResultWire `json:"rule_results,omitempty"`
}

func toDecisionWire(d risk.Decision) decisionWire {
	w := decisionWire{Allowed: d.Allowed}
	for _, v := range d.Violations {
		w.Violations = append(w.Violations, violationWire{Rule: v.Rule, Message: v.Message, Measured: v.Measured, Limit: v.Limit})
	}
	for _, ww := range d.Warnings {
		w.Warnings = append(w.Warnings, warningWire{Rule: ww.Rule, Message: ww.Message})
	}
	for _, rr := range d.RuleResults {
		rw := ruleResultWire{Rule: rr.Rule}
		for _, v := range rr.Violations {
			rw.Violations = append(rw.Violations, violationWire{Rule: v.Rule, Message: v.Message, Measured: v.Measured, Limit: v.Limit})
		}
		for _, ww := range rr.Warnings {
			rw.Warnings = append(rw.Warnings, warningWire{Rule: ww.Rule, Message: ww.Message})
		}
		w.RuleResults = append(w.RuleResults, rw)
	}
	return w
}

type requestWire struct {
	Proposal proposalWire `json:"proposal"`
	OrderID  id.OrderID   `json:"order_id"`
}

func toRequestWire(r order.Request) requestWire {
	return requestWire{Proposal: toProposalWire(r.Proposal), OrderID: r.OrderID}
}

type rejectionWire struct {
	Reason     string `json:"reason"`
	Detail     string `json:"detail,omitempty"`
	BrokerCode string `json:"broker_code,omitempty"`
}

type orderWire struct {
	Request              requestWire    `json:"request"`
	BrokerOrderID        string         `json:"broker_order_id,omitempty"`
	AcceptedQuantity     *num.Quantity  `json:"accepted_quantity,omitempty"`
	AcceptedLimitPrice   *num.Price     `json:"accepted_limit_price,omitempty"`
	AcceptedStopPrice    *num.Price     `json:"accepted_stop_price,omitempty"`
	Status               string         `json:"status"`
	FilledQuantity       num.Quantity   `json:"filled_quantity"`
	AvgFillPrice         *num.Price     `json:"avg_fill_price,omitempty"`
	Rejection            *rejectionWire `json:"rejection,omitempty"`
	AppliedFillIDs       []id.FillID    `json:"applied_fill_ids,omitempty"`
	AppliedBrokerFillIDs []string       `json:"applied_broker_fill_ids,omitempty"`
	UpdatedAt            time.Time      `json:"updated_at"`
}

func toOrderWire(o order.Order) orderWire {
	w := orderWire{
		Request:              toRequestWire(o.Request),
		BrokerOrderID:        o.BrokerOrderID,
		AcceptedQuantity:     o.AcceptedQuantity,
		AcceptedLimitPrice:   o.AcceptedLimitPrice,
		AcceptedStopPrice:    o.AcceptedStopPrice,
		Status:               o.Status.String(),
		FilledQuantity:       o.FilledQuantity,
		AvgFillPrice:         o.AvgFillPrice,
		AppliedFillIDs:       o.AppliedFillIDs,
		AppliedBrokerFillIDs: o.AppliedBrokerFillIDs,
		UpdatedAt:            o.UpdatedAt,
	}
	if o.Rejection != nil {
		w.Rejection = &rejectionWire{Reason: o.Rejection.Reason.String(), Detail: o.Rejection.Detail, BrokerCode: o.Rejection.BrokerCode}
	}
	return w
}

type fillWire struct {
	FillID        id.FillID    `json:"fill_id"`
	OrderID       id.OrderID   `json:"order_id"`
	BrokerOrderID string       `json:"broker_order_id,omitempty"`
	BrokerFillID  string       `json:"broker_fill_id,omitempty"`
	AccountID     id.AccountID `json:"account_id"`
	Listing       listingWire  `json:"listing"`
	Side          string       `json:"side"`
	Price         num.Price    `json:"price"`
	Quantity      num.Quantity `json:"quantity"`
	Commission    *num.Money   `json:"commission,omitempty"`
	Timestamp     time.Time    `json:"timestamp"`
	Metadata      id.Metadata  `json:"metadata"`
}

func toFillWire(f order.Fill) fillWire {
	return fillWire{
		FillID:        f.FillID,
		OrderID:       f.OrderID,
		BrokerOrderID: f.BrokerOrderID,
		BrokerFillID:  f.BrokerFillID,
		AccountID:     f.AccountID,
		Listing:       toListingWire(f.Listing),
		Side:          f.Side.String(),
		Price:         f.Price,
		Quantity:      f.Quantity,
		Commission:    f.Commission,
		Timestamp:     f.Timestamp,
		Metadata:      f.Metadata,
	}
}

type positionWire struct {
	AccountID id.AccountID `json:"account_id"`
	Listing   listingWire  `json:"listing"`
	Side      string       `json:"side"`
	Quantity  num.Quantity `json:"quantity"`
	AvgPrice  *num.Price   `json:"avg_price,omitempty"`
}

func toPositionWire(p order.Position) positionWire {
	return positionWire{AccountID: p.AccountID, Listing: toListingWire(p.Listing), Side: p.Side.String(), Quantity: p.Quantity, AvgPrice: p.AvgPrice}
}

type accountWire struct {
	AccountID       id.AccountID   `json:"account_id"`
	Broker          string         `json:"broker"`
	Currency        num.Currency   `json:"currency"`
	AsOf            time.Time      `json:"as_of"`
	Cursor          string         `json:"cursor,omitempty"`
	CashBalances    []num.Money    `json:"cash_balances"`
	Equity          num.Money      `json:"equity"`
	BuyingPower     num.Money      `json:"buying_power"`
	MarginUsed      num.Money      `json:"margin_used"`
	MarginAvailable num.Money      `json:"margin_available"`
	RealizedPnL     num.Money      `json:"realized_pnl"`
	UnrealizedPnL   num.Money      `json:"unrealized_pnl"`
	Fees            num.Money      `json:"fees"`
	Financing       num.Money      `json:"financing"`
	Positions       []positionWire `json:"positions,omitempty"`
	OpenOrders      []orderWire    `json:"open_orders,omitempty"`
}

func toAccountWire(s account.Snapshot) accountWire {
	w := accountWire{
		AccountID:       s.AccountID(),
		Broker:          s.Broker(),
		Currency:        s.Currency(),
		AsOf:            s.AsOf(),
		Cursor:          s.Cursor(),
		CashBalances:    s.CashBalances(),
		Equity:          s.Equity(),
		BuyingPower:     s.BuyingPower(),
		MarginUsed:      s.MarginUsed(),
		MarginAvailable: s.MarginAvailable(),
		RealizedPnL:     s.RealizedPnL(),
		UnrealizedPnL:   s.UnrealizedPnL(),
		Fees:            s.Fees(),
		Financing:       s.Financing(),
	}
	for _, p := range s.Positions() {
		w.Positions = append(w.Positions, toPositionWire(p))
	}
	for _, o := range s.OpenOrders() {
		w.OpenOrders = append(w.OpenOrders, toOrderWire(o))
	}
	return w
}

type statusWire struct {
	State      string `json:"state"`
	BrokerCode string `json:"broker_code,omitempty"`
	Message    string `json:"message,omitempty"`
}

func toStatusWire(s broker.Status) statusWire {
	return statusWire{State: s.State.String(), BrokerCode: s.BrokerCode, Message: s.Message}
}

type tradeWire struct {
	AccountID    id.AccountID `json:"account_id"`
	Listing      listingWire  `json:"listing"`
	Side         string       `json:"side"`
	EntryFillIDs []id.FillID  `json:"entry_fill_ids"`
	ExitFillIDs  []id.FillID  `json:"exit_fill_ids,omitempty"`
	OpenedAt     time.Time    `json:"opened_at"`
	ClosedAt     time.Time    `json:"closed_at"`
	RealizedPnL  num.Money    `json:"realized_pnl"`
	Costs        num.Money    `json:"costs"`
}

func toTradeWire(t order.Trade) tradeWire {
	return tradeWire{
		AccountID:    t.AccountID,
		Listing:      toListingWire(t.Listing),
		Side:         t.Side.String(),
		EntryFillIDs: t.EntryFillIDs,
		ExitFillIDs:  t.ExitFillIDs,
		OpenedAt:     t.OpenedAt,
		ClosedAt:     t.ClosedAt,
		RealizedPnL:  t.RealizedPnL,
		Costs:        t.Costs,
	}
}

type runStartedWire struct {
	RunID  id.RunID        `json:"run_id"`
	Header json.RawMessage `json:"header,omitempty"`
}

type runCompletedWire struct {
	RunID      id.RunID `json:"run_id"`
	EntryCount uint64   `json:"entry_count"`
}

// entryWire is the full JSONL wire record: one line, one Entry. Kind
// is written as its string name for human readability; decoding
// parses it back via parseKind.
type entryWire struct {
	RunID    id.RunID    `json:"run_id"`
	Sequence uint64      `json:"sequence"`
	Metadata id.Metadata `json:"metadata"`
	Kind     string      `json:"kind"`

	RunStarted   *runStartedWire   `json:"run_started,omitempty"`
	Intent       *intentWire       `json:"intent,omitempty"`
	Proposal     *proposalWire     `json:"proposal,omitempty"`
	Decision     *decisionWire     `json:"decision,omitempty"`
	Request      *requestWire      `json:"request,omitempty"`
	Order        *orderWire        `json:"order,omitempty"`
	Fill         *fillWire         `json:"fill,omitempty"`
	Account      *accountWire      `json:"account,omitempty"`
	Status       *statusWire       `json:"status,omitempty"`
	Trade        *tradeWire        `json:"trade,omitempty"`
	RunCompleted *runCompletedWire `json:"run_completed,omitempty"`
}

// toEntryWire converts entry to its JSON wire shape. Only the payload
// wire matching entry.Kind is populated — see journal.NewRecord for
// why exactly one is ever set on a valid Entry.
func toEntryWire(e journal.Entry) entryWire {
	w := entryWire{RunID: e.RunID, Sequence: e.Sequence, Metadata: e.Metadata, Kind: e.Kind.String()}
	switch e.Kind {
	case journal.KindRunStarted:
		w.RunStarted = &runStartedWire{RunID: e.RunStarted.RunID, Header: e.RunStarted.Header}
	case journal.KindIntent:
		v := toIntentWire(*e.Intent)
		w.Intent = &v
	case journal.KindProposal:
		v := toProposalWire(*e.Proposal)
		w.Proposal = &v
	case journal.KindDecision:
		v := toDecisionWire(*e.Decision)
		w.Decision = &v
	case journal.KindRequest:
		v := toRequestWire(*e.Request)
		w.Request = &v
	case journal.KindOrder:
		v := toOrderWire(*e.Order)
		w.Order = &v
	case journal.KindFill:
		v := toFillWire(*e.Fill)
		w.Fill = &v
	case journal.KindAccount:
		v := toAccountWire(*e.Account)
		w.Account = &v
	case journal.KindStatus:
		v := toStatusWire(*e.Status)
		w.Status = &v
	case journal.KindTrade:
		v := toTradeWire(*e.Trade)
		w.Trade = &v
	case journal.KindRunCompleted:
		w.RunCompleted = &runCompletedWire{RunID: e.RunCompleted.RunID, EntryCount: e.RunCompleted.EntryCount}
	}
	return w
}

// sideString is order.Side.String(), except it returns "" for the
// zero value (order.Side's zero value has no meaningful direction for
// an IntentAdjustStop/IntentTargetExposure Intent, which legitimately
// leaves Side unset) rather than whatever order.Side.String() reports
// for an unrecognized value.
func sideString(s order.Side) string {
	if s == order.Side(0) {
		return ""
	}
	return s.String()
}
