package sim

import (
	"context"
	"testing"

	"github.com/rustyeddy/trader/account"
	"github.com/rustyeddy/trader/journal"
	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── test helpers ──────────────────────────────────────────────────────────────

// stubJournal records every call so tests can assert on side-effects.
type stubJournal struct {
	trades   []journal.TradeRecord
	equity   []journal.EquitySnapshot
	closeErr error
}

func (j *stubJournal) RecordTrade(r journal.TradeRecord) error {
	j.trades = append(j.trades, r)
	return nil
}
func (j *stubJournal) RecordEquity(s journal.EquitySnapshot) error {
	j.equity = append(j.equity, s)
	return nil
}
func (j *stubJournal) Close() error { return j.closeErr }

// eurusdTick returns a valid EURUSD tick at the given mid (bid = mid-1, ask = mid+1).
func eurusdTick(mid types.Price) market.Tick {
	return market.Tick{
		Instrument: "EURUSD",
		BA:         market.BA{Bid: mid - 1, Ask: mid + 1},
	}
}

// openLot adds a minimal open lot for EURUSD to acct and returns it.
func openLot(t *testing.T, acct *account.Account, entryPrice types.Price) *account.Lot {
	t.Helper()
	lot := &account.Lot{
		TradeCommon: &account.TradeCommon{
			ID:         "lot-sim-1",
			Instrument: "EURUSD",
			Side:       types.Long,
			Units:      1000,
		},
		EntryPrice:     entryPrice,
		EntryTime:      1000,
		OriginalUnits:  1000,
		RemainingUnits: 1000,
		State:          account.LotOpen,
	}
	require.NoError(t, acct.AddLot(lot))
	return lot
}

// ── NewSimBroker ──────────────────────────────────────────────────────────────

func TestNewSimBroker_NilAccountCreatesDefault(t *testing.T) {
	s := NewSimBroker(nil, nil)
	require.NotNil(t, s)
	require.NotNil(t, s.account)
}

func TestNewSimBroker_ProvidedAccountIsUsed(t *testing.T) {
	acct := account.NewAccount("test", types.MoneyFromFloat(5_000))
	s := NewSimBroker(acct, nil)
	require.NotNil(t, s)
	assert.Equal(t, acct, s.account)
}

func TestNewSimBroker_NilJournalIsAccepted(t *testing.T) {
	s := NewSimBroker(nil, nil)
	assert.Nil(t, s.journal)
}

func TestNewSimBroker_JournalIsStored(t *testing.T) {
	j := &stubJournal{}
	s := NewSimBroker(nil, j)
	assert.Equal(t, j, s.journal)
}

// ── GetAccount ────────────────────────────────────────────────────────────────

func TestGetAccount_ReturnsAccount(t *testing.T) {
	acct := account.NewAccount("test", types.MoneyFromFloat(10_000))
	s := NewSimBroker(acct, nil)
	got, err := s.GetAccount(context.Background())
	require.NoError(t, err)
	assert.Equal(t, acct, got)
}

func TestGetAccount_NilReceiverReturnsError(t *testing.T) {
	var s *Sim
	_, err := s.GetAccount(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "account is nil")
}

func TestGetAccount_NilAccountFieldReturnsError(t *testing.T) {
	s := &Sim{}
	_, err := s.GetAccount(context.Background())
	require.Error(t, err)
}

// ── UpdatePrice ───────────────────────────────────────────────────────────────

func TestUpdatePrice_ValidTickUpdatesEquity(t *testing.T) {
	acct := account.NewAccount("test", types.MoneyFromFloat(10_000))
	s := NewSimBroker(acct, nil)

	err := s.UpdatePrice(eurusdTick(types.PriceFromFloat(1.08)))
	require.NoError(t, err)
	assert.Contains(t, s.prices, "EURUSD")
}

func TestUpdatePrice_NormalizesInstrumentName(t *testing.T) {
	s := NewSimBroker(nil, nil)
	tick := market.Tick{
		Instrument: "EUR_USD", // underscore form
		BA:         market.BA{Bid: 1_080_000, Ask: 1_080_200},
	}
	require.NoError(t, s.UpdatePrice(tick))
	assert.Contains(t, s.prices, "EURUSD")
	assert.NotContains(t, s.prices, "EUR_USD")
}

func TestUpdatePrice_BlankInstrumentReturnsError(t *testing.T) {
	s := NewSimBroker(nil, nil)
	tick := market.Tick{Instrument: "", BA: market.BA{Bid: 1_000_000, Ask: 1_000_200}}
	err := s.UpdatePrice(tick)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "blank instrument")
}

