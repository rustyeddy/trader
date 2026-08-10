package order

import (
	"testing"

	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/num"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewProposalValidMarket(t *testing.T) {
	p, err := NewProposal(Proposal{
		Listing:     mustEurUsdListing(t),
		AccountID:   mustAccountID(t),
		Side:        Buy,
		Type:        Market,
		TimeInForce: GTC,
		Quantity:    num.MustParseQuantity("1000"),
		Metadata:    id.Metadata{EventID: mustEventID(t)},
	})
	require.NoError(t, err)
	assert.Equal(t, Market, p.Type)
}

func TestNewProposalValidLimit(t *testing.T) {
	p, err := NewProposal(Proposal{
		Listing:     mustEurUsdListing(t),
		AccountID:   mustAccountID(t),
		Side:        Buy,
		Type:        Limit,
		TimeInForce: GTC,
		Quantity:    num.MustParseQuantity("1000"),
		LimitPrice:  price(t, "1.10000"),
	})
	require.NoError(t, err)
	require.NotNil(t, p.LimitPrice)
}

func TestNewProposalValidStopLimit(t *testing.T) {
	_, err := NewProposal(Proposal{
		Listing:     mustEurUsdListing(t),
		AccountID:   mustAccountID(t),
		Side:        Sell,
		Type:        StopLimit,
		TimeInForce: GTC,
		Quantity:    num.MustParseQuantity("1000"),
		LimitPrice:  price(t, "1.09000"),
		StopPrice:   price(t, "1.09500"),
	})
	require.NoError(t, err)
}

func TestNewProposalRejectsUnconstructedListing(t *testing.T) {
	_, err := NewProposal(Proposal{
		AccountID:   mustAccountID(t),
		Side:        Buy,
		Type:        Market,
		TimeInForce: GTC,
		Quantity:    num.MustParseQuantity("1000"),
	})
	assert.ErrorIs(t, err, ErrInvalidProposal)
}

func TestNewProposalRejectsZeroAccountID(t *testing.T) {
	_, err := NewProposal(Proposal{
		Listing:     mustEurUsdListing(t),
		Side:        Buy,
		Type:        Market,
		TimeInForce: GTC,
		Quantity:    num.MustParseQuantity("1000"),
	})
	assert.ErrorIs(t, err, ErrInvalidProposal)
}

func TestNewProposalRejectsInvalidSide(t *testing.T) {
	_, err := NewProposal(Proposal{
		Listing:     mustEurUsdListing(t),
		AccountID:   mustAccountID(t),
		Type:        Market,
		TimeInForce: GTC,
		Quantity:    num.MustParseQuantity("1000"),
	})
	assert.ErrorIs(t, err, ErrInvalidProposal)
}

func TestNewProposalRejectsInvalidType(t *testing.T) {
	_, err := NewProposal(Proposal{
		Listing:     mustEurUsdListing(t),
		AccountID:   mustAccountID(t),
		Side:        Buy,
		TimeInForce: GTC,
		Quantity:    num.MustParseQuantity("1000"),
	})
	assert.ErrorIs(t, err, ErrInvalidProposal)
}

func TestNewProposalRejectsInvalidTimeInForce(t *testing.T) {
	_, err := NewProposal(Proposal{
		Listing:   mustEurUsdListing(t),
		AccountID: mustAccountID(t),
		Side:      Buy,
		Type:      Market,
		Quantity:  num.MustParseQuantity("1000"),
	})
	assert.ErrorIs(t, err, ErrInvalidProposal)
}

func TestNewProposalRejectsZeroQuantity(t *testing.T) {
	_, err := NewProposal(Proposal{
		Listing:     mustEurUsdListing(t),
		AccountID:   mustAccountID(t),
		Side:        Buy,
		Type:        Market,
		TimeInForce: GTC,
	})
	assert.ErrorIs(t, err, ErrInvalidProposal)
}

func TestNewProposalRejectsMarketWithLimitPrice(t *testing.T) {
	_, err := NewProposal(Proposal{
		Listing:     mustEurUsdListing(t),
		AccountID:   mustAccountID(t),
		Side:        Buy,
		Type:        Market,
		TimeInForce: GTC,
		Quantity:    num.MustParseQuantity("1000"),
		LimitPrice:  price(t, "1.10000"),
	})
	assert.ErrorIs(t, err, ErrInvalidProposal)
}

func TestNewProposalRejectsLimitWithoutLimitPrice(t *testing.T) {
	_, err := NewProposal(Proposal{
		Listing:     mustEurUsdListing(t),
		AccountID:   mustAccountID(t),
		Side:        Buy,
		Type:        Limit,
		TimeInForce: GTC,
		Quantity:    num.MustParseQuantity("1000"),
	})
	assert.ErrorIs(t, err, ErrInvalidProposal)
}

func TestNewProposalRejectsStopWithoutStopPrice(t *testing.T) {
	_, err := NewProposal(Proposal{
		Listing:     mustEurUsdListing(t),
		AccountID:   mustAccountID(t),
		Side:        Sell,
		Type:        Stop,
		TimeInForce: GTC,
		Quantity:    num.MustParseQuantity("1000"),
	})
	assert.ErrorIs(t, err, ErrInvalidProposal)
}

func TestNewProposalRejectsLimitWithStopPrice(t *testing.T) {
	_, err := NewProposal(Proposal{
		Listing:     mustEurUsdListing(t),
		AccountID:   mustAccountID(t),
		Side:        Buy,
		Type:        Limit,
		TimeInForce: GTC,
		Quantity:    num.MustParseQuantity("1000"),
		LimitPrice:  price(t, "1.10000"),
		StopPrice:   price(t, "1.09000"),
	})
	assert.ErrorIs(t, err, ErrInvalidProposal)
}

func TestNewProposalRejectsStopPriceNotOnTick(t *testing.T) {
	_, err := NewProposal(Proposal{
		Listing:     mustEurUsdListing(t),
		AccountID:   mustAccountID(t),
		Side:        Sell,
		Type:        Stop,
		TimeInForce: GTC,
		Quantity:    num.MustParseQuantity("1000"),
		StopPrice:   price(t, "1.09000123"),
	})
	assert.ErrorIs(t, err, ErrInvalidProposal)
}

func TestNewProposalRejectsQuantityNotOnIncrement(t *testing.T) {
	// mustEurUsdListing's Spec has a quantity increment of 1.
	_, err := NewProposal(Proposal{
		Listing:     mustEurUsdListing(t),
		AccountID:   mustAccountID(t),
		Side:        Buy,
		Type:        Market,
		TimeInForce: GTC,
		Quantity:    num.MustParseQuantity("1000.5"),
	})
	assert.ErrorIs(t, err, ErrInvalidProposal)
}

func TestNewProposalRejectsPriceNotOnTick(t *testing.T) {
	_, err := NewProposal(Proposal{
		Listing:     mustEurUsdListing(t),
		AccountID:   mustAccountID(t),
		Side:        Buy,
		Type:        Limit,
		TimeInForce: GTC,
		Quantity:    num.MustParseQuantity("1000"),
		LimitPrice:  price(t, "1.10000123"),
	})
	assert.ErrorIs(t, err, ErrInvalidProposal)
}
