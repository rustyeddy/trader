// Package storetest is the contract suite every accountactivity.Store must
// pass (issue #509). An adapter's tests call Run with a factory that returns
// a fresh, empty store; the suite then checks the behaviours the import path
// relies on: idempotent upsert, ordered range queries, checkpoints that move
// only with their activities, and conflict handling.
package storetest

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/internal/accountactivity"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
)

// Factory returns a new empty Store.
type Factory func(t *testing.T) accountactivity.Store

var (
	usd  = num.MustParseCurrency("USD")
	base = time.Date(2024, 3, 5, 9, 0, 0, 0, time.UTC)

	// Sync is the stream the suite writes to.
	Sync = accountactivity.SyncKey{Provider: "oanda", Environment: accountactivity.EnvironmentPractice, ProviderAccountID: "101-001-1234567-001"}
)

func money(s string) num.Money { return num.MustParseMoney(s, usd) }

// Deposit returns a valid deposit in Sync with the given activity ID, at
// base+offset.
func Deposit(activityID string, offset time.Duration) accountactivity.Activity {
	return accountactivity.Activity{
		Provider: Sync.Provider, Environment: Sync.Environment, ProviderAccountID: Sync.ProviderAccountID,
		ProviderActivityID: activityID, ProviderType: "TRANSFER_FUNDS", Kind: accountactivity.KindDeposit,
		OccurredAt: base.Add(offset), ImportedAt: base.Add(24 * time.Hour),
		Cash: accountactivity.Reported(money("500")),
	}
}

// RichFill returns a fill exercising every optional field family: trade
// links, all cost fields, a sourced benchmark and derived slippage.
func RichFill(activityID string, offset time.Duration) accountactivity.Activity {
	must := func(v accountactivity.Value[num.Money], err error) accountactivity.Value[num.Money] {
		if err != nil {
			panic(err)
		}
		return v
	}
	spread, err := accountactivity.ReportedFrom(money("-0.25"), "halfSpreadCost")
	if err != nil {
		panic(err)
	}
	bench, err := accountactivity.ReportedFrom(num.MustParsePrice("1.08520"), "fullPrice.ask")
	if err != nil {
		panic(err)
	}
	slip, err := accountactivity.Derived(num.MustParseRate("-0.00012"), "execution price - benchmark, account sign")
	if err != nil {
		panic(err)
	}
	return accountactivity.Activity{
		Provider: Sync.Provider, Environment: Sync.Environment, ProviderAccountID: Sync.ProviderAccountID,
		ProviderActivityID: activityID, ProviderType: "ORDER_FILL", Kind: accountactivity.KindFill,
		OccurredAt: base.Add(offset), ImportedAt: base.Add(24 * time.Hour),
		Instrument: instrument.CurrencyPairID(num.MustParseCurrency("EUR"), usd), Side: order.Buy,
		Quantity:      accountactivity.Reported(num.MustParseQuantity("1000")),
		Price:         accountactivity.Reported(num.MustParsePrice("1.08532")),
		BrokerOrderID: "6359", BrokerBatchID: "6359", ProviderInstrument: "EUR_USD",
		TradeLinks:   []accountactivity.TradeLink{{BrokerTradeID: "6360", Role: accountactivity.TradeRoleOpened}, {BrokerTradeID: "6301", Role: accountactivity.TradeRoleClosed}},
		RealizedPL:   accountactivity.Reported(money("0")),
		SpreadCost:   spread,
		Commission:   accountactivity.Reported(money("-0.05")),
		Financing:    accountactivity.Reported(money("0")),
		OtherFees:    must(accountactivity.Derived(money("-0.01"), "sum of dealing fees")),
		Benchmark:    bench,
		Slippage:     slip,
		SlippageCost: must(accountactivity.Derived(money("-0.12"), "slippage * quantity")),
	}
}

func batch(cursor string, as ...accountactivity.Activity) accountactivity.Batch {
	return accountactivity.Batch{Sync: Sync, Activities: as, Cursor: cursor, At: base.Add(48 * time.Hour)}
}

func ids(as []accountactivity.Activity) []string {
	out := make([]string, len(as))
	for i, a := range as {
		out[i] = a.ProviderActivityID
	}
	return out
}

