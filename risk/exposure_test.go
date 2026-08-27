package risk

import (
	"testing"

	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResultingPositionFromFlat(t *testing.T) {
	listing := mustEurUsdListing(t)
	accountID := mustAccountID(t)
	acc := mustSnapshot(t, accountID, listing)
	proposal := mustProposalWith(t, accountID, listing, order.Buy, "1000", false)

	side, qty, err := resultingPosition(acc, proposal)
	require.NoError(t, err)
	assert.Equal(t, order.Long, side)
	assert.True(t, qty.Equal(num.MustParseQuantity("1000")))
}

func TestResultingPositionAddsSameDirection(t *testing.T) {
	listing := mustEurUsdListing(t)
	accountID := mustAccountID(t)
	pos := mustPosition(t, accountID, listing, order.Long, "500")
	acc := mustSnapshotWithPositions(t, accountID, "sim", "USD", "10000", pos)
	proposal := mustProposalWith(t, accountID, listing, order.Buy, "300", false)

	side, qty, err := resultingPosition(acc, proposal)
	require.NoError(t, err)
	assert.Equal(t, order.Long, side)
	assert.True(t, qty.Equal(num.MustParseQuantity("800")))
}

func TestResultingPositionPartialReduce(t *testing.T) {
	listing := mustEurUsdListing(t)
	accountID := mustAccountID(t)
	pos := mustPosition(t, accountID, listing, order.Long, "500")
	acc := mustSnapshotWithPositions(t, accountID, "sim", "USD", "10000", pos)
	proposal := mustProposalWith(t, accountID, listing, order.Sell, "200", true)

	side, qty, err := resultingPosition(acc, proposal)
	require.NoError(t, err)
	assert.Equal(t, order.Long, side)
	assert.True(t, qty.Equal(num.MustParseQuantity("300")))
}

func TestResultingPositionExactCloseReduceOnly(t *testing.T) {
	listing := mustEurUsdListing(t)
	accountID := mustAccountID(t)
	pos := mustPosition(t, accountID, listing, order.Long, "500")
	acc := mustSnapshotWithPositions(t, accountID, "sim", "USD", "10000", pos)
	proposal := mustProposalWith(t, accountID, listing, order.Sell, "500", true)

	side, qty, err := resultingPosition(acc, proposal)
	require.NoError(t, err)
	assert.Equal(t, order.Flat, side)
	assert.True(t, qty.IsZero())
}

func TestResultingPositionExactCloseNonReduceOnly(t *testing.T) {
	listing := mustEurUsdListing(t)
	accountID := mustAccountID(t)
	pos := mustPosition(t, accountID, listing, order.Long, "500")
	acc := mustSnapshotWithPositions(t, accountID, "sim", "USD", "10000", pos)
	proposal := mustProposalWith(t, accountID, listing, order.Sell, "500", false)

	side, qty, err := resultingPosition(acc, proposal)
	require.NoError(t, err)
	assert.Equal(t, order.Flat, side)
	assert.True(t, qty.IsZero())
}

func TestResultingPositionReversal(t *testing.T) {
	listing := mustEurUsdListing(t)
	accountID := mustAccountID(t)
	pos := mustPosition(t, accountID, listing, order.Long, "100")
	acc := mustSnapshotWithPositions(t, accountID, "sim", "USD", "10000", pos)
	proposal := mustProposalWith(t, accountID, listing, order.Sell, "150", false)

	side, qty, err := resultingPosition(acc, proposal)
	require.NoError(t, err)
	assert.Equal(t, order.Short, side)
	assert.True(t, qty.Equal(num.MustParseQuantity("50")))
}

// TestResultingPositionReduceOnlyExceedingCurrentClampsAtFlat is the
// scenario from review on #183: a ReduceOnly proposal must never be
// interpreted as reversing, even if its own quantity numerically
// exceeds the current position -- it clamps at Flat instead.
func TestResultingPositionReduceOnlyExceedingCurrentClampsAtFlat(t *testing.T) {
	listing := mustEurUsdListing(t)
	accountID := mustAccountID(t)
	pos := mustPosition(t, accountID, listing, order.Long, "100")
	acc := mustSnapshotWithPositions(t, accountID, "sim", "USD", "10000", pos)
	proposal := mustProposalWith(t, accountID, listing, order.Sell, "150", true)

	side, qty, err := resultingPosition(acc, proposal)
	require.NoError(t, err)
	assert.Equal(t, order.Flat, side)
	assert.True(t, qty.IsZero())
}

func TestResultingPositionReduceOnlyAgainstFlatIsNoOp(t *testing.T) {
	listing := mustEurUsdListing(t)
	accountID := mustAccountID(t)
	acc := mustSnapshot(t, accountID, listing)
	proposal := mustProposalWith(t, accountID, listing, order.Sell, "100", true)

	side, qty, err := resultingPosition(acc, proposal)
	require.NoError(t, err)
	assert.Equal(t, order.Flat, side)
	assert.True(t, qty.IsZero())
}

// TestResultingPositionReduceOnlySameDirectionIsNoOp covers a
// contradictory input (ReduceOnly claiming the same direction as the
// current position, which can never actually reduce it): the current
// position is left unchanged rather than growing.
func TestResultingPositionReduceOnlySameDirectionIsNoOp(t *testing.T) {
	listing := mustEurUsdListing(t)
	accountID := mustAccountID(t)
	pos := mustPosition(t, accountID, listing, order.Long, "500")
	acc := mustSnapshotWithPositions(t, accountID, "sim", "USD", "10000", pos)
	proposal := mustProposalWith(t, accountID, listing, order.Buy, "200", true)

	side, qty, err := resultingPosition(acc, proposal)
	require.NoError(t, err)
	assert.Equal(t, order.Long, side)
	assert.True(t, qty.Equal(num.MustParseQuantity("500")), "an unchanged position, not a grown one")
}

func TestResultingPositionShortReducesToward(t *testing.T) {
	listing := mustEurUsdListing(t)
	accountID := mustAccountID(t)
	pos := mustPosition(t, accountID, listing, order.Short, "500")
	acc := mustSnapshotWithPositions(t, accountID, "sim", "USD", "10000", pos)
	proposal := mustProposalWith(t, accountID, listing, order.Buy, "200", true)

	side, qty, err := resultingPosition(acc, proposal)
	require.NoError(t, err)
	assert.Equal(t, order.Short, side)
	assert.True(t, qty.Equal(num.MustParseQuantity("300")))
}
