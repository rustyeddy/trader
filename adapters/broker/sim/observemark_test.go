package sim

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	brokerpkg "github.com/rustyeddy/trader/broker"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
)

func TestObserveMark_RevaluesOpenPositionUnrealizedPnL(t *testing.T) {
	ctx := context.Background()
	deps := testDeps()
	accountID := mustAccountID(t, deps.IDs)
	b, err := NewBroker("sim", deps, AccountConfig{AccountID: accountID, StartingCash: usd("10000")})
	require.NoError(t, err)
	acc, err := b.OpenAccount(ctx, accountID)
	require.NoError(t, err)

	req := mustMarketRequest(t, deps.IDs, accountID, order.Buy, "1000")
	_, err = acc.Submit(ctx, req)
	require.NoError(t, err)

	snapBefore, err := acc.Snapshot(ctx)
	require.NoError(t, err)
	require.Len(t, snapBefore.Positions(), 1)

	handle := acc.(*accountHandle)
	require.NoError(t, handle.ObserveMark(ctx, req.Listing.InstrumentID(), num.MustParsePrice("1.20000"), time.Now()))

	snapAfter, err := acc.Snapshot(ctx)
	require.NoError(t, err)
	cmp, err := snapAfter.UnrealizedPnL().Cmp(snapBefore.UnrealizedPnL())
	require.NoError(t, err)
	assert.Greater(t, cmp, 0, "revaluing a Buy position to a materially higher price must increase unrealized PnL")
}

// TestObserveMark_NeverTriggersRestingOrders proves ObserveMark is a
// pure mark-only revaluation: a resting Limit order that would clearly
// trigger against the new price under Advance's own rules must remain
// StatusWorking after ObserveMark alone (issue #219 review — the whole
// point of extracting this from Advance was to not conflate the two).
func TestObserveMark_NeverTriggersRestingOrders(t *testing.T) {
	ctx := context.Background()
	deps := testDeps()
	accountID := mustAccountID(t, deps.IDs)
	b, err := NewBroker("sim", deps, AccountConfig{AccountID: accountID, StartingCash: usd("10000")})
	require.NoError(t, err)
	acc, err := b.OpenAccount(ctx, accountID)
	require.NoError(t, err)

	// A resting Buy Limit well above the current fixed price (1.10) —
	// Advance would fill this immediately once the observed price
	// trades through it.
	limitReq := mustLimitRequest(t, deps.IDs, accountID, order.Buy, "1000", "1.05000")
	limitOrder, err := acc.Submit(ctx, limitReq)
	require.NoError(t, err)
	require.Equal(t, order.StatusWorking, limitOrder.Status)

	handle := acc.(*accountHandle)
	// A price that would clearly trigger the limit under Advance's own
	// rules (Open/Low <= limit for a Buy).
	require.NoError(t, handle.ObserveMark(ctx, limitReq.Listing.InstrumentID(), num.MustParsePrice("1.00000"), time.Now()))

	snap, err := acc.Snapshot(ctx)
	require.NoError(t, err)
	require.Len(t, snap.OpenOrders(), 1)
	assert.Equal(t, order.StatusWorking, snap.OpenOrders()[0].Status, "ObserveMark must never evaluate or fill resting order triggers")
}

func TestObserveMark_NoOpWhenNoOpenPosition(t *testing.T) {
	ctx := context.Background()
	deps := testDeps()
	accountID := mustAccountID(t, deps.IDs)
	b, err := NewBroker("sim", deps, AccountConfig{AccountID: accountID, StartingCash: usd("10000")})
	require.NoError(t, err)
	acc, err := b.OpenAccount(ctx, accountID)
	require.NoError(t, err)

	handle := acc.(*accountHandle)
	require.NoError(t, handle.ObserveMark(ctx, mustEurUsdListing(t).InstrumentID(), num.MustParsePrice("1.30000"), time.Now()))

	snap, err := acc.Snapshot(ctx)
	require.NoError(t, err)
	assert.Empty(t, snap.Positions())
	assert.True(t, snap.UnrealizedPnL().IsZero())
}

func TestObserveMark_RejectsAfterBrokerClose(t *testing.T) {
	ctx := context.Background()
	deps := testDeps()
	accountID := mustAccountID(t, deps.IDs)
	b, err := NewBroker("sim", deps, AccountConfig{AccountID: accountID, StartingCash: usd("10000")})
	require.NoError(t, err)
	acc, err := b.OpenAccount(ctx, accountID)
	require.NoError(t, err)
	require.NoError(t, b.Close())

	handle := acc.(*accountHandle)
	err = handle.ObserveMark(ctx, mustEurUsdListing(t).InstrumentID(), num.MustParsePrice("1.30000"), time.Now())
	require.ErrorIs(t, err, brokerpkg.ErrClosed)
}
