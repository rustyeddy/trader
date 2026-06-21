package sim

import (
	"context"
	"testing"

	"github.com/rustyeddy/trader"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── test helpers ──────────────────────────────────────────────────────────────

// stubJournal records every call so tests can assert on side-effects.
type stubJournal struct {
	trades  []trader.TradeRecord
	equity  []trader.EquitySnapshot
	closeErr error
}

func (j *stubJournal) RecordTrade(r trader.TradeRecord) error {
	j.trades = append(j.trades, r)
	return nil
}
func (j *stubJournal) RecordEquity(s trader.EquitySnapshot) error {
	j.equity = append(j.equity, s)
	return nil
}
func (j *stubJournal) Close() error { return j.closeErr }

// eurusdTick returns a valid EURUSD tick at the given mid (bid = mid-1, ask = mid+1).
func eurusdTick(mid trader.Price) trader.Tick {
	return trader.Tick{
		Instrument: "EURUSD",
		BA:         trader.BA{Bid: mid - 1, Ask: mid + 1},
	}
}

// openLot adds a minimal open lot for EURUSD to acct and returns it.
func openLot(t *testing.T, acct *trader.Account, entryPrice trader.Price) *trader.Lot {
	t.Helper()
	lot := &trader.Lot{
		TradeCommon: &trader.TradeCommon{
			ID:         "lot-sim-1",
			Instrument: "EURUSD",
			Side:       trader.Long,
			Units:      1000,
		},
		EntryPrice:     entryPrice,
		EntryTime:      1000,
		OriginalUnits:  1000,
		RemainingUnits: 1000,
		State:          trader.LotOpen,
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
	acct := trader.NewAccount("test", trader.MoneyFromFloat(5_000))
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
	acct := trader.NewAccount("test", trader.MoneyFromFloat(10_000))
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
	acct := trader.NewAccount("test", trader.MoneyFromFloat(10_000))
	s := NewSimBroker(acct, nil)

	err := s.UpdatePrice(eurusdTick(trader.PriceFromFloat(1.08)))
	require.NoError(t, err)
	assert.Contains(t, s.prices, "EURUSD")
}

func TestUpdatePrice_NormalizesInstrumentName(t *testing.T) {
	s := NewSimBroker(nil, nil)
	tick := trader.Tick{
		Instrument: "EUR_USD", // underscore form
		BA:         trader.BA{Bid: 1_080_000, Ask: 1_080_200},
	}
	require.NoError(t, s.UpdatePrice(tick))
	assert.Contains(t, s.prices, "EURUSD")
	assert.NotContains(t, s.prices, "EUR_USD")
}

func TestUpdatePrice_BlankInstrumentReturnsError(t *testing.T) {
	s := NewSimBroker(nil, nil)
	tick := trader.Tick{Instrument: "", BA: trader.BA{Bid: 1_000_000, Ask: 1_000_200}}
	err := s.UpdatePrice(tick)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "blank instrument")
}

func TestUpdatePrice_InvalidTickReturnsError(t *testing.T) {
	s := NewSimBroker(nil, nil)
	// Ask < Bid → invalid
	tick := trader.Tick{
		Instrument: "EURUSD",
		BA:         trader.BA{Bid: 1_080_200, Ask: 1_080_000},
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

	eurTick := eurusdTick(trader.PriceFromFloat(1.08))
	jpyTick := trader.Tick{
		Instrument: "USDJPY",
		BA:         trader.BA{Bid: 150_000_000, Ask: 150_020_000},
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
	acct := trader.NewAccount("test", trader.MoneyFromFloat(10_000))
	j := &stubJournal{}
	s := NewSimBroker(acct, j)

	entry := trader.PriceFromFloat(1.08)
	openLot(t, acct, entry)

	// Provide a price so CloseAll can compute an exit.
	require.NoError(t, s.UpdatePrice(eurusdTick(trader.PriceFromFloat(1.09))))

	require.NoError(t, s.CloseAll(context.Background(), "take-profit"))

	assert.Equal(t, 0, acct.Lots.Len())
	require.Len(t, j.trades, 1)
	assert.Equal(t, "EURUSD", j.trades[0].Instrument)
	assert.Equal(t, "take-profit", j.trades[0].Reason)
	assert.Len(t, j.equity, 1)
}

func TestCloseAll_MissingPriceReturnsError(t *testing.T) {
	acct := trader.NewAccount("test", trader.MoneyFromFloat(10_000))
	s := NewSimBroker(acct, nil)
	openLot(t, acct, trader.PriceFromFloat(1.08))

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
	acct := trader.NewAccount("test", trader.MoneyFromFloat(10_000))
	s := NewSimBroker(acct, nil) // no journal
	openLot(t, acct, trader.PriceFromFloat(1.08))
	require.NoError(t, s.UpdatePrice(eurusdTick(trader.PriceFromFloat(1.09))))

	require.NotPanics(t, func() {
		_ = s.CloseAll(context.Background(), "close")
	})
}
