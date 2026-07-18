package journal

import (
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/rustyeddy/trader/brokers/oanda"
	"github.com/rustyeddy/trader/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type captureJournal struct {
	trades []TradeRecord
	err    error
}

func (j *captureJournal) RecordTrade(tr TradeRecord) error {
	if j.err != nil {
		return j.err
	}
	j.trades = append(j.trades, tr)
	return nil
}

func (j *captureJournal) RecordEquity(EquitySnapshot) error { return nil }
func (j *captureJournal) Close() error                      { return nil }

func TestLiveJournalHandleStreamEventReturnsError(t *testing.T) {
	t.Parallel()

	lj := NewLiveJournal(nil, "", &captureJournal{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	wantErr := errors.New("stream failed")

	err := lj.handleStreamEvent(oanda.TxEvent{Err: wantErr})
	assert.ErrorIs(t, err, wantErr)
}

func TestLiveJournalHandleTransactionRecordsCloseAndOpenFromMixedFill(t *testing.T) {
	t.Parallel()

	journal := &captureJournal{}
	lj := NewLiveJournal(nil, "", journal, slog.New(slog.NewTextHandler(io.Discard, nil)))

	openTime := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	closeTime := openTime.Add(2 * time.Hour)
	nextCloseTime := closeTime.Add(90 * time.Minute)

	lj.recordOpen(oanda.Transaction{
		TradeID:    "open-1",
		Instrument: "EUR_USD",
		Units:      1000,
		Price:      1.11111,
		Time:       openTime,
	})

	lj.handleTransaction(oanda.Transaction{
		ID:         "42",
		Type:       "ORDER_FILL",
		TradeID:    "open-2",
		Instrument: "EUR_USD",
		Units:      2000,
		Price:      1.22222,
		Time:       closeTime,
		Reason:     "CLIENT_REQUEST",
		TradesClosed: []oanda.ClosedTrade{
			{TradeID: "open-1", Units: -1000, Price: 1.12345, RealizedPL: 12.34},
		},
	})

	require.Len(t, journal.trades, 1)
	assert.Equal(t, "open-1", journal.trades[0].TradeID)
	assert.Equal(t, types.PriceFromFloat(1.11111), journal.trades[0].EntryPrice)
	assert.Equal(t, types.FromTime(openTime), journal.trades[0].OpenTime)
	assert.Equal(t, int64(42), lj.LastSeenTxID())

	lj.handleTransaction(oanda.Transaction{
		ID:         "43",
		Type:       "ORDER_FILL",
		Instrument: "EUR_USD",
		Time:       nextCloseTime,
		Reason:     "STOP_LOSS_ORDER",
		TradesClosed: []oanda.ClosedTrade{
			{TradeID: "open-2", Units: -2000, Price: 1.2, RealizedPL: -4.56},
		},
	})

	require.Len(t, journal.trades, 2)
	assert.Equal(t, "open-2", journal.trades[1].TradeID)
	assert.Equal(t, types.PriceFromFloat(1.22222), journal.trades[1].EntryPrice)
	assert.Equal(t, types.FromTime(closeTime), journal.trades[1].OpenTime)
}

func TestLiveJournalRecordClosePreservesPendingOpenOnWriteFailure(t *testing.T) {
	t.Parallel()

	journal := &captureJournal{err: errors.New("write failed")}
	lj := NewLiveJournal(nil, "", journal, slog.New(slog.NewTextHandler(io.Discard, nil)))

	lj.recordOpen(oanda.Transaction{
		TradeID:    "open-1",
		Instrument: "EUR_USD",
		Units:      1000,
		Price:      1.11111,
		Time:       time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC),
	})

	lj.recordClose(oanda.Transaction{
		Instrument: "EUR_USD",
		Time:       time.Date(2024, 1, 2, 4, 4, 5, 0, time.UTC),
		Reason:     "CLIENT_REQUEST",
	}, oanda.ClosedTrade{
		TradeID:    "open-1",
		Units:      -1000,
		Price:      1.12,
		RealizedPL: 1.23,
	})

	lj.mu.Lock()
	_, ok := lj.pendingOpens["open-1"]
	lj.mu.Unlock()
	assert.True(t, ok)
}

func TestParseTxID(t *testing.T) {
	t.Parallel()

	assert.Equal(t, int64(12345), parseTxID("12345"))
	assert.Equal(t, int64(0), parseTxID("bad"))
}
