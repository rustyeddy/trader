package order

import (
	"testing"

	"github.com/rustyeddy/trader/id"
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