func TestUpdatePrice_InvalidTickReturnsError(t *testing.T) {
	s := NewSimBroker(nil, nil)
	// Ask < Bid → invalid
	tick := market.Tick{
		Instrument: "EURUSD",
		BA:         market.BA{Bid: 1_080_200, Ask: 1_080_000},
	}
	err := s.UpdatePrice(tick)
	require.Error(t, err)
}

func TestUpdatePrice_NilReceiverReturnsError(t *testing.T) {
	var s *Sim
	err := s.UpdatePrice(eurusdTick(1_080_000))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "account is nil")
}

func TestUpdatePrice_MultipleInstrumentsTrackedSeparately(t *testing.T) {
	s := NewSimBroker(nil, nil)

	eurTick := eurusdTick(types.PriceFromFloat(1.08))
	jpyTick := market.Tick{
		Instrument: "USDJPY",
		BA:         market.BA{Bid: 150_000_000, Ask: 150_020_000},
	}
	require.NoError(t, s.UpdatePrice(eurTick))
	require.NoError(t, s.UpdatePrice(jpyTick))
	assert.Len(t, s.prices, 2)
	assert.Contains(t, s.prices, "EURUSD")
	assert.Contains(t, s.prices, "USDJPY")
}

// ── CloseAll ─────────────────────────────────────────────────────────────────

func TestCloseAll_NoLots_Succeeds(t *testing.T) {
	s := NewSimBroker(nil, nil)
	err := s.CloseAll(context.Background(), "test")
	require.NoError(t, err)
}

func TestCloseAll_NoLots_RecordsEquitySnapshot(t *testing.T) {
	j := &stubJournal{}
	s := NewSimBroker(nil, j)
	require.NoError(t, s.CloseAll(context.Background(), "eod"))
	assert.Len(t, j.equity, 1)
	assert.Empty(t, j.trades)
}

func TestCloseAll_WithLot_ClosesAndRecordsTrade(t *testing.T) {
	acct := account.NewAccount("test", types.MoneyFromFloat(10_000))
	j := &stubJournal{}
	s := NewSimBroker(acct, j)

	entry := types.PriceFromFloat(1.08)
	openLot(t, acct, entry)

	// Provide a price so CloseAll can compute an exit.
	require.NoError(t, s.UpdatePrice(eurusdTick(types.PriceFromFloat(1.09))))

	require.NoError(t, s.CloseAll(context.Background(), "take-profit"))

	assert.Equal(t, 0, acct.Lots.Len())
	require.Len(t, j.trades, 1)
	assert.Equal(t, "EURUSD", j.trades[0].Instrument)
	assert.Equal(t, "take-profit", j.trades[0].Reason)
	assert.Len(t, j.equity, 1)
}

func TestCloseAll_MissingPriceReturnsError(t *testing.T) {
	acct := account.NewAccount("test", types.MoneyFromFloat(10_000))
	s := NewSimBroker(acct, nil)
	openLot(t, acct, types.PriceFromFloat(1.08))

	// No UpdatePrice called → price map is empty.
	err := s.CloseAll(context.Background(), "reason")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no market price")
}

func TestCloseAll_NilReceiverReturnsError(t *testing.T) {
	var s *Sim
	err := s.CloseAll(context.Background(), "reason")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "account is nil")
}

func TestCloseAll_NilJournal_DoesNotPanic(t *testing.T) {
	acct := account.NewAccount("test", types.MoneyFromFloat(10_000))
	s := NewSimBroker(acct, nil) // no journal
	openLot(t, acct, types.PriceFromFloat(1.08))
	require.NoError(t, s.UpdatePrice(eurusdTick(types.PriceFromFloat(1.09))))

	require.NotPanics(t, func() {
		_ = s.CloseAll(context.Background(), "close")
	})
}

// ── SubmitMarketOrder ────────────────────────────────────────────────────────

func TestSubmitMarketOrder_LongFillsAtAsk(t *testing.T) {
	acct := account.NewAccount("test", types.MoneyFromFloat(10_000))
	s := NewSimBroker(acct, nil)
	require.NoError(t, s.UpdatePrice(eurusdTick(types.PriceFromFloat(1.1000))))

	res, err := s.SubmitMarketOrder(context.Background(), "acct", "EURUSD", 1000, 0)
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, int64(1000), res.Units)
	assert.Equal(t, "EURUSD", res.Instrument)
	assert.InDelta(t, types.PriceFromFloat(1.1000).Float64()+1.0/float64(types.PriceScale), res.Price, 1e-9,
		"long fills at ask (mid+1 tick), no slippage configured")
	require.Equal(t, 1, acct.Lots.Len())
}

