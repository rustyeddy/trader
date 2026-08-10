package order

import (
	"testing"

	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/num"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewRequestValid(t *testing.T) {
	r, err := NewRequest(mustProposal(t), mustOrderID(t))
	require.NoError(t, err)
	assert.False(t, r.OrderID.IsZero())
	assert.Equal(t, Market, r.Type, "proposal fields are promoted")
}

func TestNewRequestRejectsUnconstructedProposal(t *testing.T) {
	_, err := NewRequest(Proposal{}, mustOrderID(t))
	assert.ErrorIs(t, err, ErrInvalidRequest)
}

func TestNewRequestRejectsZeroOrderID(t *testing.T) {
	_, err := NewRequest(mustProposal(t), id.OrderID{})
	assert.ErrorIs(t, err, ErrInvalidRequest)
}

// NewRequest must fully revalidate its Proposal, not merely trust it by
// provenance: Proposal's exported fields let a caller build one as a
// bare struct literal, bypassing NewProposal entirely.
func TestNewRequestRevalidatesProposalBuiltAsStructLiteral(t *testing.T) {
	bypassed := Proposal{
		Listing: mustEurUsdListing(t),
		// AccountID deliberately left zero, which NewProposal would
		// have rejected.
		Side:        Buy,
		Type:        Market,
		TimeInForce: GTC,
		Quantity:    num.MustParseQuantity("1000"),
	}
	_, err := NewRequest(bypassed, mustOrderID(t))
	assert.ErrorIs(t, err, ErrInvalidRequest)
}