// Run executes the contract suite against stores from newStore.
func Run(t *testing.T, newStore Factory) {
	ctx := context.Background()

	t.Run("a new stream has no activities and no checkpoint", func(t *testing.T) {
		s := newStore(t)
		got, err := s.Query(ctx, accountactivity.Query{Sync: Sync})
		require.NoError(t, err)
		assert.Empty(t, got)
		_, ok, err := s.Checkpoint(ctx, Sync)
		require.NoError(t, err)
		assert.False(t, ok)
	})

	t.Run("every field survives a round trip", func(t *testing.T) {
		s := newStore(t)
		want := []accountactivity.Activity{RichFill("6360", time.Hour), Deposit("6361", 2*time.Hour)}
		_, err := s.Apply(ctx, batch("6361", want...))
		require.NoError(t, err)

		got, err := s.Query(ctx, accountactivity.Query{Sync: Sync})
		require.NoError(t, err)
		require.Len(t, got, 2)
		for i := range want {
			assert.True(t, want[i].SameRecord(got[i]), "record %d changed in storage", i)
			assert.True(t, want[i].ImportedAt.Equal(got[i].ImportedAt), "ImportedAt is kept")
		}
		// Provenance is preserved per value.
		assert.Equal(t, accountactivity.BasisDerived, got[0].Slippage.Basis())
		assert.Equal(t, "halfSpreadCost", got[0].SpreadCost.Source())
		assert.Equal(t, accountactivity.BasisReported, got[0].Financing.Basis())
		assert.Len(t, got[0].TradeLinks, 2)
		assert.Equal(t, "6359", got[0].BrokerBatchID, "the broker's batch grouping is stored")
		assert.Equal(t, "EUR_USD", got[0].ProviderInstrument, "the provider-native instrument is stored")
	})

	t.Run("queries return deterministic order whatever the arrival order", func(t *testing.T) {
		s := newStore(t)
		// Same instant: IDs 9 and 10 must sort numerically, then later times.
		_, err := s.Apply(ctx, batch("c1", Deposit("30", 3*time.Hour), Deposit("10", time.Hour), Deposit("9", time.Hour)))
		require.NoError(t, err)
		_, err = s.Apply(ctx, batch("c2", Deposit("5", 2*time.Hour), Deposit("1", 0)))
		require.NoError(t, err)

		got, err := s.Query(ctx, accountactivity.Query{Sync: Sync})
		require.NoError(t, err)
		assert.Equal(t, []string{"1", "9", "10", "5", "30"}, ids(got))
	})

	t.Run("time range is half open and kinds filter", func(t *testing.T) {
		s := newStore(t)
		_, err := s.Apply(ctx, batch("c", Deposit("1", 0), Deposit("2", time.Hour), RichFill("3", 2*time.Hour), Deposit("4", 3*time.Hour)))
		require.NoError(t, err)

		got, err := s.Query(ctx, accountactivity.Query{Sync: Sync, From: base.Add(time.Hour), To: base.Add(3 * time.Hour)})
		require.NoError(t, err)
		assert.Equal(t, []string{"2", "3"}, ids(got), "From inclusive, To exclusive")

		got, err = s.Query(ctx, accountactivity.Query{Sync: Sync, From: base.Add(2 * time.Hour)})
		require.NoError(t, err)
		assert.Equal(t, []string{"3", "4"}, ids(got), "zero To is unbounded")

		got, err = s.Query(ctx, accountactivity.Query{Sync: Sync, Kinds: []accountactivity.Kind{accountactivity.KindFill}})
		require.NoError(t, err)
		assert.Equal(t, []string{"3"}, ids(got))

		_, err = s.Query(ctx, accountactivity.Query{Sync: Sync, From: base.Add(time.Hour), To: base})
		assert.ErrorIs(t, err, accountactivity.ErrInvalidQuery)
	})

	t.Run("streams are isolated by provider, environment and account", func(t *testing.T) {
		s := newStore(t)
		_, err := s.Apply(ctx, batch("c", Deposit("1", 0)))
		require.NoError(t, err)

		for _, other := range []accountactivity.SyncKey{
			{Provider: "alpaca", Environment: Sync.Environment, ProviderAccountID: Sync.ProviderAccountID},
			{Provider: Sync.Provider, Environment: accountactivity.EnvironmentLive, ProviderAccountID: Sync.ProviderAccountID},
			{Provider: Sync.Provider, Environment: Sync.Environment, ProviderAccountID: "101-001-9999999-001"},
		} {
			got, err := s.Query(ctx, accountactivity.Query{Sync: other})
			require.NoError(t, err)
			assert.Empty(t, got, other.String())
			_, ok, err := s.Checkpoint(ctx, other)
			require.NoError(t, err)
			assert.False(t, ok, other.String())
		}

		// The same activity ID in another environment is a different activity.
		live := Deposit("1", 0)
		live.Environment = accountactivity.EnvironmentLive
		_, err = s.Apply(ctx, accountactivity.Batch{
			Sync:       accountactivity.SyncKey{Provider: live.Provider, Environment: live.Environment, ProviderAccountID: live.ProviderAccountID},
			Activities: []accountactivity.Activity{live}, Cursor: "c", At: base.Add(48 * time.Hour),
		})
		require.NoError(t, err)
		got, err := s.Query(ctx, accountactivity.Query{Sync: Sync})
		require.NoError(t, err)
		assert.Len(t, got, 1)
	})

	t.Run("importing the same batch twice creates no duplicates", func(t *testing.T) {
		s := newStore(t)
		b := batch("c", Deposit("1", 0), RichFill("2", time.Hour))
		res, err := s.Apply(ctx, b)
		require.NoError(t, err)
		assert.Equal(t, accountactivity.ApplyResult{Added: 2}, res)

		res, err = s.Apply(ctx, b)
		require.NoError(t, err)
		assert.Equal(t, accountactivity.ApplyResult{Unchanged: 2}, res)

		got, err := s.Query(ctx, accountactivity.Query{Sync: Sync})
		require.NoError(t, err)
		assert.Equal(t, []string{"1", "2"}, ids(got))
	})

	t.Run("a repeat inside one batch is harmless", func(t *testing.T) {
		s := newStore(t)
		res, err := s.Apply(ctx, batch("c", Deposit("1", 0), Deposit("1", 0)))
		require.NoError(t, err)
		assert.Equal(t, accountactivity.ApplyResult{Added: 1, Unchanged: 1}, res)
	})

	t.Run("re-importing later keeps the first ImportedAt", func(t *testing.T) {
		s := newStore(t)
		first := Deposit("1", 0)
		_, err := s.Apply(ctx, batch("c", first))
		require.NoError(t, err)

		again := Deposit("1", 0)
		again.ImportedAt = base.Add(500 * time.Hour)
		res, err := s.Apply(ctx, batch("c2", again))
		require.NoError(t, err)
		assert.Equal(t, accountactivity.ApplyResult{Unchanged: 1}, res)

		got, err := s.Query(ctx, accountactivity.Query{Sync: Sync})
		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.True(t, first.ImportedAt.Equal(got[0].ImportedAt))
	})

	t.Run("a conflicting activity rejects the whole batch and changes nothing", func(t *testing.T) {
		s := newStore(t)
		_, err := s.Apply(ctx, batch("c1", RichFill("1", 0)))
		require.NoError(t, err)

		changed := RichFill("1", 0)
		changed.Price = accountactivity.Reported(num.MustParsePrice("1.09999"))
		_, err = s.Apply(ctx, batch("c2", Deposit("2", time.Hour), changed))
		require.ErrorIs(t, err, accountactivity.ErrConflict)

		got, err := s.Query(ctx, accountactivity.Query{Sync: Sync})
		require.NoError(t, err)
		assert.Equal(t, []string{"1"}, ids(got), "the new activity in the rejected batch was not stored")
		cp, ok, err := s.Checkpoint(ctx, Sync)
		require.NoError(t, err)
		require.True(t, ok)
		assert.Equal(t, "c1", cp.Cursor, "the checkpoint did not move")
	})

	t.Run("the checkpoint advances with each applied batch", func(t *testing.T) {
		s := newStore(t)
		_, err := s.Apply(ctx, batch("cursor-1", Deposit("1", 0)))
		require.NoError(t, err)
		cp, ok, err := s.Checkpoint(ctx, Sync)
		require.NoError(t, err)
		require.True(t, ok)
		assert.Equal(t, accountactivity.Checkpoint{Sync: Sync, Cursor: "cursor-1", UpdatedAt: base.Add(48 * time.Hour)}, normalized(cp))

		// An empty page still moves the cursor.
		b := batch("cursor-2")
		b.At = base.Add(72 * time.Hour)
		_, err = s.Apply(ctx, b)
		require.NoError(t, err)
		cp, _, err = s.Checkpoint(ctx, Sync)
		require.NoError(t, err)
		assert.Equal(t, "cursor-2", cp.Cursor)
		assert.True(t, b.At.Equal(cp.UpdatedAt))
	})

	t.Run("an invalid batch is rejected before anything is written", func(t *testing.T) {
		s := newStore(t)
		foreign := Deposit("2", time.Hour)
		foreign.ProviderAccountID = "someone-else"
		broken := Deposit("3", 2*time.Hour)
		broken.ProviderType = ""
		noCursor := batch("", Deposit("4", 0))
		noTime := batch("c", Deposit("5", 0))
		noTime.At = time.Time{}

		for name, b := range map[string]accountactivity.Batch{
			"foreign stream": batch("c", Deposit("1", 0), foreign),
			"invalid record": batch("c", Deposit("1", 0), broken),
			"no cursor":      noCursor,
			"no time":        noTime,
		} {
			_, err := s.Apply(ctx, b)
			assert.ErrorIs(t, err, accountactivity.ErrInvalidBatch, name)
		}
		got, err := s.Query(ctx, accountactivity.Query{Sync: Sync})
		require.NoError(t, err)
		assert.Empty(t, got)
		_, ok, err := s.Checkpoint(ctx, Sync)
		require.NoError(t, err)
		assert.False(t, ok)
	})

	t.Run("a cancelled context is honoured", func(t *testing.T) {
		s := newStore(t)
		cctx, cancel := context.WithCancel(ctx)
		cancel()
		_, err := s.Apply(cctx, batch("c", Deposit("1", 0)))
		assert.ErrorIs(t, err, context.Canceled)
		_, err = s.Query(cctx, accountactivity.Query{Sync: Sync})
		assert.ErrorIs(t, err, context.Canceled)
	})
}

// normalized drops monotonic and location differences from UpdatedAt.
func normalized(cp accountactivity.Checkpoint) accountactivity.Checkpoint {
	cp.UpdatedAt = cp.UpdatedAt.UTC().Round(0)
	return cp
}
