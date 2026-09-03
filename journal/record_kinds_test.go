package journal_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/account"
	"github.com/rustyeddy/trader/broker"
	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/journal"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
	"github.com/rustyeddy/trader/risk"
)

func mustProposal(t *testing.T) order.Proposal {
	t.Helper()
	p, err := order.NewProposal(order.Proposal{
		Listing:     mustEurUsdListing(t),
		AccountID:   mustAccountID(t),
		Side:        order.Buy,
		Type:        order.Market,
		TimeInForce: order.GTC,
		Quantity:    num.MustParseQuantity("1000"),
		Metadata:    id.Metadata{EventID: mustEventID(t), CorrelationID: mustCorrelationID(t)},
	})
	require.NoError(t, err)
	return p
}

func mustAccountID(t *testing.T) id.AccountID {
	t.Helper()
	v, err := id.GenerateAccountID(testIDs)
	require.NoError(t, err)
	return v
}

func mustOrderID(t *testing.T) id.OrderID {
	t.Helper()
	v, err := id.GenerateOrderID(testIDs)
	require.NoError(t, err)
	return v
}

func mustFillID(t *testing.T) id.FillID {
	t.Helper()
	v, err := id.GenerateFillID(testIDs)
	require.NoError(t, err)
	return v
}

func TestNewRecordValidEveryKind(t *testing.T) {
	runID := mustRunID(t)
	now := time.Now()
	base := func() journal.Record {
		return journal.Record{RunID: runID, Metadata: id.Metadata{Timestamp: now}}
	}

	proposal := mustProposal(t)
	request, err := order.NewRequest(proposal, mustOrderID(t))
	require.NoError(t, err)
	accepted := num.MustParseQuantity("1000")
	ord, err := order.NewOrder(order.Order{Request: request, AcceptedQuantity: &accepted, Status: order.StatusWorking})
	require.NoError(t, err)
	fill, err := order.NewFill(order.Fill{
		FillID: mustFillID(t), OrderID: request.OrderID, AccountID: request.AccountID,
		Listing: mustEurUsdListing(t), Side: order.Buy,
		Price: num.MustParsePrice("1.1"), Quantity: num.MustParseQuantity("1000"),
	})
	require.NoError(t, err)
	usd := num.MustParseCurrency("USD")
	snap, err := account.NewSnapshot(account.SnapshotParams{
		AccountID: mustAccountID(t), Broker: "sim", Currency: usd, AsOf: now,
		CashBalances: []num.Money{num.MustParseMoney("1000", usd)},
		Equity:       num.MustParseMoney("1000", usd), BuyingPower: num.MustParseMoney("1000", usd),
		MarginUsed: num.MustParseMoney("0", usd), MarginAvailable: num.MustParseMoney("1000", usd),
		RealizedPnL: num.MustParseMoney("0", usd), UnrealizedPnL: num.MustParseMoney("0", usd),
		Fees: num.MustParseMoney("0", usd), Financing: num.MustParseMoney("0", usd),
	})
	require.NoError(t, err)
	trade, err := order.NewTrade(order.Trade{
		AccountID: mustAccountID(t), Listing: mustEurUsdListing(t), Side: order.Long,
		EntryFillIDs: []id.FillID{mustFillID(t)}, OpenedAt: now,
		RealizedPnL: num.MustParseMoney("0", usd), Costs: num.MustParseMoney("0", usd),
	})
	require.NoError(t, err)
	status := broker.Status{State: broker.AccountStatusActive}
	decision := risk.Decision{Allowed: true}
	signal := journal.Signal{Strategy: "ema-cross", Values: map[string]string{"action": "none"}}

	cases := []journal.Record{
		func() journal.Record { r := base(); r.Kind, r.Proposal = journal.KindProposal, &proposal; return r }(),
		func() journal.Record { r := base(); r.Kind, r.Decision = journal.KindDecision, &decision; return r }(),
		func() journal.Record { r := base(); r.Kind, r.Request = journal.KindRequest, &request; return r }(),
		func() journal.Record { r := base(); r.Kind, r.Order = journal.KindOrder, &ord; return r }(),
		func() journal.Record { r := base(); r.Kind, r.Fill = journal.KindFill, &fill; return r }(),
		func() journal.Record { r := base(); r.Kind, r.Account = journal.KindAccount, &snap; return r }(),
		func() journal.Record { r := base(); r.Kind, r.Status = journal.KindStatus, &status; return r }(),
		func() journal.Record { r := base(); r.Kind, r.Trade = journal.KindTrade, &trade; return r }(),
		func() journal.Record { r := base(); r.Kind, r.Signal = journal.KindSignal, &signal; return r }(),
	}
	for _, rec := range cases {
		_, err := journal.NewRecord(rec)
		require.NoError(t, err, "kind %s", rec.Kind)
	}
}

func TestNewRecordRejectsSignalWithEmptyStrategy(t *testing.T) {
	rec := journal.Record{
		RunID:    mustRunID(t),
		Metadata: id.Metadata{Timestamp: time.Now()},
		Kind:     journal.KindSignal,
		Signal:   &journal.Signal{Values: map[string]string{"action": "none"}},
	}
	_, err := journal.NewRecord(rec)
	require.ErrorIs(t, err, journal.ErrInvalidRecord)
}
