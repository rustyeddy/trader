package tradertest

import (
	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/internal/id"
	runtimeorder "github.com/rustyeddy/trader/internal/order"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
)

// ProposalParams builds an order.Proposal. Side, Type, TimeInForce, and
// Quantity default to Buy, Market, GTC, and "1000" — every existing M1
// test fixture used these unless specifically testing a different
// order type. Listing and AccountID are required.
type ProposalParams struct {
	Listing     instrument.Listing
	AccountID   id.AccountID
	Side        order.Side
	Type        runtimeorder.Type
	TimeInForce runtimeorder.TimeInForce
	Quantity    string
	// LimitPrice and StopPrice are decimal text, required exactly when
	// Type needs them (Limit/StopLimit for LimitPrice, Stop/StopLimit
	// for StopPrice).
	LimitPrice string
	StopPrice  string
	ReduceOnly bool
	Metadata   id.Metadata
}

// NewProposal returns a valid order.Proposal built from p, filling in
// defaults for zero-valued fields.
func NewProposal(p ProposalParams) (runtimeorder.Proposal, error) {
	if p.Side == 0 {
		p.Side = order.Buy
	}
	if p.Type == 0 {
		p.Type = runtimeorder.Market
	}
	if p.TimeInForce == 0 {
		p.TimeInForce = runtimeorder.GTC
	}
	if p.Quantity == "" {
		p.Quantity = "1000"
	}

	quantity, err := num.ParseQuantity(p.Quantity)
	if err != nil {
		return runtimeorder.Proposal{}, err
	}

	var limitPrice, stopPrice *num.Price
	if p.LimitPrice != "" {
		lp, err := num.ParsePrice(p.LimitPrice)
		if err != nil {
			return runtimeorder.Proposal{}, err
		}
		limitPrice = &lp
	}
	if p.StopPrice != "" {
		sp, err := num.ParsePrice(p.StopPrice)
		if err != nil {
			return runtimeorder.Proposal{}, err
		}
		stopPrice = &sp
	}

	return runtimeorder.NewProposal(runtimeorder.Proposal{
		Listing:     p.Listing,
		AccountID:   p.AccountID,
		Side:        p.Side,
		Type:        p.Type,
		TimeInForce: p.TimeInForce,
		Quantity:    quantity,
		LimitPrice:  limitPrice,
		StopPrice:   stopPrice,
		ReduceOnly:  p.ReduceOnly,
		Metadata:    p.Metadata,
	})
}

// MustNewProposal is like NewProposal but panics on error.
func MustNewProposal(p ProposalParams) runtimeorder.Proposal {
	proposal, err := NewProposal(p)
	if err != nil {
		panic(err)
	}
	return proposal
}