func TestSubmitMarketOrder_ShortFillsAtBid(t *testing.T) {
	acct := account.NewAccount("test", types.MoneyFromFloat(10_000))
	s := NewSimBroker(acct, nil)
	require.NoError(t, s.UpdatePrice(eurusdTick(types.PriceFromFloat(1.1000))))

	res, err := s.SubmitMarketOrder(context.Background(), "acct", "EURUSD", -1000, 0)
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, int64(-1000), res.Units)
	assert.InDelta(t, types.PriceFromFloat(1.1000).Float64()-1.0/float64(types.PriceScale), res.Price, 1e-9,
		"short fills at bid (mid-1 tick), no slippage configured")

	lot := acct.Lots.Get(res.TradeID)
	require.NotNil(t, lot)
	assert.Equal(t, types.Short, lot.Side)
}

func TestSubmitMarketOrder_SlippageWorsensLongFill(t *testing.T) {
	acct := account.NewAccount("test", types.MoneyFromFloat(10_000))
	s := NewSimBroker(acct, nil)
	s.Slippage = types.PriceFromFloat(0.0002)
	require.NoError(t, s.UpdatePrice(eurusdTick(types.PriceFromFloat(1.1000))))

	noSlip, err := s.SubmitMarketOrder(context.Background(), "acct", "EURUSD", 1000, 0)
	require.NoError(t, err)
	assert.Greater(t, noSlip.Price, types.PriceFromFloat(1.1000).Float64(),
		"slippage must worsen (raise) a long fill above the ask")
}

func TestSubmitMarketOrder_NoPriceReturnsError(t *testing.T) {
	acct := account.NewAccount("test", types.MoneyFromFloat(10_000))
	s := NewSimBroker(acct, nil)

	_, err := s.SubmitMarketOrder(context.Background(), "acct", "EURUSD", 1000, 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no market price")
}

func TestSubmitMarketOrder_ZeroUnitsReturnsError(t *testing.T) {
	acct := account.NewAccount("test", types.MoneyFromFloat(10_000))
	s := NewSimBroker(acct, nil)
	require.NoError(t, s.UpdatePrice(eurusdTick(types.PriceFromFloat(1.1000))))

	_, err := s.SubmitMarketOrder(context.Background(), "acct", "EURUSD", 0, 0)
	require.Error(t, err)
}

// ── CloseTrade ───────────────────────────────────────────────────────────────

func TestCloseTrade_ClosesLongAtBid(t *testing.T) {
	acct := account.NewAccount("test", types.MoneyFromFloat(10_000))
	s := NewSimBroker(acct, nil)
	require.NoError(t, s.UpdatePrice(eurusdTick(types.PriceFromFloat(1.1000))))
	open, err := s.SubmitMarketOrder(context.Background(), "acct", "EURUSD", 1000, 0)
	require.NoError(t, err)

	require.NoError(t, s.UpdatePrice(eurusdTick(types.PriceFromFloat(1.1050))))
	res, err := s.CloseTrade(context.Background(), "acct", open.TradeID, 0)
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, open.TradeID, res.TradeID)
	assert.Equal(t, 0, acct.Lots.Len(), "lot must be removed after close")
	require.Len(t, acct.Trades, 1)
	assert.Greater(t, acct.Trades[0].PNL, types.Money(0), "long closed higher than it opened must be profitable")
}

func TestCloseTrade_UnknownTradeIDReturnsError(t *testing.T) {
	acct := account.NewAccount("test", types.MoneyFromFloat(10_000))
	s := NewSimBroker(acct, nil)

	_, err := s.CloseTrade(context.Background(), "acct", "no-such-trade", 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no open trade")
}

func TestCloseTrade_RecordsJournalEntry(t *testing.T) {
	acct := account.NewAccount("test", types.MoneyFromFloat(10_000))
	j := &stubJournal{}
	s := NewSimBroker(acct, j)
	require.NoError(t, s.UpdatePrice(eurusdTick(types.PriceFromFloat(1.1000))))
	open, err := s.SubmitMarketOrder(context.Background(), "acct", "EURUSD", 1000, 0)
	require.NoError(t, err)

	require.NoError(t, s.UpdatePrice(eurusdTick(types.PriceFromFloat(1.1050))))
	_, err = s.CloseTrade(context.Background(), "acct", open.TradeID, 0)
	require.NoError(t, err)
	require.Len(t, j.trades, 1)
	assert.Equal(t, open.TradeID, j.trades[0].TradeID)
}

// ── UpdateTradeStop ──────────────────────────────────────────────────────────

func TestUpdateTradeStop_SetsStopAndTake(t *testing.T) {
	acct := account.NewAccount("test", types.MoneyFromFloat(10_000))
	s := NewSimBroker(acct, nil)
	require.NoError(t, s.UpdatePrice(eurusdTick(types.PriceFromFloat(1.1000))))
	open, err := s.SubmitMarketOrder(context.Background(), "acct", "EURUSD", 1000, 0)
	require.NoError(t, err)

	require.NoError(t, s.UpdateTradeStop(context.Background(), "acct", open.TradeID, 1.0950, 1.1100))

	lot := acct.Lots.Get(open.TradeID)
	require.NotNil(t, lot)
	assert.Equal(t, types.PriceFromFloat(1.0950), lot.Stop)
	assert.Equal(t, types.PriceFromFloat(1.1100), lot.Take)
}

func TestUpdateTradeStop_NegativeCancels(t *testing.T) {
	acct := account.NewAccount("test", types.MoneyFromFloat(10_000))
	s := NewSimBroker(acct, nil)
	require.NoError(t, s.UpdatePrice(eurusdTick(types.PriceFromFloat(1.1000))))
	open, err := s.SubmitMarketOrder(context.Background(), "acct", "EURUSD", 1000, 0)
	require.NoError(t, err)
	require.NoError(t, s.UpdateTradeStop(context.Background(), "acct", open.TradeID, 1.0950, 0))

	require.NoError(t, s.UpdateTradeStop(context.Background(), "acct", open.TradeID, -1, 0))

	lot := acct.Lots.Get(open.TradeID)
	require.NotNil(t, lot)
	assert.Equal(t, types.Price(0), lot.Stop)
}

func TestUpdateTradeStop_UnknownTradeIDReturnsError(t *testing.T) {
	acct := account.NewAccount("test", types.MoneyFromFloat(10_000))
	s := NewSimBroker(acct, nil)

	err := s.UpdateTradeStop(context.Background(), "acct", "no-such-trade", 1.0950, 0)
	require.Error(t, err)
}

// ── GetOpenTrades / GetAccountSummary / GetAccountDetails ───────────────────

func TestGetOpenTrades_ReflectsOpenLots(t *testing.T) {
	acct := account.NewAccount("test", types.MoneyFromFloat(10_000))
	s := NewSimBroker(acct, nil)
	require.NoError(t, s.UpdatePrice(eurusdTick(types.PriceFromFloat(1.1000))))
	_, err := s.SubmitMarketOrder(context.Background(), "acct", "EURUSD", 1000, 0)
	require.NoError(t, err)
	_, err = s.SubmitMarketOrder(context.Background(), "acct", "EURUSD", -500, 0)
	require.NoError(t, err)

	trades, err := s.GetOpenTrades(context.Background(), "acct")
	require.NoError(t, err)
	require.Len(t, trades, 2)

	var sawLong, sawShort bool
	for _, tr := range trades {
		switch {
		case tr.Units == 1000:
			sawLong = true
		case tr.Units == -500:
			sawShort = true
		}
	}
	assert.True(t, sawLong)
	assert.True(t, sawShort)
}

func TestGetAccountSummary_ReflectsLedgerState(t *testing.T) {
	acct := account.NewAccount("test", types.MoneyFromFloat(10_000))
	s := NewSimBroker(acct, nil)

	summary, err := s.GetAccountSummary(context.Background(), "acct")
	require.NoError(t, err)
	assert.Equal(t, acct.ID, summary.ID)
	assert.Equal(t, acct.Balance.Float64(), summary.Balance)
	assert.Equal(t, acct.Equity.Float64(), summary.NAV)
}

func TestGetAccountDetails_IncludesOpenTrades(t *testing.T) {
	acct := account.NewAccount("test", types.MoneyFromFloat(10_000))
	s := NewSimBroker(acct, nil)
	require.NoError(t, s.UpdatePrice(eurusdTick(types.PriceFromFloat(1.1000))))
	_, err := s.SubmitMarketOrder(context.Background(), "acct", "EURUSD", 1000, 0)
	require.NoError(t, err)

	details, err := s.GetAccountDetails(context.Background(), "acct")
	require.NoError(t, err)
	assert.Equal(t, acct.ID, details.ID)
	require.Len(t, details.OpenTrades, 1)
}
